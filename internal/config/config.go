// Package config loads Roundpen daemon configuration from the environment.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
)

// Config holds process-wide settings for roundpend.
type Config struct {
	HTTPAddr    string
	DatabaseURL string
	DataRoot    string
	Backend     string // docker | kern | k8s
	LogLevel    slog.Level
}

// Load reads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:    getenv("ROUNDPEN_HTTP_ADDR", ":9527"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		DataRoot:    getenv("ROUNDPEN_DATA_ROOT", "./data"),
		Backend:     getenv("ROUNDPEN_BACKEND", "docker"),
		LogLevel:    slog.LevelInfo,
	}
	if v := os.Getenv("ROUNDPEN_LOG_LEVEL"); v != "" {
		var level slog.Level
		if err := level.UnmarshalText([]byte(v)); err != nil {
			return nil, fmt.Errorf("ROUNDPEN_LOG_LEVEL: %w", err)
		}
		cfg.LogLevel = level
	}
	switch strings.ToLower(cfg.Backend) {
	case "docker", "kern", "k8s":
	default:
		return nil, fmt.Errorf("unsupported ROUNDPEN_BACKEND %q", cfg.Backend)
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
