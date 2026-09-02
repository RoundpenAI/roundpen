package config

import "testing"

func TestParseVirtualKeys(t *testing.T) {
	keys, err := ParseVirtualKeys("vk-dev:dev, vk-prod")
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("len=%d", len(keys))
	}
	if keys[0].Key != "vk-dev" || keys[0].Name != "dev" {
		t.Fatalf("key0=%+v", keys[0])
	}
	if keys[1].Key != "vk-prod" || keys[1].Name != "vk-prod" {
		t.Fatalf("key1=%+v", keys[1])
	}
}

func TestParseVirtualKeysDuplicate(t *testing.T) {
	_, err := ParseVirtualKeys("a:x,a:y")
	if err == nil {
		t.Fatal("expected duplicate error")
	}
}

func TestLoadLLMGWAutoEnable(t *testing.T) {
	t.Setenv("ROUNDPEN_LLMGW_ENABLED", "")
	t.Setenv("ROUNDPEN_LLMGW_OPENAI_BASE_URL", "https://api.openai.com")
	t.Setenv("ROUNDPEN_LLMGW_OPENAI_API_KEY", "sk-test")
	t.Setenv("ROUNDPEN_LLMGW_VIRTUAL_KEYS", "vk-dev:dev")
	t.Setenv("ROUNDPEN_LLMGW_ANTHROPIC_BASE_URL", "")
	t.Setenv("ROUNDPEN_LLMGW_ANTHROPIC_API_KEY", "")
	t.Setenv("ROUNDPEN_LLMGW_LOG_BODY_MAX_BYTES", "")
	t.Setenv("ROUNDPEN_LLMGW_PUBLIC_URL", "")

	cfg, err := loadLLMGW()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled {
		t.Fatal("expected enabled")
	}
	if cfg.OpenAI == nil || cfg.OpenAI.APIKey != "sk-test" {
		t.Fatalf("openai=%+v", cfg.OpenAI)
	}
	if len(cfg.VirtualKeys) != 1 {
		t.Fatalf("keys=%v", cfg.VirtualKeys)
	}
}

func TestLoadLLMGWDisabled(t *testing.T) {
	t.Setenv("ROUNDPEN_LLMGW_ENABLED", "false")
	t.Setenv("ROUNDPEN_LLMGW_OPENAI_BASE_URL", "https://api.openai.com")
	t.Setenv("ROUNDPEN_LLMGW_OPENAI_API_KEY", "sk-test")
	t.Setenv("ROUNDPEN_LLMGW_VIRTUAL_KEYS", "")
	t.Setenv("ROUNDPEN_LLMGW_ANTHROPIC_BASE_URL", "")
	t.Setenv("ROUNDPEN_LLMGW_ANTHROPIC_API_KEY", "")

	cfg, err := loadLLMGW()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Enabled {
		t.Fatal("expected disabled")
	}
}
