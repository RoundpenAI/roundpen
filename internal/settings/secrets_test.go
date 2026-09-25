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
		DefaultImage:      "host",
		DefaultTtlSeconds: 1800,
		LlmgwVirtualKeys:  "vk-devsecret:dev",
	}
	out := s.SanitizeForResponse()
	if out.LlmgwVirtualKeys == "vk-devsecret:dev" ||
		(!strings.Contains(out.LlmgwVirtualKeys, "****") && !strings.Contains(out.LlmgwVirtualKeys, "...")) {
		t.Fatalf("virtual keys not masked: %q", out.LlmgwVirtualKeys)
	}
}

func TestAppSettingsValidate(t *testing.T) {
	valid := settings.AppSettings{
		DefaultImage:      "host",
		DefaultTtlSeconds: 1800,
		LlmgwVirtualKeys:  "vk-dev:dev",
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	bad := valid
	bad.DefaultTtlSeconds = 0
	if err := bad.Validate(); err == nil {
		t.Fatal("expected ttl validation error")
	}
	noImage := valid
	noImage.DefaultImage = ""
	if err := noImage.Validate(); err == nil {
		t.Fatal("expected defaultImage validation error")
	}
}
