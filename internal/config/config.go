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
	HTTPAddr                  string
	APIKey                    string // seeds the bootstrap admin API key; empty = generate one
	AllowPublicRegistration   bool
	BootstrapAdmin            bool
	BootstrapAdminPassword    string // initial admin password when unset; with Pin, restored every boot
	BootstrapAdminPinPassword bool   // when true with BootstrapAdminPassword, pin on every boot (local dev)
	IMEnabled                 bool   // start IM channel engines; off unless set (secondary/test instances must not race the primary one for chat connections)
	DatabaseURL               string
	DataRoot                  string
	SecretKey                 string // master key (hex/base64, 32 bytes) sealing DB secrets; empty = auto-generated file under DataRoot
	AllowKeyMismatch          bool   // start even when the master key cannot open secrets already sealed in the DB (recovery: re-enter them by hand)
	Backend                   string // docker (qemu accepted as legacy alias; k8s/kern rejected)
	DockerHost                string
	DockerRuntime             string // e.g. runc, runsc; empty = daemon default
	DefaultImage              string
	DefaultBrowserTemplate    string // browser slot template name (default "browser")
	DefaultAgentTemplate      string // agent slot template name (default code-agent)
	AgentImage                string // default agent OCI image ref
	BrowserImage              string // default browser OCI image
	ImageMirrors              string         // ROUNDPEN_IMAGE_MIRRORS: comma templates, or none/off
	ImageMirrorRaceDelay      *time.Duration // nil=default 15s; 0=disable race; ROUNDPEN_IMAGE_MIRROR_RACE_DELAY
	DefaultTTL                time.Duration
	LogLevel                  slog.Level
	LLMGW                     LLMGWConfig
	WebTools                  WebToolsConfig
	PreviewPublicURL          string        // absolute base URL for preview links
	PreviewDomain             string        // host suffix for per-port preview subdomains ({id}-{port}.{domain}); "" = path previews only
	PreviewDomainScheme       string        // scheme for those links; "" = follow the console request
	PreviewTokenTTL           time.Duration // default 15m
	TrustedProxies            string        // comma-separated CIDRs that may send X-Forwarded-*
	CDP                       CDPConfig
	QEMUEnabled               bool // attempt to attach qemu for browser slot
	VAPIDPublicKey            string
	VAPIDPrivateKey           string
	VAPIDSubject              string // mailto: or https: contact for VAPID
	FnosGatewaySocket         string // fnOS unified-gateway Unix socket; empty = gateway SSO off
	FnosGatewayPrefix         string // gateway path prefix the fnOS web console mounts at
	GitHubOAuthRelay          string // official GitHub OAuth relay origin; empty = off
}

