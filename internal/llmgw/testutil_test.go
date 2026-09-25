package llmgw_test

import (
	"context"
	"os"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

func testDB(t *testing.T) *storage.DB {
	t.Helper()
	dsn := os.Getenv("ROUNDPEN_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set DATABASE_URL or ROUNDPEN_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	db, err := storage.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.MigrateEmbedded(ctx); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedGateway(t *testing.T) *llmgw.Gateway {
	t.Helper()
	gw := llmgw.New(testDB(t), llmgw.Options{LogBodyMaxBytes: 128, PublicURL: "https://roundpen.test"})
	ctx := context.Background()
	seedUpstream(t, gw, llmgw.Upstream{
		Provider: "openai",
		Protocol: llmgw.ProtocolOpenAI,
		BaseURL:  "https://upstream.test",
		APIKey:   "sk-openai",
		ModelMap: map[string]string{"gpt-alias": "gpt-real"},
	}, llmgw.Upstream{
		Provider: "anthropic",
		Protocol: llmgw.ProtocolAnthropic,
		BaseURL:  "https://anthropic.test",
		APIKey:   "sk-ant",
	})
	if err := gw.Store().UpsertVirtualKey(llmgw.VirtualKey{Key: "vk-test", Name: "test", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	if err := gw.EnsureInternal(ctx); err != nil {
		t.Fatal(err)
	}
	return gw
}

// seedUpstream points the relay at test providers and enables them.
func seedUpstream(t *testing.T, gw *llmgw.Gateway, ups ...llmgw.Upstream) {
	t.Helper()
	for i := range ups {
		ups[i].Enabled = true
		if ups[i].ModelMap == nil {
			ups[i].ModelMap = map[string]string{}
		}
	}
	gw.SetUpstreams(ups)
	if len(ups) > 0 {
		gw.SetEmbedding(ups[0].Provider, "")
	}
}
