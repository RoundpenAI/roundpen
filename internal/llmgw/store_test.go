package llmgw_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

func TestStoreUpstreamVirtualKeyRoundTrip(t *testing.T) {
	db := testDB(t)
	store := llmgw.NewStore(db, nil)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := store.UpsertVirtualKey(llmgw.VirtualKey{
		Key: "vk-roundtrip", Name: "roundtrip", Enabled: true, CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	vk, err := store.GetVirtualKey(ctx, "vk-roundtrip")
	if err != nil || vk.Name != "roundtrip" {
		t.Fatalf("GetVirtualKey: err=%v vk=%#v", err, vk)
	}
	keys, err := store.ListVirtualKeys(ctx)
	if err != nil || len(keys) == 0 {
		t.Fatalf("ListVirtualKeys: err=%v len=%d", err, len(keys))
	}

	tx := llmgw.Transaction{
		RequestID: "req-1", VirtualKey: "vk-roundtrip", VirtualName: "roundtrip",
		Provider: llmgw.ProviderOpenAI, Method: "POST", Path: "/llmgw/openai/v1/chat/completions",
		UpstreamURL: "https://api.openai.com/v1/chat/completions", StatusCode: 200,
		RequestBytes: 10, ResponseBytes: 20, DurationMS: 5, CreatedAt: now,
	}
	bodies := &llmgw.TransactionBodies{Request: `{"model":"alias"}`, Response: `{"ok":true}`}
	if err := store.InsertTransaction(ctx, tx, bodies); err != nil {
		t.Fatal(err)
	}

	logs, err := store.ListTransactions(ctx, llmgw.ListOptions{VirtualKey: "vk-roundtrip", Limit: 10})
	if err != nil || len(logs) == 0 {
		t.Fatalf("ListTransactions: err=%v len=%d", err, len(logs))
	}

	detail, gotBodies, err := store.GetTransaction(ctx, logs[0].ID)
	if err != nil || detail.RequestID != "req-1" || gotBodies == nil || gotBodies.Request == "" {
		t.Fatalf("GetTransaction: err=%v tx=%#v bodies=%#v", err, detail, gotBodies)
	}

	stats, err := store.Stats(ctx, "vk-roundtrip")
	if err != nil || stats.TotalRequests < 1 {
		t.Fatalf("Stats: err=%v stats=%#v", err, stats)
	}

	if _, err := store.GetVirtualKey(ctx, "missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetVirtualKey missing: err=%v", err)
	}
}

func TestStoreListTransactionsDefaults(t *testing.T) {
	db := testDB(t)
	store := llmgw.NewStore(db, nil)
	ctx := context.Background()
	logs, err := store.ListTransactions(ctx, llmgw.ListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if logs == nil {
		t.Fatal("expected non-nil slice")
	}
}
