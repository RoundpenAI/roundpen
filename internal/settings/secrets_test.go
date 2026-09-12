package settings_test

import (
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/settings"
)

func TestSecretMasking(t *testing.T) {
	if settings.MaskSecret("sk-secret") != settings.SecretMask {
		t.Fatal("expected mask")
	}
	if settings.MaskSecret("") != "" {
		t.Fatal("empty should stay empty")
	}
	if got := settings.ResolveSecret(settings.SecretMask, "sk-old"); got != "sk-old" {
		t.Fatalf("resolve masked: %q", got)
	}
	if got := settings.ResolveSecret("sk-new", "sk-old"); got != "sk-new" {
		t.Fatalf("resolve new: %q", got)
	}
}

func TestAppSettingsSanitizeForResponse(t *testing.T) {
	s := settings.AppSettings{
		DefaultImage:           "host",
		DefaultTtlSeconds:      1800,
		PreviewTokenTtlSeconds: 900,
		LlmgwOpenaiAPIKey:      "sk-openai",
		LlmgwAnthropicAPIKey:   "sk-ant",
		LlmgwVirtualKeys:       "vk-devsecret:dev",
	}
	out := s.SanitizeForResponse()
	if out.LlmgwOpenaiAPIKey != settings.SecretMask || out.LlmgwAnthropicAPIKey != settings.SecretMask {
		t.Fatalf("sanitized: %+v", out)
	}
	if out.LlmgwVirtualKeys == "vk-devsecret:dev" || !strings.Contains(out.LlmgwVirtualKeys, "****") && !strings.Contains(out.LlmgwVirtualKeys, "...") {
		t.Fatalf("virtual keys not masked: %q", out.LlmgwVirtualKeys)
	}
}

func TestAppSettingsLLMGWValidate(t *testing.T) {
	valid := settings.AppSettings{
		DefaultImage:           "host",
		DefaultTtlSeconds:      1800,
		PreviewTokenTtlSeconds: 900,
		LlmgwOpenaiBaseURL:     "https://api.openai.com",
		LlmgwOpenaiAPIKey:      "sk-test",
		LlmgwVirtualKeys:       "vk-dev:dev",
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := valid
	bad.LlmgwOpenaiAPIKey = ""
	if err := bad.Validate(); err == nil {
		t.Fatal("expected openai key validation error")
	}
}
