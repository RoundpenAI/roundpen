package settings_test

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/settings"
)

func TestDecodeAppSettingsKeepsFallbackLLMGW(t *testing.T) {
	fallback := settings.AppSettings{
		DefaultImage:           "host",
		DefaultTtlSeconds:      1800,
		PreviewTokenTtlSeconds: 900,
		LlmgwEnabled:           true,
		LlmgwOpenaiBaseURL:     "https://api.openai.com",
		LlmgwOpenaiAPIKey:      "sk-env",
		CDPProvider:            "docker",
		CDPPort:                9222,
	}
	got, err := settings.DecodeAppSettings([]byte(`{
		"allowPublicRegistration": true,
		"defaultImage": "python",
		"defaultTtlSeconds": 1200,
		"previewTokenTtlSeconds": 600
	}`), fallback)
	if err != nil {
		t.Fatal(err)
	}
	if !got.AllowPublicRegistration || got.DefaultImage != "python" {
		t.Fatalf("overlay: %+v", got)
	}
	if !got.LlmgwEnabled || got.LlmgwOpenaiAPIKey != "sk-env" {
		t.Fatalf("llmgw fallback lost: %+v", got)
	}
	if got.CDPProvider != "docker" || got.CDPPort != 9222 {
		t.Fatalf("cdp fallback lost: %+v", got)
	}
}

func TestDecodeAppSettingsMapsLegacyKaniko(t *testing.T) {
	got, err := settings.DecodeAppSettings([]byte(`{
		"defaultImage": "host",
		"defaultTtlSeconds": 1800,
		"previewTokenTtlSeconds": 900,
		"templateBuilder": "kaniko",
		"kanikoDestination": "git.example.com/roundpen"
	}`), settings.AppSettings{})
	if err != nil {
		t.Fatal(err)
	}
	if got.TemplateBuilder != "docker" {
		t.Fatalf("legacy kaniko should map to docker, got %q", got.TemplateBuilder)
	}
}

func TestDecodeAppSettingsHonorsExplicitLLMGW(t *testing.T) {
	fallback := settings.AppSettings{LlmgwEnabled: true, LlmgwOpenaiAPIKey: "sk-env"}
	got, err := settings.DecodeAppSettings([]byte(`{
		"defaultImage": "host",
		"defaultTtlSeconds": 1800,
		"previewTokenTtlSeconds": 900,
		"llmgwEnabled": false
	}`), fallback)
	if err != nil {
		t.Fatal(err)
	}
	if got.LlmgwEnabled {
		t.Fatal("explicit false should win")
	}
}
