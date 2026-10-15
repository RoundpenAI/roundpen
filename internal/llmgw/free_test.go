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
