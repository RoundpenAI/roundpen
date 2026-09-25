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
	BootstrapAdminPassword  string // pins the admin password on every boot (local dev); empty = random when unset
	IMEnabled               bool   // start IM channel engines; off unless set (secondary/test instances must not race the primary one for chat connections)
	DatabaseURL             string
	DataRoot                string
	SecretKey               string // master key (hex/base64, 32 bytes) sealing DB secrets; empty = auto-generated file under DataRoot
	Backend                 string // docker (qemu accepted as legacy alias; k8s/kern rejected)
	DockerHost              string
	DockerRuntime           string // e.g. runc, runsc; empty = daemon default
	DefaultImage            string
	DefaultBrowserTemplate  string // browser slot template name (default "browser")
	DefaultAgentTemplate    string // agent slot template name (default code-agent)
	AgentImage              string // default agent OCI image ref
	BrowserImage            string // default browser OCI image
	DefaultTTL              time.Duration
	LogLevel                slog.Level
	LLMGW                   LLMGWConfig
	WebTools                WebToolsConfig
	PreviewPublicURL        string        // absolute base URL for preview links
	PreviewTokenTTL         time.Duration // default 15m
	TrustedProxies          string        // comma-separated CIDRs that may send X-Forwarded-*
	CDP                     CDPConfig
	QEMUEnabled             bool // attempt to attach qemu for browser slot
}

// Load reads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:                getenv("ROUNDPEN_HTTP_ADDR", ":9527"),
		APIKey:                  os.Getenv("ROUNDPEN_API_KEY"),
		AllowPublicRegistration: getenvBool("ROUNDPEN_ALLOW_PUBLIC_REGISTRATION", false),
		BootstrapAdmin:          getenvBool("ROUNDPEN_BOOTSTRAP_ADMIN", true),
		BootstrapAdminPassword:  os.Getenv("ROUNDPEN_BOOTSTRAP_ADMIN_PASSWORD"),
		IMEnabled:               getenvBool("ROUNDPEN_IM_ENABLED", false),
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		DataRoot:                getenv("ROUNDPEN_DATA_ROOT", "./data"),
		SecretKey:               os.Getenv("ROUNDPEN_SECRET_KEY"),
		Backend:                 getenv("ROUNDPEN_BACKEND", "docker"),
		DockerHost:              getenv("DOCKER_HOST", "unix:///var/run/docker.sock"),
		DockerRuntime:           os.Getenv("ROUNDPEN_DOCKER_RUNTIME"),
		DefaultImage:            getenv("ROUNDPEN_DEFAULT_IMAGE", "ghcr.io/roundpenai/code-agent:0.1.0"),
		DefaultBrowserTemplate:  getenv("ROUNDPEN_DEFAULT_BROWSER_TEMPLATE", "browser"),
		DefaultAgentTemplate:    getenv("ROUNDPEN_DEFAULT_AGENT_TEMPLATE", "code-agent"),
		AgentImage:              getenv("ROUNDPEN_AGENT_IMAGE", "ghcr.io/roundpenai/code-agent:0.1.0"),
		BrowserImage:            getenv("ROUNDPEN_BROWSER_IMAGE", "ghcr.io/browserless/chrome:v2.56.7"),
		DefaultTTL:              30 * time.Minute,
		LogLevel:                slog.LevelInfo,
		PreviewPublicURL:        os.Getenv("ROUNDPEN_PREVIEW_PUBLIC_URL"),
		PreviewTokenTTL:         15 * time.Minute,
		TrustedProxies:          strings.TrimSpace(os.Getenv("ROUNDPEN_TRUSTED_PROXIES")),
		QEMUEnabled:             getenvBool("ROUNDPEN_QEMU_ENABLED", true),
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
	case "docker", "qemu":
		// qemu here means multi-backend default preference historically; Agent is always Docker.
	case "k8s":
		return nil, fmt.Errorf("ROUNDPEN_BACKEND=k8s is not implemented; use docker (Agent) + QEMU (Browser)")
	case "kern":
		return nil, fmt.Errorf("ROUNDPEN_BACKEND=kern is removed; use docker (Agent) + QEMU (Browser)")
	default:
		return nil, fmt.Errorf("unsupported ROUNDPEN_BACKEND %q", cfg.Backend)
	}
	if strings.EqualFold(cfg.Backend, "qemu") {
		// Legacy: ROUNDPEN_BACKEND=qemu selected Agent-on-QEMU. Agent is Docker-only now.
		cfg.Backend = "docker"
	}
	if cfg.DefaultAgentTemplate == "" || cfg.DefaultAgentTemplate == "agent-claude" || cfg.DefaultAgentTemplate == "host" {
		cfg.DefaultAgentTemplate = "code-agent"
	}
	if cfg.DefaultImage == "" || cfg.DefaultImage == "host" || strings.HasSuffix(cfg.DefaultImage, ".qcow2") {
		cfg.DefaultImage = cfg.AgentImage
		if cfg.DefaultImage == "" {
			cfg.DefaultImage = "ghcr.io/roundpenai/code-agent:0.1.0"
		}
	}
	if cfg.DatabaseURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}
	llmgwCfg, err := loadLLMGW()
	if err != nil {
		return nil, err
	}
	cfg.LLMGW = llmgwCfg
	cfg.WebTools = loadWebTools()
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
