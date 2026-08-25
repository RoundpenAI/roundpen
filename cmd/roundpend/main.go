// Package main is the Roundpen control-plane daemon (roundpend).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/RoundpenAI/roundpen/internal/config"
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
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Phase 1 wiring (storage / backend / HTTP) lands next; for now block until signal.
	<-ctx.Done()
	logger.Info("roundpend shutting down")
}