// Load reads configuration from environment variables with sensible defaults.
func Load() (*Config, error) {
	cfg := &Config{
		HTTPAddr:                  getenv("ROUNDPEN_HTTP_ADDR", ":9527"),
		APIKey:                    os.Getenv("ROUNDPEN_API_KEY"),
		AllowPublicRegistration:   getenvBool("ROUNDPEN_ALLOW_PUBLIC_REGISTRATION", false),
		BootstrapAdmin:            getenvBool("ROUNDPEN_BOOTSTRAP_ADMIN", true),
		BootstrapAdminPassword:    os.Getenv("ROUNDPEN_BOOTSTRAP_ADMIN_PASSWORD"),
		BootstrapAdminPinPassword: getenvBool("ROUNDPEN_BOOTSTRAP_ADMIN_PIN_PASSWORD", false),
		IMEnabled:                 getenvBool("ROUNDPEN_IM_ENABLED", false),
		DatabaseURL:               os.Getenv("DATABASE_URL"),
		DataRoot:                  getenv("ROUNDPEN_DATA_ROOT", "./data"),
		SecretKey:                 os.Getenv("ROUNDPEN_SECRET_KEY"),
		AllowKeyMismatch:          getenvBool("ROUNDPEN_ALLOW_KEY_MISMATCH", false),
		Backend:                   getenv("ROUNDPEN_BACKEND", "docker"),
		DockerHost:                getenv("DOCKER_HOST", "unix:///var/run/docker.sock"),
		DockerRuntime:             os.Getenv("ROUNDPEN_DOCKER_RUNTIME"),
		DefaultImage:              getenv("ROUNDPEN_DEFAULT_IMAGE", defaultAgentImage),
		DefaultBrowserTemplate:    getenv("ROUNDPEN_DEFAULT_BROWSER_TEMPLATE", "browser"),
		DefaultAgentTemplate:      getenv("ROUNDPEN_DEFAULT_AGENT_TEMPLATE", "code-agent"),
		AgentImage:                getenv("ROUNDPEN_AGENT_IMAGE", defaultAgentImage),
		BrowserImage:              getenv("ROUNDPEN_BROWSER_IMAGE", "ghcr.io/browserless/chrome:v2.56.7"),
		ImageMirrors:              strings.TrimSpace(os.Getenv("ROUNDPEN_IMAGE_MIRRORS")),
		DefaultTTL:                30 * time.Minute,
		LogLevel:                  slog.LevelInfo,
		PreviewPublicURL:          os.Getenv("ROUNDPEN_PREVIEW_PUBLIC_URL"),
		PreviewTokenTTL:           15 * time.Minute,
		TrustedProxies:            strings.TrimSpace(os.Getenv("ROUNDPEN_TRUSTED_PROXIES")),
		QEMUEnabled:               getenvBool("ROUNDPEN_QEMU_ENABLED", true),
		VAPIDPublicKey:            strings.TrimSpace(os.Getenv("ROUNDPEN_VAPID_PUBLIC_KEY")),
		VAPIDPrivateKey:           strings.TrimSpace(os.Getenv("ROUNDPEN_VAPID_PRIVATE_KEY")),
		VAPIDSubject:              strings.TrimSpace(os.Getenv("ROUNDPEN_VAPID_SUBJECT")),
		FnosGatewaySocket:         strings.TrimSpace(os.Getenv("ROUNDPEN_FNOS_GATEWAY_SOCKET")),
		FnosGatewayPrefix:         strings.TrimSpace(getenv("ROUNDPEN_FNOS_GATEWAY_PREFIX", "/app/roundpen")),
		GitHubOAuthRelay:          githubOAuthRelay(os.Getenv("ROUNDPEN_GITHUB_OAUTH_RELAY")),
	}
	domain, scheme, err := parsePreviewDomain(os.Getenv("ROUNDPEN_PREVIEW_DOMAIN"))
	if err != nil {
		return nil, err
	}
	cfg.PreviewDomain, cfg.PreviewDomainScheme = domain, scheme
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
	if v := strings.TrimSpace(os.Getenv("ROUNDPEN_IMAGE_MIRROR_RACE_DELAY")); v != "" {
		secs, err := strconv.Atoi(v)
		if err != nil || secs < 0 {
			return nil, fmt.Errorf("ROUNDPEN_IMAGE_MIRROR_RACE_DELAY: must be >= 0 seconds")
		}
		d := time.Duration(secs) * time.Second
		cfg.ImageMirrorRaceDelay = &d
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
	cfg.DefaultImage = CoerceDefaultImage(cfg.DefaultImage, cfg.AgentImage)
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

const defaultAgentImage = "ghcr.io/roundpenai/code-agent:0.1.0"

// PullableImage reports whether ref names an OCI image. The legacy default
// "host" and QEMU disk paths are not images, and pulling them fails.
func PullableImage(ref string) bool {
	ref = strings.TrimSpace(ref)
	if ref == "" || strings.EqualFold(ref, "host") {
		return false
	}
	return !strings.HasSuffix(strings.ToLower(ref), ".qcow2")
}

// CoerceDefaultImage maps a stored or env default onto an image the daemon
// can pull. Empty, "host", and *.qcow2 fall back to the Agent image.
func CoerceDefaultImage(image, agentImage string) string {
	if PullableImage(image) {
		return strings.TrimSpace(image)
	}
	if PullableImage(agentImage) {
		return strings.TrimSpace(agentImage)
	}
	return defaultAgentImage
}

// parsePreviewDomain reads ROUNDPEN_PREVIEW_DOMAIN, which names the zone that
// per-port preview subdomains live under: "rp.mk", or "https://rp.mk" to pin
// the scheme the reverse proxy terminates (console on plain HTTP, previews on
// HTTPS). A bad value stops the daemon instead of minting unreachable links.
func parsePreviewDomain(raw string) (host, scheme string, err error) {
	v := strings.TrimSpace(raw)
	if v == "" {
		return "", "", nil
	}
	if i := strings.Index(v, "://"); i >= 0 {
		scheme = strings.ToLower(v[:i])
		if scheme != "http" && scheme != "https" {
			return "", "", fmt.Errorf("ROUNDPEN_PREVIEW_DOMAIN: scheme must be http or https")
		}
		v = v[i+3:]
	}
	host = strings.ToLower(strings.TrimSuffix(v, "."))
	if i := strings.IndexAny(host, "/:?#"); i >= 0 {
		return "", "", fmt.Errorf("ROUNDPEN_PREVIEW_DOMAIN: %q must be a bare domain, without a port or path", raw)
	}
	if err := validateDomain(host); err != nil {
		return "", "", fmt.Errorf("ROUNDPEN_PREVIEW_DOMAIN: %w", err)
	}
	return host, scheme, nil
}

// validateDomain accepts a DNS name we can append preview labels to, so the
// links we mint are always resolvable hosts.
func validateDomain(host string) error {
	if len(host) > 253 {
		return fmt.Errorf("%q is longer than a domain name", host)
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 {
			return fmt.Errorf("%q has an empty or over-long label", host)
		}
		if strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return fmt.Errorf("%q has a label starting or ending with a hyphen", host)
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return fmt.Errorf("%q is not a lower-case letters, digits and hyphens domain", host)
			}
		}
	}
	return nil
}

// githubOAuthRelay is the shared official relay. Unset uses that host;
// off/none disables it so an install can bring its own GitHub App instead.
func githubOAuthRelay(raw string) string {
	v := strings.TrimRight(strings.TrimSpace(raw), "/")
	switch strings.ToLower(v) {
	case "", "default":
		return "https://auth.eaxi.com"
	case "off", "none", "false", "0":
		return ""
	default:
		return v
	}
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
