// Package settings persists mutable Roundpen configuration in PostgreSQL.
package settings

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RoundpenAI/roundpen/internal/automode"
	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/config"
)

const globalID = "global"

// AppSettings are admin-editable values stored in PostgreSQL. Integrations
// (LLM providers, proxies, search backends, browser sources) live as setting
// items instead; see internal/settingitems.
type AppSettings struct {
	AllowPublicRegistration bool   `json:"allowPublicRegistration"`
	DefaultImage            string `json:"defaultImage"`
	DefaultTtlSeconds       int    `json:"defaultTtlSeconds"`
	// AgentImage pins the Agent sandbox image, overriding the agent template's
	// own image while the template still supplies its resources. Empty keeps
	// the template in charge.
	AgentImage string `json:"agentImage"`
	LlmgwEnabled            bool   `json:"llmgwEnabled"`
	LlmgwPublicURL          string `json:"llmgwPublicUrl"`
	LlmgwLogBodyMaxBytes    int    `json:"llmgwLogBodyMaxBytes"`
	LlmgwVirtualKeys        string `json:"llmgwVirtualKeys"`
	// AutoMode configures the policy classifier behind the chat Auto toggle.
	AutoMode AutoModeSettings `json:"autoMode"`
}

// AutoModeSettings holds the admin-configured auto-mode policy. Lists carry
// prose rules read by the classifier; the "$defaults" token splices in the
// built-in rules at its position, and a non-empty list without the token
// replaces them.
type AutoModeSettings struct {
	Environment []string `json:"environment,omitempty"`
	Allow       []string `json:"allow,omitempty"`
	SoftDeny    []string `json:"softDeny,omitempty"`
	HardDeny    []string `json:"hardDeny,omitempty"`
}

// Rules converts stored lists into classifier rules; "$defaults" expands at
// evaluation time.
func (a AutoModeSettings) Rules() automode.Rules {
	return automode.Rules{
		Environment: a.Environment,
		Allow:       a.Allow,
		SoftDeny:    a.SoftDeny,
		HardDeny:    a.HardDeny,
	}
}

const (
	autoModeMaxEntries    = 50
	autoModeMaxEntryRunes = 800
)

func defaultAutoModeSettings() AutoModeSettings {
	return AutoModeSettings{
		Environment: []string{automode.DefaultsToken},
		Allow:       []string{automode.DefaultsToken},
		SoftDeny:    []string{automode.DefaultsToken},
		HardDeny:    []string{automode.DefaultsToken},
	}
}

// normalize trims free-text values before validation.
func (s *AppSettings) normalize() {
	s.AgentImage = strings.TrimSpace(s.AgentImage)
	s.normalizeAutoMode()
}

func (s *AppSettings) normalizeAutoMode() {
	s.AutoMode.Environment = normalizeAutoModeList(s.AutoMode.Environment)
	s.AutoMode.Allow = normalizeAutoModeList(s.AutoMode.Allow)
	s.AutoMode.SoftDeny = normalizeAutoModeList(s.AutoMode.SoftDeny)
	s.AutoMode.HardDeny = normalizeAutoModeList(s.AutoMode.HardDeny)
}

