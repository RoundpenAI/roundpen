package llmgw_test

import (
	"context"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/settingitems"
)

func llmCatalog(t *testing.T) *settingitems.Catalog {
	t.Helper()
	reg, err := settingitems.NewRegistry([]settingitems.KindDef{llmgw.Kind()}, llmgw.Slots())
	if err != nil {
		t.Fatal(err)
	}
	cat := settingitems.NewCatalog(settingitems.NewMemoryStore(nil), reg)
	if err := cat.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	return cat
}

func TestBuiltinVendorFillsEndpointFromKeyAndModel(t *testing.T) {
	cat := llmCatalog(t)
	ctx := context.Background()
	saved, err := cat.Save(ctx, settingitems.Item{
		Kind: settingitems.KindLLM, ID: "ds", Name: "DeepSeek", Enabled: true,
		Config: map[string]any{
			"vendor":       "deepseek",
			"apiKey":       "sk-deepseek",
			"defaultModel": "deepseek-v4-pro",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.String("protocol") != llmgw.ProtocolOpenAI || saved.String("baseUrl") != "https://api.deepseek.com" {
		t.Fatalf("filled endpoint: protocol=%q base=%q", saved.String("protocol"), saved.String("baseUrl"))
	}
	if saved.Secret("apiKey") != "sk-deepseek" {
		t.Fatalf("key=%q", saved.Secret("apiKey"))
	}

	// A model the platform does not offer is rejected.
	if _, err := cat.Save(ctx, settingitems.Item{
		Kind: settingitems.KindLLM, ID: "ds", Name: "DeepSeek", Enabled: true,
		Config: map[string]any{"vendor": "deepseek", "apiKey": "sk-deepseek", "defaultModel": "gpt-4"},
	}); err == nil || !strings.Contains(err.Error(), "defaultModel") {
		t.Fatalf("err=%v", err)
	}

	// With no slot binding, every usage point inherits this single provider.
	for _, slot := range []string{settingitems.SlotLLMDefault, settingitems.SlotLLMAgent, settingitems.SlotLLMSysAgent, settingitems.SlotLLMEmbedding} {
		target, ok := llmgw.Resolve(cat.Snapshot(), slot, "")
		if !ok || target.ItemID != "ds" || target.Model != "deepseek-v4-pro" {
			t.Fatalf("slot %s: %+v ok=%v", slot, target, ok)
		}
	}
	cfg := llmgw.VendorSearch(cat.Snapshot(), "")
	if cfg.Vendor != llmgw.VendorDeepSeek || cfg.APIKey != "sk-deepseek" || cfg.Model != "deepseek-v4-pro" {
		t.Fatalf("vendor search: %+v", cfg)
	}
	if cfg.BaseURL != "https://api.deepseek.com/anthropic/v1" {
		t.Fatalf("search base=%q", cfg.BaseURL)
	}
}

func TestCustomVendorStillRequiresEndpoint(t *testing.T) {
	cat := llmCatalog(t)
	_, err := cat.Save(context.Background(), settingitems.Item{
		Kind: settingitems.KindLLM, ID: "mine", Name: "Mine", Enabled: true,
		Config: map[string]any{"vendor": "custom", "apiKey": "sk", "protocol": "openai"},
	})
	if err == nil || !strings.Contains(err.Error(), "baseUrl") {
		t.Fatalf("err=%v", err)
	}
}

func TestOpenRouterAcceptsUnlistedModelID(t *testing.T) {
	cat := llmCatalog(t)
	saved, err := cat.Save(context.Background(), settingitems.Item{
		Kind: settingitems.KindLLM, ID: "or", Name: "OpenRouter", Enabled: true,
		Config: map[string]any{
			"vendor": "openrouter", "apiKey": "sk-or", "defaultModel": "anthropic/claude-sonnet-4.5",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if saved.String("baseUrl") != "https://openrouter.ai/api/v1" {
		t.Fatalf("base=%q", saved.String("baseUrl"))
	}
	if _, err := cat.Save(context.Background(), settingitems.Item{
		Kind: settingitems.KindLLM, ID: "or", Name: "OpenRouter", Enabled: true,
		Config: map[string]any{"vendor": "openrouter", "apiKey": "sk-or", "defaultModel": "not a model"},
	}); err == nil {
		t.Fatal("expected invalid model id")
	}
}

func TestMultipleProvidersInheritFirstUntilBound(t *testing.T) {
	cat := llmCatalog(t)
	ctx := context.Background()
	for i, id := range []string{"kimi", "glm"} {
		vendor := id
		model := "kimi-k3"
		if id == "glm" {
			model = "glm-4.7"
		}
		if _, err := cat.Save(ctx, settingitems.Item{
			Kind: settingitems.KindLLM, ID: id, Name: id, Enabled: true, Position: i,
			Config: map[string]any{"vendor": vendor, "apiKey": "sk-" + id, "defaultModel": model},
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Nothing is bound: every slot inherits the first enabled provider.
	target, ok := llmgw.Resolve(cat.Snapshot(), settingitems.SlotLLMAgent, "")
	if !ok || target.ItemID != "kimi" {
		t.Fatalf("inherited agent: %+v ok=%v", target, ok)
	}
	if err := cat.SetBinding(ctx, settingitems.Binding{
		Scope: settingitems.GlobalScope, Slot: settingitems.SlotLLMAgent,
		Kind: settingitems.KindLLM, ItemID: "glm",
	}); err != nil {
		t.Fatal(err)
	}
	target, ok = llmgw.Resolve(cat.Snapshot(), settingitems.SlotLLMAgent, "")
	if !ok || target.ItemID != "glm" || target.Model != "glm-4.7" {
		t.Fatalf("bound agent: %+v ok=%v", target, ok)
	}
	// An unbound sibling still inherits the first provider, not the agent override.
	sys, ok := llmgw.Resolve(cat.Snapshot(), settingitems.SlotLLMSysAgent, "")
	if !ok || sys.ItemID != "kimi" {
		t.Fatalf("sysagent inherit: %+v ok=%v", sys, ok)
	}
}
