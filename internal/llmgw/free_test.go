package llmgw_test

import (
	"context"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/settingitems"
)

func TestSeedFreeBindsSlots(t *testing.T) {
	reg, err := settingitems.NewRegistry([]settingitems.KindDef{llmgw.Kind()}, llmgw.Slots())
	if err != nil {
		t.Fatal(err)
	}
	cat := settingitems.NewCatalog(settingitems.NewMemoryStore(nil), reg)
	ctx := context.Background()
	if err := cat.Reload(ctx); err != nil {
		t.Fatal(err)
	}

	env := llmgw.FreeEnv{BaseURL: "https://free.example/v1", Model: "free-model"}
	if err := llmgw.SeedFree(ctx, cat, env); err != nil {
		t.Fatal(err)
	}
	it, ok := cat.Snapshot().Item(settingitems.KindLLM, llmgw.FreeItemID)
	if !ok || it.String("authMode") != llmgw.AuthModeInstallIdentity {
		t.Fatalf("item=%+v ok=%v", it, ok)
	}
	if it.String("baseUrl") != env.BaseURL {
		t.Fatalf("baseUrl=%q", it.String("baseUrl"))
	}
	for _, slot := range []string{settingitems.SlotLLMDefault, settingitems.SlotLLMSysAgent} {
		target, ok := llmgw.Resolve(cat.Snapshot(), slot, "")
		if !ok || target.ItemID != llmgw.FreeItemID {
			t.Fatalf("slot %s: %+v ok=%v", slot, target, ok)
		}
	}
	if mode := llmgw.ModeOf(true, cat.Snapshot()); mode != "free" {
		t.Fatalf("mode=%s", mode)
	}
	if err := cat.Delete(ctx, settingitems.KindLLM, llmgw.FreeItemID, true); err == nil {
		t.Fatal("expected protected delete to fail")
	}
}

func TestEnsureDefaultFreeBootstrapsOnce(t *testing.T) {
	reg, err := settingitems.NewRegistry([]settingitems.KindDef{llmgw.Kind()}, llmgw.Slots())
	if err != nil {
		t.Fatal(err)
	}
	cat := settingitems.NewCatalog(settingitems.NewMemoryStore(nil), reg)
	ctx := context.Background()
	if err := cat.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if !llmgw.NeedsFreeBootstrap(cat.Snapshot()) {
		t.Fatal("empty catalog should need bootstrap")
	}

	enabled := 0
	env := llmgw.FreeEnv{BaseURL: "https://free.example/v1", Model: "free-model"}
	if err := llmgw.EnsureDefaultFree(ctx, cat, env, func(context.Context) error {
		enabled++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("enable calls=%d", enabled)
	}
	if llmgw.NeedsFreeBootstrap(cat.Snapshot()) {
		t.Fatal("after seed, bootstrap should not be needed")
	}
	if mode := llmgw.ModeOf(true, cat.Snapshot()); mode != "free" {
		t.Fatalf("mode=%s", mode)
	}

	// Second boot must not re-enable or rebind over an existing setup.
	if err := llmgw.EnsureDefaultFree(ctx, cat, env, func(context.Context) error {
		enabled++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if enabled != 1 {
		t.Fatalf("second ensure must be a no-op; enable calls=%d", enabled)
	}
}

func TestEnsureDefaultFreeSkipsWhenBYOKBound(t *testing.T) {
	reg, err := settingitems.NewRegistry([]settingitems.KindDef{llmgw.Kind()}, llmgw.Slots())
	if err != nil {
		t.Fatal(err)
	}
	cat := settingitems.NewCatalog(settingitems.NewMemoryStore(nil), reg)
	ctx := context.Background()
	if err := cat.Reload(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := cat.Save(ctx, settingitems.Item{
		Kind: settingitems.KindLLM, ID: "openai", Name: "OpenAI", Enabled: true,
		Config: map[string]any{
			"protocol": llmgw.ProtocolOpenAI, "baseUrl": "https://api.openai.com/v1", "apiKey": "sk-test",
		},
	}); err != nil {
		t.Fatal(err)
	}
	if err := cat.SetBinding(ctx, settingitems.Binding{
		Scope: settingitems.GlobalScope, Slot: settingitems.SlotLLMDefault,
		Kind: settingitems.KindLLM, ItemID: "openai",
	}); err != nil {
		t.Fatal(err)
	}

	if err := llmgw.EnsureDefaultFree(ctx, cat, llmgw.LoadFreeEnv(), func(context.Context) error {
		t.Fatal("must not enable when BYOK already bound")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, ok := cat.Snapshot().Item(settingitems.KindLLM, llmgw.FreeItemID); ok {
		t.Fatal("must not seed FREE over BYOK")
	}
}
