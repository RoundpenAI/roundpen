// Package settings persists mutable Roundpen configuration in PostgreSQL.
package settings

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/template"
)

const globalID = "global"

// AppSettings are admin-editable values stored in PostgreSQL.
type AppSettings struct {
	AllowPublicRegistration bool   `json:"allowPublicRegistration"`
	DefaultImage            string `json:"defaultImage"`
	DefaultTtlSeconds       int    `json:"defaultTtlSeconds"`
	PreviewPublicURL        string `json:"previewPublicUrl"`
	PreviewTokenTtlSeconds  int    `json:"previewTokenTtlSeconds"`
	TemplateBuilder         string `json:"templateBuilder"`
	LlmgwEnabled            bool   `json:"llmgwEnabled"`
	LlmgwPublicURL          string `json:"llmgwPublicUrl"`
	LlmgwLogBodyMaxBytes    int    `json:"llmgwLogBodyMaxBytes"`
	LlmgwEmbeddingModel     string `json:"llmgwEmbeddingModel"`
	LlmgwDefaultModel       string `json:"llmgwDefaultModel"`
	LlmgwOpenaiBaseURL      string `json:"llmgwOpenaiBaseUrl"`
	LlmgwOpenaiAPIKey       string `json:"llmgwOpenaiApiKey"`
	LlmgwAnthropicBaseURL   string `json:"llmgwAnthropicBaseUrl"`
	LlmgwAnthropicAPIKey    string `json:"llmgwAnthropicApiKey"`
	LlmgwVirtualKeys        string `json:"llmgwVirtualKeys"`
	CDPProvider             string `json:"cdpProvider"`
	CDPEndpoint             string `json:"cdpEndpoint"`
	CDPToken                string `json:"cdpToken"`
	CDPPort                 int    `json:"cdpPort"`
	WebSearchEndpoint       string `json:"webSearchEndpoint"`
	WebSearchApiKey         string `json:"webSearchApiKey"`
}

// SystemInfo is read-only infrastructure metadata for the settings UI.
type SystemInfo struct {
	Backend               string `json:"backend"`
	DockerHost            string `json:"dockerHost"`
	DataRoot              string `json:"dataRoot"`
	HTTPAddr              string `json:"httpAddr"`
	TemplateBuilderActive string `json:"templateBuilderActive"`
	TemplateBuilderHint   string `json:"templateBuilderHint,omitempty"`
	LlmgwActive           bool   `json:"llmgwActive"`
	LlmgwMounted          bool   `json:"llmgwMounted"`
	CDPProviderActive     string `json:"cdpProviderActive"`
	CDPHostChromeFound    bool   `json:"cdpHostChromeFound"`
	CDPHint               string `json:"cdpHint,omitempty"`
}

// FromConfig extracts DB-backed settings from process config.
func FromConfig(cfg *config.Config) AppSettings {
	out := AppSettings{
		AllowPublicRegistration: cfg.AllowPublicRegistration,
		DefaultImage:            cfg.DefaultImage,
		DefaultTtlSeconds:       int(cfg.DefaultTTL / time.Second),
		PreviewPublicURL:        cfg.PreviewPublicURL,
		PreviewTokenTtlSeconds:  int(cfg.PreviewTokenTTL / time.Second),
		TemplateBuilder:         cfg.TemplateBuilder,
		LlmgwEnabled:            cfg.LLMGW.Enabled,
		LlmgwPublicURL:          cfg.LLMGW.PublicURL,
		LlmgwLogBodyMaxBytes:    cfg.LLMGW.LogBodyMaxBytes,
		LlmgwEmbeddingModel:     cfg.LLMGW.EmbeddingModel,
		LlmgwDefaultModel:       cfg.LLMGW.DefaultModel,
		LlmgwOpenaiBaseURL:      llmgwUpstreamBase(cfg.LLMGW.OpenAI),
		LlmgwOpenaiAPIKey:       llmgwUpstreamKey(cfg.LLMGW.OpenAI),
		LlmgwAnthropicBaseURL:   llmgwUpstreamBase(cfg.LLMGW.Anthropic),
		LlmgwAnthropicAPIKey:    llmgwUpstreamKey(cfg.LLMGW.Anthropic),
		LlmgwVirtualKeys:        config.FormatVirtualKeys(cfg.LLMGW.VirtualKeys),
		CDPProvider:             cfg.CDP.Provider,
		CDPEndpoint:             cfg.CDP.Endpoint,
		CDPToken:                cfg.CDP.Token,
		CDPPort:                 cfg.CDP.Port,
		WebSearchEndpoint:       cfg.WebTools.SearchEndpoint,
		WebSearchApiKey:         cfg.WebTools.SearchAPIKey,
	}
	out.normalizeCDP()
	return out
}

