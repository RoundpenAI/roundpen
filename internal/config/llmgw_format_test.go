package config_test

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/config"
)

func TestFormatVirtualKeys(t *testing.T) {
	got := config.FormatVirtualKeys([]config.LLMGWVirtualKey{
		{Key: "vk-dev", Name: "dev"},
		{Key: "vk-prod", Name: "vk-prod"},
	})
	if got != "vk-dev:dev,vk-prod" {
		t.Fatalf("got %q", got)
	}
}

func TestApplyLLMGWSettings(t *testing.T) {
	var cfg config.LLMGWConfig
	if err := config.ApplyLLMGWSettings(&cfg, true, "https://rp.test", "embed-model", 1024,
		"https://api.openai.com", "sk-test", "", "", "vk-a:a"); err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.OpenAI == nil || cfg.OpenAI.APIKey != "sk-test" {
		t.Fatalf("cfg: %+v", cfg)
	}
	if len(cfg.VirtualKeys) != 1 || cfg.VirtualKeys[0].Key != "vk-a" {
		t.Fatalf("keys: %+v", cfg.VirtualKeys)
	}
}
