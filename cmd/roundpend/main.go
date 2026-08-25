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
	kernbackend "github.com/RoundpenAI/roundpen/internal/backend/kern"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/workspace"
	"github.com/RoundpenAI/roundpen/internal/workspace/local"
	"github.com/RoundpenAI/roundpen/internal/workspace/sshfs"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		os.Exit(1)
	}

	dataRoot := cfg.EffectiveDataRoot()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: cfg.LogLevel}))
	logger.Info("roundpend starting",
		slog.String("http_addr", cfg.HTTPAddr),
		slog.String("data_root", dataRoot),
		slog.String("backend", cfg.Backend),
		slog.String("docker_host", cfg.DockerHost),
		slog.String("default_image", cfg.DefaultImage),
		slog.Bool("workspace_ssh", cfg.WorkspaceUsesSSH()),
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

	wsFS, err := newWorkspaceFS(cfg, dataRoot, logger)
	if err != nil {
		logger.Error("workspace", slog.Any("err", err))
		os.Exit(1)
	}

	var mgr sandbox.Manager
	store := storage.NewSandboxStore(db)
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
		mgr = sandbox.NewService(store, be, wsFS, cfg.DefaultImage, cfg.DefaultTTL, logger)
	case "kern":
		be := kernbackend.New()
		mgr = sandbox.NewService(store, be, wsFS, cfg.DefaultImage, cfg.DefaultTTL, logger)
		logger.Info("using kern backend (daemonless host processes)")
	default:
		logger.Error("backend not implemented", slog.String("backend", cfg.Backend))
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

func newWorkspaceFS(cfg *config.Config, dataRoot string, logger *slog.Logger) (workspace.FS, error) {
	if cfg.WorkspaceUsesSSH() {
		fs, err := sshfs.NewFromDockerHost(cfg.DockerHost, dataRoot)
		if err != nil {
			return nil, err
		}
		logger.Info("using ssh workspace fs",
			slog.String("docker_host", cfg.DockerHost),
			slog.String("remote_root", dataRoot),
		)
		return fs, nil
	}
	if err := os.MkdirAll(dataRoot, 0o755); err != nil {
		return nil, err
	}
	return local.New(dataRoot), nil
}
