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
	HTTPAddr                string
	APIKey                  string // seeds the bootstrap admin API key; empty = generate one
	AllowPublicRegistration bool
	BootstrapAdmin          bool
	DatabaseURL             string
	DataRoot                string
	Backend                 string // docker | kern | k8s
	DockerHost              string
	DockerRuntime           string // e.g. runc, runsc; empty = daemon default
	DefaultImage            string
	DefaultTTL              time.Duration
	LogLevel                slog.Level
	LLMGW                   LLMGWConfig
	PreviewPublicURL        string        // absolute base URL for preview links
	PreviewTokenTTL         time.Duration // default 15m
	TrustedProxies          string        // comma-separated CIDRs that may send X-Forwarded-*
	TemplateBuilder         string        // docker | kaniko | auto
	KanikoExecutor          string
	KanikoDestination       string
	KanikoInsecure          bool
	KanikoSkipTLSVerify     bool
	KanikoRegistryMirrors   []string
	KanikoExtraArgs         []string
	CDP                     CDPConfig
}

// Load reads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:                getenv("ROUNDPEN_HTTP_ADDR", ":9527"),
		APIKey:                  os.Getenv("ROUNDPEN_API_KEY"),
		AllowPublicRegistration: getenvBool("ROUNDPEN_ALLOW_PUBLIC_REGISTRATION", false),
		BootstrapAdmin:          getenvBool("ROUNDPEN_BOOTSTRAP_ADMIN", true),
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		DataRoot:                getenv("ROUNDPEN_DATA_ROOT", "./data"),
		Backend:                 getenv("ROUNDPEN_BACKEND", "kern"),
		DockerHost:              getenv("DOCKER_HOST", "unix:///var/run/docker.sock"),
		DockerRuntime:           os.Getenv("ROUNDPEN_DOCKER_RUNTIME"),
		DefaultImage:            getenv("ROUNDPEN_DEFAULT_IMAGE", "host"),
		DefaultTTL:              30 * time.Minute,
		LogLevel:                slog.LevelInfo,
		PreviewPublicURL:        os.Getenv("ROUNDPEN_PREVIEW_PUBLIC_URL"),
		PreviewTokenTTL:         15 * time.Minute,
		TrustedProxies:          strings.TrimSpace(os.Getenv("ROUNDPEN_TRUSTED_PROXIES")),
		TemplateBuilder:         strings.ToLower(strings.TrimSpace(os.Getenv("ROUNDPEN_TEMPLATE_BUILDER"))),
		KanikoExecutor:          getenv("ROUNDPEN_KANIKO_EXECUTOR", "executor"),
		KanikoDestination:       strings.TrimSpace(os.Getenv("ROUNDPEN_KANIKO_DESTINATION")),
		KanikoInsecure:          getenvBool("ROUNDPEN_KANIKO_INSECURE", false),
		KanikoSkipTLSVerify:     getenvBool("ROUNDPEN_KANIKO_SKIP_TLS_VERIFY", false),
		KanikoRegistryMirrors:   SplitKanikoMirrors(os.Getenv("ROUNDPEN_KANIKO_REGISTRY_MIRROR")),
	}
	if v := strings.TrimSpace(os.Getenv("ROUNDPEN_KANIKO_EXTRA_ARGS")); v != "" {
		cfg.KanikoExtraArgs = strings.Fields(v)
	}
	if v := os.Getenv("ROUNDPEN_PREVIEW_TOKEN_TTL"); v != "" {
		secs, err := strconv.Atoi(v)
		if err != nil || secs <= 0 {
			return nil, fmt.Errorf("ROUNDPEN_PREVIEW_TOKEN_TTL: must be positive seconds")
		}
		cfg.PreviewTokenTTL = time.Duration(secs) * time.Second
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
	llmgwCfg, err := loadLLMGW()
	if err != nil {
		return nil, err
	}
	cfg.LLMGW = llmgwCfg
	cdpCfg, err := loadCDP()
	if err != nil {
		return nil, err
	}
	cfg.CDP = cdpCfg
	return cfg, nil
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvBool(key string, fallback bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return fallback
}

// SplitKanikoMirrors parses space/comma-separated registry mirrors.
// Accepts hosts or URLs (https://docker.1ms.run → docker.1ms.run).
func SplitKanikoMirrors(v string) []string {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	fields := strings.FieldsFunc(v, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	})
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		f = strings.TrimPrefix(f, "https://")
		f = strings.TrimPrefix(f, "http://")
		f = strings.TrimSuffix(f, "/")
		if f != "" {
			out = append(out, f)
		}
	}
	return out
}
