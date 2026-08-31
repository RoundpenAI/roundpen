package llmgw_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/llmgw"
)

func TestApplyConfigUpdatesRuntimeFields(t *testing.T) {
	gw := llmgw.New(testDB(t), llmgw.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	cfg := config.LLMGWConfig{
		Enabled:         true,
		PublicURL:       "https://roundpen.test",
		LogBodyMaxBytes: 0,
		EmbeddingModel:  "text-embedding-3-small",
		OpenAI:          &config.LLMGWUpstream{BaseURL: "https://api.openai.com", APIKey: "sk-test"},
		VirtualKeys:     []config.LLMGWVirtualKey{{Key: "vk-a", Name: "a"}},
	}
	if err := gw.ApplyConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if !gw.Enabled() {
		t.Fatal("expected enabled")
	}
	cfg.Enabled = false
	if err := gw.ApplyConfig(context.Background(), cfg); err != nil {
		t.Fatal(err)
	}
	if gw.Enabled() {
		t.Fatal("expected disabled")
	}
	vk, err := gw.Store().GetVirtualKey(context.Background(), "vk-a")
	if err != nil || vk.Name != "a" {
		t.Fatalf("virtual key: %+v err=%v", vk, err)
	}
}
