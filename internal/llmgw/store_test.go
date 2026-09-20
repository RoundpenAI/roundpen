package llmgw_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/secretbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

func TestStoreUpstreamVirtualKeyRoundTrip(t *testing.T) {
	db := testDB(t)
	store := llmgw.NewStore(db, nil)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := store.UpsertUpstream(llmgw.Upstream{
		Provider: llmgw.ProviderOpenAI,
		BaseURL:  "https://api.openai.com",
		APIKey:   "sk-test",
		ModelMap: map[string]string{"alias": "real"},
		ModelPatterns: []llmgw.ModelPattern{
			{Pattern: "gpt-*", Target: "gpt-up"},
		},
		Enabled:   true,
		UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := store.GetUpstream(ctx, llmgw.ProviderOpenAI)
	if err != nil || got.BaseURL != "https://api.openai.com" || got.ModelMap["alias"] != "real" {
		t.Fatalf("GetUpstream: err=%v upstream=%#v", err, got)
	}
	list, err := store.ListUpstreams(ctx)
	if err != nil || len(list) == 0 {
		t.Fatalf("ListUpstreams: err=%v len=%d", err, len(list))
	}

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

	if _, err := store.GetUpstream(ctx, "missing"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetUpstream missing: err=%v", err)
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

func TestStoreUpstreamAPIKeyEncryptedAtRest(t *testing.T) {
	db := testDB(t)
	box, err := secretbox.New(bytes.Repeat([]byte{3}, 32))
	if err != nil {
		t.Fatal(err)
	}
	store := llmgw.NewStore(db, box)
	ctx := context.Background()

	if err := store.UpsertUpstream(llmgw.Upstream{
		Provider:  llmgw.ProviderAnthropic,
		BaseURL:   "https://api.anthropic.com",
		APIKey:    "sk-ant-real-key",
		Enabled:   true,
		UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	var raw string
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT api_key FROM llmgw_upstreams WHERE provider=$1`,
		llmgw.ProviderAnthropic).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(raw, "sk-ant-real-key") {
		t.Fatalf("plaintext api key at rest: %s", raw)
	}
	if !strings.HasPrefix(raw, "enc:v1:") {
		t.Fatalf("expected sealed api key, got %q", raw)
	}

	got, err := store.GetUpstream(ctx, llmgw.ProviderAnthropic)
	if err != nil || got.APIKey != "sk-ant-real-key" {
		t.Fatalf("GetUpstream: err=%v key=%q", err, got.APIKey)
	}
	list, err := store.ListUpstreams(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, u := range list {
		if u.Provider == llmgw.ProviderAnthropic {
			found = u.APIKey == "sk-ant-real-key"
		}
	}
	if !found {
		t.Fatal("ListUpstreams did not decrypt the api key")
	}

	// A legacy plaintext row remains readable.
	if _, err := db.SQL.ExecContext(ctx,
		`UPDATE llmgw_upstreams SET api_key='sk-legacy-plaintext' WHERE provider=$1`,
		llmgw.ProviderAnthropic); err != nil {
		t.Fatal(err)
	}
	legacy, err := store.GetUpstream(ctx, llmgw.ProviderAnthropic)
	if err != nil || legacy.APIKey != "sk-legacy-plaintext" {
		t.Fatalf("legacy: err=%v key=%q", err, legacy.APIKey)
	}
}
