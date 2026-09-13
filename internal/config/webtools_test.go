package config

import "testing"

func TestLoadWebToolsEmpty(t *testing.T) {
	t.Setenv("ROUNDPEN_WEB_SEARCH_ENDPOINT", "")
	t.Setenv("ROUNDPEN_WEB_SEARCH_API_KEY", "")
	cfg := loadWebTools()
	if cfg.SearchEndpoint != "" || cfg.SearchAPIKey != "" {
		t.Fatalf("expected empty web tools config, got %+v", cfg)
	}
}

func TestLoadWebToolsFromEnv(t *testing.T) {
	t.Setenv("ROUNDPEN_WEB_SEARCH_ENDPOINT", "https://search.internal.example/")
	t.Setenv("ROUNDPEN_WEB_SEARCH_API_KEY", " tvly-x ")
	cfg := loadWebTools()
	if cfg.SearchEndpoint != "https://search.internal.example/" {
		t.Fatalf("endpoint = %q", cfg.SearchEndpoint)
	}
	if cfg.SearchAPIKey != "tvly-x" {
		t.Fatalf("key = %q", cfg.SearchAPIKey)
	}
}
