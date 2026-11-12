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
	"github.com/RoundpenAI/roundpen/internal/buildinfo"
	"github.com/RoundpenAI/roundpen/internal/config"
)

const globalID = "global"

// AppSettings are admin-editable values stored in PostgreSQL. Integrations
// (LLM providers, proxies, search backends, browser sources) live as setting
// items instead; see internal/settingitems.
type AppSettings struct {
	// AllowPublicRegistration is retained for document compatibility; the live
	// gate is ROUNDPEN_ALLOW_PUBLIC_REGISTRATION only (see ApplyToConfig).
	AllowPublicRegistration bool   `json:"allowPublicRegistration"`
	DefaultImage            string `json:"defaultImage"`
	DefaultTtlSeconds       int    `json:"defaultTtlSeconds"`
	// AgentImage pins the Agent sandbox image, overriding the agent template's
	// own image while the template still supplies its resources. Empty keeps
	// the template in charge.
	AgentImage           string `json:"agentImage"`
	LlmgwEnabled         bool   `json:"llmgwEnabled"`
	LlmgwPublicURL       string `json:"llmgwPublicUrl"`
	LlmgwLogBodyMaxBytes int    `json:"llmgwLogBodyMaxBytes"`
	LlmgwVirtualKeys     string `json:"llmgwVirtualKeys"`
	// AutoMode configures the policy classifier behind the chat Auto toggle.
	AutoMode AutoModeSettings `json:"autoMode"`
	// FeedbackFormURL is a Feishu (or other) intelligent-form link opened from
	// the console chrome. Empty hides the feedback entry.
	FeedbackFormURL string `json:"feedbackFormUrl"`
	// SuggestedPrompts are server-delivered starter chips keyed by assistant kind.
	SuggestedPrompts SuggestedPrompts `json:"suggestedPrompts"`
	// EnabledPlugins lists optional instance capabilities (see internal/plugins).
	// Omitted from a settings PUT keeps the stored set.
	EnabledPlugins []string `json:"enabledPlugins,omitempty"`
}

// SuggestedPrompts holds admin-configured empty-session starter prompts.
type SuggestedPrompts struct {
	Global []string            `json:"global,omitempty"`
	ByKind map[string][]string `json:"byKind,omitempty"`
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

	suggestedPromptMaxEntries    = 5
	suggestedPromptMaxEntryRunes = 200
	feedbackFormURLMaxLen        = 2000
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
	s.FeedbackFormURL = strings.TrimSpace(s.FeedbackFormURL)
	// Relay cannot be turned off from settings (or the API document).
	s.LlmgwEnabled = true
	s.normalizeAutoMode()
	s.SuggestedPrompts = normalizeSuggestedPrompts(s.SuggestedPrompts)
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

func normalizeSuggestedPrompts(p SuggestedPrompts) SuggestedPrompts {
	p.Global = normalizePromptList(p.Global)
	if len(p.ByKind) == 0 {
		p.ByKind = nil
		return p
	}
	next := make(map[string][]string, len(p.ByKind))
	for k, list := range p.ByKind {
		k = strings.TrimSpace(strings.ToLower(k))
		if k == "" {
			continue
		}
		list = normalizePromptList(list)
		if len(list) == 0 {
			continue
		}
		next[k] = list
	}
	if len(next) == 0 {
		p.ByKind = nil
	} else {
		p.ByKind = next
	}
	return p
}

func normalizePromptList(list []string) []string {
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
		if len(out) >= suggestedPromptMaxEntries {
			break
		}
	}
	return out
}

// ResolveSuggestedPrompts picks kind-specific prompts, then global.
func ResolveSuggestedPrompts(p SuggestedPrompts, kind string) []string {
	kind = strings.TrimSpace(strings.ToLower(kind))
	if kind != "" && p.ByKind != nil {
		if list := normalizePromptList(p.ByKind[kind]); len(list) > 0 {
			return list
		}
	}
	return normalizePromptList(p.Global)
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
	Version            string `json:"version,omitempty"`
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
	// Public registration is env-only (ROUNDPEN_ALLOW_PUBLIC_REGISTRATION);
	// the stored field is kept for document compatibility and ignored here.
	cfg.DefaultImage = config.CoerceDefaultImage(s.DefaultImage, cfg.AgentImage)
	cfg.DefaultTTL = time.Duration(s.DefaultTtlSeconds) * time.Second
	// Relay is always on; body logging stays on the env-loaded value
	// (ROUNDPEN_LLMGW_LOG_BODY_MAX_BYTES) and is not editable via settings.
	cfg.LLMGW.Enabled = true
	cfg.LLMGW.PublicURL = strings.TrimSpace(s.LlmgwPublicURL)
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
	if err := validateFeedbackFormURL(s.FeedbackFormURL); err != nil {
		return err
	}
	if err := validateSuggestedPrompts(s.SuggestedPrompts); err != nil {
		return err
	}
	return nil
}

func validateFeedbackFormURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if len(raw) > feedbackFormURLMaxLen {
		return fmt.Errorf("feedbackFormUrl is too long (max %d characters)", feedbackFormURLMaxLen)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("feedbackFormUrl must be a valid URL")
	}
	if scheme := strings.ToLower(u.Scheme); scheme != "https" && scheme != "http" {
		return fmt.Errorf("feedbackFormUrl must use http or https")
	}
	return nil
}

func validateSuggestedPrompts(p SuggestedPrompts) error {
	if err := validatePromptList("suggestedPrompts.global", p.Global); err != nil {
		return err
	}
	for k, list := range p.ByKind {
		k = strings.TrimSpace(strings.ToLower(k))
		switch k {
		case "user", "system", "coordinator":
		default:
			return fmt.Errorf("suggestedPrompts.byKind: unknown kind %q", k)
		}
		if err := validatePromptList("suggestedPrompts.byKind."+k, list); err != nil {
			return err
		}
	}
	return nil
}

func validatePromptList(field string, list []string) error {
	if len(list) > suggestedPromptMaxEntries {
		return fmt.Errorf("%s: at most %d entries", field, suggestedPromptMaxEntries)
	}
	for _, e := range list {
		if utf8.RuneCountInString(e) > suggestedPromptMaxEntryRunes {
			return fmt.Errorf("%s: entry exceeds %d characters", field, suggestedPromptMaxEntryRunes)
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
		Version:            buildinfo.Current(),
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
