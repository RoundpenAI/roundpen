package settings

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/config"
)

func TestWebSearchSettingsFromConfigAndApply(t *testing.T) {
	cfg := &config.Config{WebTools: config.WebToolsConfig{
		SearchEndpoint: "https://api.tavily.com",
		SearchAPIKey:   "tvly-env",
	}}
	s := FromConfig(cfg)
	if s.WebSearchEndpoint != "https://api.tavily.com" || s.WebSearchApiKey != "tvly-env" {
		t.Fatalf("FromConfig = %+v", s)
	}
	s.WebSearchEndpoint = "https://search.internal.example/"
	s.WebSearchApiKey = "tvly-new"
	if err := ApplyToConfig(&s, cfg); err != nil {
		t.Fatalf("ApplyToConfig: %v", err)
	}
	if cfg.WebTools.SearchEndpoint != "https://search.internal.example/" || cfg.WebTools.SearchAPIKey != "tvly-new" {
		t.Fatalf("cfg.WebTools = %+v", cfg.WebTools)
	}
}

func TestWebSearchSecretMaskAndMerge(t *testing.T) {
	s := AppSettings{WebSearchApiKey: "tvly-secret"}
	if got := s.SanitizeForResponse().WebSearchApiKey; got != SecretMask {
		t.Fatalf("masked = %q", got)
	}
	s.WebSearchApiKey = SecretMask
	s.MergeSecrets(AppSettings{WebSearchApiKey: "tvly-stored"})
	if s.WebSearchApiKey != "tvly-stored" {
		t.Fatalf("merge = %q", s.WebSearchApiKey)
	}
	s.WebSearchApiKey = ""
	s.MergeSecrets(AppSettings{WebSearchApiKey: "tvly-stored"})
	if s.WebSearchApiKey != "" {
		t.Fatalf("empty submit must clear the stored key, got %q", s.WebSearchApiKey)
	}
}

func TestWebSearchEndpointValidation(t *testing.T) {
	base := AppSettings{
		DefaultImage:           "ghcr.io/x/y:1",
		DefaultTtlSeconds:      60,
		PreviewTokenTtlSeconds: 60,
		TemplateBuilder:        "docker",
		CDPProvider:            "auto",
		CDPPort:                3000,
	}
	ok := base
	ok.WebSearchEndpoint = "https://api.tavily.com"
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid endpoint rejected: %v", err)
	}
	empty := base
	if err := empty.Validate(); err != nil {
		t.Fatalf("empty endpoint must be valid: %v", err)
	}
	bad := base
	bad.WebSearchEndpoint = "api.tavily.com"
	if err := bad.Validate(); err == nil {
		t.Fatal("endpoint without scheme must fail validation")
	}
}

func TestDecodeAppSettingsKeepsWebSearchFallback(t *testing.T) {
	fallback := AppSettings{WebSearchEndpoint: "https://api.tavily.com", WebSearchApiKey: "tvly-env"}
	got, err := DecodeAppSettings([]byte(`{"defaultImage":"img"}`), fallback)
	if err != nil {
		t.Fatalf("DecodeAppSettings: %v", err)
	}
	if got.WebSearchEndpoint != "https://api.tavily.com" || got.WebSearchApiKey != "tvly-env" {
		t.Fatalf("fallback lost: %+v", got)
	}
	got2, err := DecodeAppSettings([]byte(`{"webSearchApiKey":"tvly-db"}`), fallback)
	if err != nil {
		t.Fatalf("DecodeAppSettings: %v", err)
	}
	if got2.WebSearchApiKey != "tvly-db" {
		t.Fatalf("stored value not applied: %+v", got2)
	}
}

func TestWebSearchClearedKeyStaysClearedAcrossDecode(t *testing.T) {
	cfg := &config.Config{WebTools: config.WebToolsConfig{SearchEndpoint: "", SearchAPIKey: "tvly-env"}}
	seed := FromConfig(cfg)
	cleared := AppSettings{WebSearchEndpoint: "", WebSearchApiKey: ""}
	cleared.MergeSecrets(seed)
	if cleared.WebSearchApiKey != "" {
		t.Fatalf("cleared key must stay empty, got %q", cleared.WebSearchApiKey)
	}
	raw := []byte(`{"webSearchEndpoint":"","webSearchApiKey":""}`)
	got, err := DecodeAppSettings(raw, seed)
	if err != nil {
		t.Fatalf("DecodeAppSettings: %v", err)
	}
	if got.WebSearchApiKey != "" || got.WebSearchEndpoint != "" {
		t.Fatalf("stored empties must survive decode, got %+v", got)
	}
}