func (s *AppSettings) normalizeCDP() {
	if strings.TrimSpace(s.CDPProvider) == "" {
		s.CDPProvider = config.CDPProviderAuto
	}
	if s.CDPPort <= 0 {
		s.CDPPort = config.DefaultCDPPort
	}
}

// SanitizeForResponse masks secrets before returning settings to clients.
func (s AppSettings) SanitizeForResponse() AppSettings {
	out := s
	out.LlmgwOpenaiAPIKey = MaskSecret(s.LlmgwOpenaiAPIKey)
	out.LlmgwAnthropicAPIKey = MaskSecret(s.LlmgwAnthropicAPIKey)
	out.LlmgwVirtualKeys = MaskVirtualKeysSetting(s.LlmgwVirtualKeys)
	out.CDPToken = MaskSecret(s.CDPToken)
	out.WebSearchApiKey = MaskSecret(s.WebSearchApiKey)
	return out
}

// MergeSecrets preserves stored API keys when the client leaves them masked.
func (s *AppSettings) MergeSecrets(previous AppSettings) {
	s.LlmgwOpenaiAPIKey = ResolveSecret(s.LlmgwOpenaiAPIKey, previous.LlmgwOpenaiAPIKey)
	s.LlmgwAnthropicAPIKey = ResolveSecret(s.LlmgwAnthropicAPIKey, previous.LlmgwAnthropicAPIKey)
	s.LlmgwVirtualKeys = ResolveVirtualKeysSetting(s.LlmgwVirtualKeys, previous.LlmgwVirtualKeys)
	s.CDPToken = ResolveSecret(s.CDPToken, previous.CDPToken)
	// Web 搜索密钥例外于其它密钥字段：提交掩码 = 保持原值，提交空串 = 显式清除
	// （设置页清空输入框即可关闭 WebSearch）。JSON 中缺省的字段在
	// DecodeAppSettings 阶段已回填当前值，不会走到这里被清空。
	if s.WebSearchApiKey == SecretMask {
		s.WebSearchApiKey = previous.WebSearchApiKey
	}
}

// ApplyToConfig writes settings into the in-memory process config.
func ApplyToConfig(s *AppSettings, cfg *config.Config) error {
	cfg.AllowPublicRegistration = s.AllowPublicRegistration
	cfg.DefaultImage = strings.TrimSpace(s.DefaultImage)
	cfg.DefaultTTL = time.Duration(s.DefaultTtlSeconds) * time.Second
	cfg.PreviewPublicURL = strings.TrimSpace(s.PreviewPublicURL)
	cfg.PreviewTokenTTL = time.Duration(s.PreviewTokenTtlSeconds) * time.Second
	cfg.TemplateBuilder = strings.ToLower(strings.TrimSpace(s.TemplateBuilder))
	if err := config.ApplyLLMGWSettings(
		&cfg.LLMGW,
		s.LlmgwEnabled,
		s.LlmgwPublicURL,
		s.LlmgwEmbeddingModel,
		s.LlmgwDefaultModel,
		s.LlmgwLogBodyMaxBytes,
		s.LlmgwOpenaiBaseURL,
		s.LlmgwOpenaiAPIKey,
		s.LlmgwAnthropicBaseURL,
		s.LlmgwAnthropicAPIKey,
		s.LlmgwVirtualKeys,
	); err != nil {
		return err
	}
	cfg.WebTools = config.WebToolsConfig{
		SearchEndpoint: strings.TrimSpace(s.WebSearchEndpoint),
		SearchAPIKey:   strings.TrimSpace(s.WebSearchApiKey),
	}
	cfg.CDP = config.CDPConfig{
		Provider: s.CDPProvider,
		Endpoint: s.CDPEndpoint,
		Token:    s.CDPToken,
		Port:     s.CDPPort,
	}
	return config.NormalizeCDP(&cfg.CDP)
}

