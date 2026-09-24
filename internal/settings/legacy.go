package settings

import (
	"context"
	"encoding/json"

	"github.com/RoundpenAI/roundpen/internal/config"
)

// LegacyDocument holds the settings fields that moved to setting items. It is
// decoded from the stored payload so an install upgraded from an older release
// keeps its configuration; a fresh install has none of these keys and seeds
// items from the process config instead.
type LegacyDocument struct {
	Proxies               []ProxyProfile `json:"proxies"`
	LlmgwOpenaiBaseURL    string         `json:"llmgwOpenaiBaseUrl"`
	LlmgwOpenaiAPIKey     string         `json:"llmgwOpenaiApiKey"`
	LlmgwOpenaiProxy      string         `json:"llmgwOpenaiProxy"`
	LlmgwAnthropicBaseURL string         `json:"llmgwAnthropicBaseUrl"`
	LlmgwAnthropicAPIKey  string         `json:"llmgwAnthropicApiKey"`
	LlmgwAnthropicProxy   string         `json:"llmgwAnthropicProxy"`
	LlmgwDefaultModel     string         `json:"llmgwDefaultModel"`
	LlmgwEmbeddingModel   string         `json:"llmgwEmbeddingModel"`
	CDPProvider           string         `json:"cdpProvider"`
	CDPEndpoint           string         `json:"cdpEndpoint"`
	CDPToken              string         `json:"cdpToken"`
	CDPPort               int            `json:"cdpPort"`
	WebSearchEndpoint     string         `json:"webSearchEndpoint"`
	WebSearchApiKey       string         `json:"webSearchApiKey"`
	WebSearchProxy        string         `json:"webSearchProxy"`
	AutoMode              struct {
		Model string `json:"model"`
	} `json:"autoMode"`
}

// LoadLegacy reads the stored payload for the fields that became items. The
// payload is sealed the same way the live document is, so secrets are opened
// here too. A missing row yields an empty document.
func (s *Store) LoadLegacy(ctx context.Context) (LegacyDocument, error) {
	var raw []byte
	err := s.sql.QueryRowContext(ctx, `SELECT payload FROM app_settings WHERE id=$1`, globalID).Scan(&raw)
	if err != nil {
		return LegacyDocument{}, nil
	}
	var doc LegacyDocument
	if err := json.Unmarshal(raw, &doc); err != nil {
		return LegacyDocument{}, err
	}
	if s.box != nil {
		for _, f := range []*string{
			&doc.LlmgwOpenaiAPIKey,
			&doc.LlmgwAnthropicAPIKey,
			&doc.CDPToken,
			&doc.WebSearchApiKey,
		} {
			plain, err := s.box.Open(*f)
			if err != nil {
				// A master-key change blanks stored secrets elsewhere too; the
				// affected item is re-imported without a credential.
				*f = ""
				continue
			}
			*f = plain
		}
	}
	return doc, nil
}

// FromConfigLegacy mirrors the environment into the legacy shape, so a fresh
// install seeds the same items an upgraded one would.
func FromConfigLegacy(cfg *config.Config) LegacyDocument {
	if cfg == nil {
		return LegacyDocument{}
	}
	doc := LegacyDocument{
		LlmgwDefaultModel:   cfg.LLMGW.DefaultModel,
		LlmgwEmbeddingModel: cfg.LLMGW.EmbeddingModel,
		CDPProvider:         cfg.CDP.Provider,
		CDPEndpoint:         cfg.CDP.Endpoint,
		CDPToken:            cfg.CDP.Token,
		CDPPort:             cfg.CDP.Port,
		WebSearchEndpoint:   cfg.WebTools.SearchEndpoint,
		WebSearchApiKey:     cfg.WebTools.SearchAPIKey,
		WebSearchProxy:      cfg.WebTools.SearchProxyURL,
	}
	if cfg.LLMGW.OpenAI != nil {
		doc.LlmgwOpenaiBaseURL = cfg.LLMGW.OpenAI.BaseURL
		doc.LlmgwOpenaiAPIKey = cfg.LLMGW.OpenAI.APIKey
		doc.LlmgwOpenaiProxy = cfg.LLMGW.OpenAI.ProxyURL
	}
	if cfg.LLMGW.Anthropic != nil {
		doc.LlmgwAnthropicBaseURL = cfg.LLMGW.Anthropic.BaseURL
		doc.LlmgwAnthropicAPIKey = cfg.LLMGW.Anthropic.APIKey
		doc.LlmgwAnthropicProxy = cfg.LLMGW.Anthropic.ProxyURL
	}
	return doc
}

// Overlay fills unset fields from fallback (the environment), so the stored
// document wins where it has a value.
func (d LegacyDocument) Overlay(fallback LegacyDocument) LegacyDocument {
	out := d
	if out.LlmgwOpenaiBaseURL == "" && out.LlmgwOpenaiAPIKey == "" {
		out.LlmgwOpenaiBaseURL, out.LlmgwOpenaiAPIKey = fallback.LlmgwOpenaiBaseURL, fallback.LlmgwOpenaiAPIKey
		out.LlmgwOpenaiProxy = fallback.LlmgwOpenaiProxy
	}
	if out.LlmgwAnthropicBaseURL == "" && out.LlmgwAnthropicAPIKey == "" {
		out.LlmgwAnthropicBaseURL, out.LlmgwAnthropicAPIKey = fallback.LlmgwAnthropicBaseURL, fallback.LlmgwAnthropicAPIKey
		out.LlmgwAnthropicProxy = fallback.LlmgwAnthropicProxy
	}
	if out.LlmgwDefaultModel == "" {
		out.LlmgwDefaultModel = fallback.LlmgwDefaultModel
	}
	if out.LlmgwEmbeddingModel == "" {
		out.LlmgwEmbeddingModel = fallback.LlmgwEmbeddingModel
	}
	if out.CDPProvider == "" {
		out.CDPProvider = fallback.CDPProvider
	}
	if out.CDPEndpoint == "" {
		out.CDPEndpoint = fallback.CDPEndpoint
	}
	if out.CDPToken == "" {
		out.CDPToken = fallback.CDPToken
	}
	if out.CDPPort == 0 {
		out.CDPPort = fallback.CDPPort
	}
	if out.WebSearchEndpoint == "" {
		out.WebSearchEndpoint = fallback.WebSearchEndpoint
	}
	if out.WebSearchApiKey == "" {
		out.WebSearchApiKey = fallback.WebSearchApiKey
	}
	if out.WebSearchProxy == "" {
		out.WebSearchProxy = fallback.WebSearchProxy
	}
	if len(out.Proxies) == 0 {
		out.Proxies = fallback.Proxies
	}
	return out
}
