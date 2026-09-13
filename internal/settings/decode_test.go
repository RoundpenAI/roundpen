package settings_test

import (
	"fmt"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/config"
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
	// The docker fallback keeps its provider, but the legacy 9222 port is
	// migrated to the browserless default.
	if got.CDPProvider != "docker" || got.CDPPort != config.DefaultCDPPort {
		t.Fatalf("cdp fallback lost: %+v", got)
	}
}

// TestDecodeAppSettingsMigratesLegacyCDPPort proves rows written before the
// browserless move (port 9222) are repaired for the providers that own the
// managed container, while an explicit external provider keeps its port.
func TestDecodeAppSettingsMigratesLegacyCDPPort(t *testing.T) {
	cases := []struct {
		name     string
		provider string
		wantPort int
	}{
		{"empty provider", "", config.DefaultCDPPort},
		{"auto provider", config.CDPProviderAuto, config.DefaultCDPPort},
		{"docker provider", config.CDPProviderDocker, config.DefaultCDPPort},
		{"remote provider keeps its port", config.CDPProviderRemote, 9222},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := fmt.Sprintf(`{
				"defaultImage": "host",
				"defaultTtlSeconds": 1800,
				"previewTokenTtlSeconds": 900,
				"cdpProvider": %q,
				"cdpPort": 9222
			}`, tc.provider)
			got, err := settings.DecodeAppSettings([]byte(raw), settings.AppSettings{})
			if err != nil {
				t.Fatal(err)
			}
			if got.CDPProvider != tc.provider {
				t.Fatalf("cdpProvider = %q, want %q", got.CDPProvider, tc.provider)
			}
			if got.CDPPort != tc.wantPort {
				t.Fatalf("cdpPort = %d, want %d", got.CDPPort, tc.wantPort)
			}
		})
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