// Validate checks user-editable settings.
func (s AppSettings) Validate() error {
	if strings.TrimSpace(s.DefaultImage) == "" {
		return fmt.Errorf("defaultImage is required")
	}
	if s.DefaultTtlSeconds <= 0 {
		return fmt.Errorf("defaultTtlSeconds must be positive")
	}
	if s.PreviewTokenTtlSeconds <= 0 {
		return fmt.Errorf("previewTokenTtlSeconds must be positive")
	}
	switch strings.ToLower(strings.TrimSpace(s.TemplateBuilder)) {
	case "", "auto", "docker", "ci", "disabled":
	default:
		return fmt.Errorf("templateBuilder must be auto, docker, ci, disabled, or empty")
	}
	if s.LlmgwLogBodyMaxBytes < -1 {
		return fmt.Errorf("llmgwLogBodyMaxBytes must be >= -1")
	}
	openaiBase := strings.TrimSpace(s.LlmgwOpenaiBaseURL)
	openaiKey := strings.TrimSpace(s.LlmgwOpenaiAPIKey)
	if openaiBase != "" || openaiKey != "" {
		if openaiBase == "" || openaiKey == "" {
			return fmt.Errorf("llmgwOpenaiBaseUrl and llmgwOpenaiApiKey must both be set")
		}
	}
	anthropicBase := strings.TrimSpace(s.LlmgwAnthropicBaseURL)
	anthropicKey := strings.TrimSpace(s.LlmgwAnthropicAPIKey)
	if anthropicBase != "" || anthropicKey != "" {
		if anthropicBase == "" || anthropicKey == "" {
			return fmt.Errorf("llmgwAnthropicBaseUrl and llmgwAnthropicApiKey must both be set")
		}
	}
	if ep := strings.TrimSpace(s.WebSearchEndpoint); ep != "" {
		u, err := url.Parse(ep)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("webSearchEndpoint must be an http(s) URL")
		}
	}
	if _, err := config.ParseVirtualKeys(s.LlmgwVirtualKeys); err != nil {
		return err
	}
	s.normalizeCDP()
	probe := config.CDPConfig{
		Provider: s.CDPProvider,
		Endpoint: s.CDPEndpoint,
		Token:    s.CDPToken,
		Port:     s.CDPPort,
	}
	return config.NormalizeCDP(&probe)
}

// SystemFromConfig returns read-only system metadata.
func SystemFromConfig(cfg *config.Config, llmgwMounted bool) SystemInfo {
	active := cfg.ResolveTemplateBuilder()
	info := SystemInfo{
		Backend:               cfg.Backend,
		DockerHost:            cfg.DockerHost,
		DataRoot:              cfg.EffectiveDataRoot(),
		HTTPAddr:              cfg.HTTPAddr,
		TemplateBuilderActive: active,
		LlmgwActive:           cfg.LLMGW.Enabled,
		LlmgwMounted:          llmgwMounted,
		CDPProviderActive:     config.ResolveCDPProvider(cfg, browser.ChromeOnPATH()),
		CDPHostChromeFound:    browser.ChromeOnPATH(),
		CDPHint:               config.CDPHint(cfg, browser.ChromeOnPATH()),
	}
	if active == "" {
		info.TemplateBuilderHint = template.BuilderUnavailableHint(cfg)
	}
	return info
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
