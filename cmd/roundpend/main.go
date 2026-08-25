// Package main is the Roundpen control-plane daemon (roundpend).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/api/e2b"
	"github.com/RoundpenAI/roundpen/internal/api/httpapi"
	dockerbackend "github.com/RoundpenAI/roundpen/internal/backend/docker"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/workspace/local"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	logger.Info("roundpend starting",
		slog.String("http_addr", cfg.HTTPAddr),
		slog.String("data_root", cfg.DataRoot),
		slog.String("backend", cfg.Backend),
		slog.String("default_image", cfg.DefaultImage),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := storage.OpenPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("postgres", slog.Any("err", err))
		os.Exit(1)
	}
	defer db.Close()

	if err := db.MigrateEmbedded(ctx); err != nil {
		logger.Error("migrate", slog.Any("err", err))
		os.Exit(1)
	}

	if err := os.MkdirAll(cfg.DataRoot, 0o755); err != nil {
		logger.Error("data_root", slog.Any("err", err))
		os.Exit(1)
	}
	wsFS := local.New(cfg.DataRoot)

	var mgr sandbox.Manager
	switch cfg.Backend {
	case "docker":
		be, err := dockerbackend.New(cfg.DockerHost, cfg.DockerRuntime)
		if err != nil {
			logger.Error("docker backend", slog.Any("err", err))
			os.Exit(1)
		}
		defer be.Close()
		if err := be.Ping(ctx); err != nil {
			logger.Error("docker ping", slog.Any("err", err))
			os.Exit(1)
		}
		mgr = sandbox.NewService(storage.NewSandboxStore(db), be, wsFS, cfg.DefaultImage, cfg.DefaultTTL, logger)
	default:
		logger.Error("backend not implemented for mvp", slog.String("backend", cfg.Backend))
		os.Exit(1)
	}

	mux := http.NewServeMux()
	(&e2b.Handler{Manager: mgr}).Mount(mux)
	(&httpapi.Handler{Manager: mgr}).Mount(mux)

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           auth.APIKey(cfg.APIKey, mux),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("http listening", slog.String("addr", cfg.HTTPAddr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("http server", slog.Any("err", err))
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
	logger.Info("roundpend shut down")
}
