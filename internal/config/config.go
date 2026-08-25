// Package config loads Roundpen daemon configuration from the environment.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds process-wide settings for roundpend.
type Config struct {
	HTTPAddr      string
	APIKey        string // empty = auth disabled (dev only)
	DatabaseURL   string
	DataRoot      string
	Backend       string // docker | kern | k8s
	DockerHost    string
	DockerRuntime string // e.g. runc, runsc; empty = daemon default
	DefaultImage  string
	DefaultTTL    time.Duration
	LogLevel      slog.Level
}

// Load reads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:      getenv("ROUNDPEN_HTTP_ADDR", ":9527"),
		APIKey:        os.Getenv("ROUNDPEN_API_KEY"),
		DatabaseURL:   os.Getenv("DATABASE_URL"),
		DataRoot:      getenv("ROUNDPEN_DATA_ROOT", "./data"),
		Backend:       getenv("ROUNDPEN_BACKEND", "docker"),
		DockerHost:    getenv("DOCKER_HOST", "unix:///var/run/docker.sock"),
		DockerRuntime: os.Getenv("ROUNDPEN_DOCKER_RUNTIME"),
		DefaultImage:  getenv("ROUNDPEN_DEFAULT_IMAGE", "python:3.12-slim"),
		DefaultTTL:    30 * time.Minute,
		LogLevel:      slog.LevelInfo,
	}
	if v := os.Getenv("ROUNDPEN_DEFAULT_TTL"); v != "" {
		secs, err := strconv.Atoi(v)
		if err != nil || secs <= 0 {
			return nil, fmt.Errorf("ROUNDPEN_DEFAULT_TTL: must be positive seconds")
		}
		cfg.DefaultTTL = time.Duration(secs) * time.Second
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
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
