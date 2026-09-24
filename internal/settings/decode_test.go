package settings_test

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/settings"
)

func TestDecodeAppSettingsKeepsFallback(t *testing.T) {
	fallback := settings.AppSettings{
		DefaultImage:      "host",
		DefaultTtlSeconds: 1800,
		LlmgwEnabled:      true,
		LlmgwVirtualKeys:  "vk-env",
	}
	got, err := settings.DecodeAppSettings([]byte(`{
		"allowPublicRegistration": true,
		"defaultImage": "python",
		"defaultTtlSeconds": 1200
	}`), fallback)
	if err != nil {
		t.Fatal(err)
	}
	if !got.AllowPublicRegistration || got.DefaultImage != "python" {
		t.Fatalf("overlay: %+v", got)
	}
	if !got.LlmgwEnabled || got.LlmgwVirtualKeys != "vk-env" {
		t.Fatalf("llmgw fallback lost: %+v", got)
	}
}

// TestDecodeAppSettingsIgnoresRetiredKeys proves a document written by an older
// release still loads: the integration fields it carries are ignored here and
// read separately for the one-time item import.
func TestDecodeAppSettingsIgnoresRetiredKeys(t *testing.T) {
	raw := []byte(`{
		"defaultImage": "host",
		"defaultTtlSeconds": 1800,
		"cdpProvider": "remote",
		"webSearchEndpoint": "https://search.example",
		"proxies": [{"id": "us", "name": "US", "url": "socks5://10.0.0.9:1080"}],
		"llmgwOpenaiBaseUrl": "https://api.openai.com"
	}`)
	got, err := settings.DecodeAppSettings(raw, settings.AppSettings{})
	if err != nil {
		t.Fatal(err)
	}
	if got.DefaultImage != "host" || got.DefaultTtlSeconds != 1800 {
		t.Fatalf("surviving fields: %+v", got)
	}
}

func TestDecodeAppSettingsHonorsExplicitLLMGW(t *testing.T) {
	fallback := settings.AppSettings{LlmgwEnabled: true, LlmgwVirtualKeys: "vk-env"}
	got, err := settings.DecodeAppSettings([]byte(`{
		"defaultImage": "host",
		"defaultTtlSeconds": 1800,
		"llmgwEnabled": false
	}`), fallback)
	if err != nil {
		t.Fatal(err)
	}
	if got.LlmgwEnabled {
		t.Fatal("explicit false should win")
	}
}