func normalizeAutoModeList(list []string) []string {
	out := make([]string, 0, len(list))
	seen := map[string]struct{}{}
	for _, e := range list {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if _, dup := seen[e]; dup {
			continue
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	return out
}

// ProxyProfile is a named egress proxy from the pre-item settings document.
// New installs configure proxies as setting items; this type remains only for
// the one-time import of an older document (see LegacyDocument).
type ProxyProfile struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
}

// SystemInfo is read-only infrastructure metadata for the settings UI.
type SystemInfo struct {
	Backend            string `json:"backend"`
	DockerHost         string `json:"dockerHost"`
	DataRoot           string `json:"dataRoot"`
	HTTPAddr           string `json:"httpAddr"`
	LlmgwActive        bool   `json:"llmgwActive"`
	LlmgwMounted       bool   `json:"llmgwMounted"`
	CDPProviderActive  string `json:"cdpProviderActive"`
	CDPHostChromeFound bool   `json:"cdpHostChromeFound"`
	CDPHint            string `json:"cdpHint,omitempty"`
}

// FromConfig extracts DB-backed settings from process config.
func FromConfig(cfg *config.Config) AppSettings {
	return AppSettings{
		AllowPublicRegistration: cfg.AllowPublicRegistration,
		DefaultImage:            cfg.DefaultImage,
		DefaultTtlSeconds:       int(cfg.DefaultTTL / time.Second),
		LlmgwEnabled:            cfg.LLMGW.Enabled,
		LlmgwPublicURL:          cfg.LLMGW.PublicURL,
		LlmgwLogBodyMaxBytes:    cfg.LLMGW.LogBodyMaxBytes,
		LlmgwVirtualKeys:        config.FormatVirtualKeys(cfg.LLMGW.VirtualKeys),
		AutoMode:                defaultAutoModeSettings(),
	}
}

// SanitizeForResponse masks secrets before returning settings to clients.
func (s AppSettings) SanitizeForResponse() AppSettings {
	out := s
	out.LlmgwVirtualKeys = MaskVirtualKeysSetting(s.LlmgwVirtualKeys)
	return out
}

// MergeSecrets preserves stored API keys when the client leaves them masked.
func (s *AppSettings) MergeSecrets(previous AppSettings) {
	s.LlmgwVirtualKeys = ResolveVirtualKeysSetting(s.LlmgwVirtualKeys, previous.LlmgwVirtualKeys)
}

// ApplyToConfig writes settings into the in-memory process config. Provider
// endpoints and the browser source are not part of the document any more:
// they live as items and reach their subsystems through the catalog.
func ApplyToConfig(s *AppSettings, cfg *config.Config) error {
	cfg.AllowPublicRegistration = s.AllowPublicRegistration
	cfg.DefaultImage = strings.TrimSpace(s.DefaultImage)
	cfg.DefaultTTL = time.Duration(s.DefaultTtlSeconds) * time.Second
	cfg.LLMGW.Enabled = s.LlmgwEnabled
	cfg.LLMGW.PublicURL = strings.TrimSpace(s.LlmgwPublicURL)
	cfg.LLMGW.LogBodyMaxBytes = s.LlmgwLogBodyMaxBytes
	keys, err := config.ParseVirtualKeys(s.LlmgwVirtualKeys)
	if err != nil {
		return err
	}
	cfg.LLMGW.VirtualKeys = keys
	return nil
}

// Validate checks user-editable settings.
func (s AppSettings) Validate() error {
	if strings.TrimSpace(s.DefaultImage) == "" {
		return fmt.Errorf("defaultImage is required")
	}
	if err := validateAgentImage(s.AgentImage); err != nil {
		return err
	}
	if s.DefaultTtlSeconds <= 0 {
		return fmt.Errorf("defaultTtlSeconds must be positive")
	}
	if s.LlmgwLogBodyMaxBytes < -1 {
		return fmt.Errorf("llmgwLogBodyMaxBytes must be >= -1")
	}
	if _, err := config.ParseVirtualKeys(s.LlmgwVirtualKeys); err != nil {
		return err
	}
	ruleLists := []struct {
		field string
		list  []string
	}{
		{"autoMode.environment", s.AutoMode.Environment},
		{"autoMode.allow", s.AutoMode.Allow},
		{"autoMode.softDeny", s.AutoMode.SoftDeny},
		{"autoMode.hardDeny", s.AutoMode.HardDeny},
	}
	for _, r := range ruleLists {
		if len(r.list) > autoModeMaxEntries {
			return fmt.Errorf("%s: at most %d entries", r.field, autoModeMaxEntries)
		}
		for _, e := range r.list {
			if utf8.RuneCountInString(e) > autoModeMaxEntryRunes {
				return fmt.Errorf("%s: entry exceeds %d characters", r.field, autoModeMaxEntryRunes)
			}
		}
	}
	return nil
}

const agentImageMaxLen = 300

// validateAgentImage accepts an OCI image reference typed by an admin. The
// Agent slot is Docker-only, so qcow2 disks and pasted URLs are rejected here
// with a readable message instead of failing later at sandbox creation.
func validateAgentImage(ref string) error {
	if ref == "" {
		return nil
	}
	if len(ref) > agentImageMaxLen {
		return fmt.Errorf("agentImage is too long (max %d characters)", agentImageMaxLen)
	}
	if strings.Contains(ref, "://") {
		return fmt.Errorf("agentImage must be an image reference, not a URL (e.g. ghcr.io/roundpenai/code-agent:0.1.0)")
	}
	if strings.ContainsAny(ref, " \t\r\n") {
		return fmt.Errorf("agentImage must not contain whitespace")
	}
	if strings.HasPrefix(ref, "-") {
		return fmt.Errorf("agentImage must not start with a dash")
	}
	if strings.HasSuffix(strings.ToLower(ref), ".qcow2") {
		return fmt.Errorf("agentImage must be an OCI image; the Agent slot runs Docker only")
	}
	return nil
}

// SystemFromConfig returns read-only system metadata.
func SystemFromConfig(cfg *config.Config, llmgwMounted bool) SystemInfo {
	return SystemInfo{
		Backend:            cfg.Backend,
		DockerHost:         cfg.DockerHost,
		DataRoot:           cfg.EffectiveDataRoot(),
		HTTPAddr:           cfg.HTTPAddr,
		LlmgwActive:        cfg.LLMGW.Enabled,
		LlmgwMounted:       llmgwMounted,
		CDPProviderActive:  config.ResolveCDPProvider(cfg, browser.ChromeOnPATH()),
		CDPHostChromeFound: browser.ChromeOnPATH(),
		CDPHint:            config.CDPHint(cfg, browser.ChromeOnPATH()),
	}
}

func llmgwUpstreamBase(u *config.LLMGWUpstream) string {
	if u == nil {
		return ""
	}
	return u.BaseURL
}

func llmgwUpstreamKey(u *config.LLMGWUpstream) string {
	if u == nil {
		return ""
	}
	return u.APIKey
}

func llmgwUpstreamProxy(u *config.LLMGWUpstream) string {
	if u == nil {
		return ""
	}
	return u.ProxyURL
}

// ValidateProxyURL accepts empty (direct) or an http/https/socks5 proxy URL.
func ValidateProxyURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("proxy URL %q is not a valid URL", raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5", "socks5h":
		return nil
	default:
		return fmt.Errorf("proxy URL %q must use http, https, socks5 or socks5h", raw)
	}
}
