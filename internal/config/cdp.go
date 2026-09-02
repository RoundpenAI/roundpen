package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	CDPProviderAuto   = "auto"
	CDPProviderDocker = "docker"
	CDPProviderHost   = "host"
	CDPProviderRemote = "remote"
	CDPProviderCloud  = "cloud"
	DefaultCDPPort    = 9222
)

// CDPConfig is the instance-wide DevTools endpoint policy.
type CDPConfig struct {
	// Provider is auto | docker | host | remote | cloud.
	Provider string
	// Endpoint is a CDP http/ws URL (host/remote/cloud).
	Endpoint string
	// Token is an optional bearer/query secret for remote/cloud.
	Token string
	// Port is the guest CDP port for the docker provider (default 9222).
	Port int
}

func loadCDP() (CDPConfig, error) {
	cfg := CDPConfig{
		Provider: strings.ToLower(strings.TrimSpace(os.Getenv("ROUNDPEN_CDP_PROVIDER"))),
		Endpoint: strings.TrimSpace(os.Getenv("ROUNDPEN_CDP_ENDPOINT")),
		Token:    strings.TrimSpace(os.Getenv("ROUNDPEN_CDP_TOKEN")),
		Port:     DefaultCDPPort,
	}
	if cfg.Provider == "" {
		cfg.Provider = CDPProviderAuto
	}
	if v := strings.TrimSpace(os.Getenv("ROUNDPEN_CDP_PORT")); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 65535 {
			return cfg, fmt.Errorf("ROUNDPEN_CDP_PORT: must be 1–65535")
		}
		cfg.Port = n
	}
	if err := NormalizeCDP(&cfg); err != nil {
		return cfg, err
	}
	return cfg, nil
}

// NormalizeCDP validates and fills defaults.
func NormalizeCDP(c *CDPConfig) error {
	if c == nil {
		return fmt.Errorf("cdp config is nil")
	}
	c.Provider = strings.ToLower(strings.TrimSpace(c.Provider))
	if c.Provider == "" {
		c.Provider = CDPProviderAuto
	}
	switch c.Provider {
	case CDPProviderAuto, CDPProviderDocker, CDPProviderHost, CDPProviderRemote, CDPProviderCloud:
	default:
		return fmt.Errorf("cdp provider must be auto, docker, host, remote, or cloud")
	}
	c.Endpoint = strings.TrimSpace(c.Endpoint)
	c.Token = strings.TrimSpace(c.Token)
	if c.Port <= 0 {
		c.Port = DefaultCDPPort
	}
	if c.Port > 65535 {
		return fmt.Errorf("cdp port must be 1–65535")
	}
	if (c.Provider == CDPProviderRemote || c.Provider == CDPProviderCloud) && c.Endpoint == "" {
		return fmt.Errorf("cdp endpoint is required for provider %s", c.Provider)
	}
	return nil
}

// ResolveCDPProvider picks the effective provider. hostChromeFound is true when
// a Chrome/Chromium binary exists on the API process (laptop only).
func ResolveCDPProvider(cfg *Config, hostChromeFound bool) string {
	if cfg == nil {
		if hostChromeFound {
			return CDPProviderHost
		}
		return CDPProviderDocker
	}
	p := strings.ToLower(strings.TrimSpace(cfg.CDP.Provider))
	if p == "" {
		p = CDPProviderAuto
	}
	if p != CDPProviderAuto {
		return p
	}
	if strings.EqualFold(cfg.Backend, "docker") {
		return CDPProviderDocker
	}
	if hostChromeFound {
		return CDPProviderHost
	}
	return CDPProviderDocker
}

// CDPHint explains the resolved provider for the settings UI.
func CDPHint(cfg *Config, hostChromeFound bool) string {
	resolved := ResolveCDPProvider(cfg, hostChromeFound)
	switch resolved {
	case CDPProviderDocker:
		if cfg != nil && !strings.EqualFold(cfg.Backend, "docker") {
			return "Docker Chrome needs ROUNDPEN_BACKEND=docker (and docker.sock on NAS). Or switch to remote/cloud CDP."
		}
		return "Connects to Chrome inside the sandbox via Dial on the CDP port. The sandbox image must expose DevTools."
	case CDPProviderHost:
		if !hostChromeFound {
			return "Host Chrome was selected but no chrome/chromium binary is on this process PATH (typical on NAS compose)."
		}
		return "Uses Chrome on the machine running roundpend. Laptop/debug only."
	case CDPProviderRemote:
		return "Attaches to the configured CDP URL (Browserless or self-hosted Chrome)."
	case CDPProviderCloud:
		return "Attaches to a cloud browser CDP websocket (paste the session URL)."
	default:
		return ""
	}
}
