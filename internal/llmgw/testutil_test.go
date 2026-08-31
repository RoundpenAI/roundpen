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
	if err := gw.SeedFromConfig(llmgw.SeedConfig{
		OpenAI: &llmgw.UpstreamSeed{
			BaseURL:  "https://upstream.test",
			APIKey:   "sk-openai",
			ModelMap: map[string]string{"gpt-alias": "gpt-real"},
		},
		Anthropic: &llmgw.UpstreamSeed{
			BaseURL: "https://anthropic.test/",
			APIKey:  "sk-ant",
		},
		Keys: []llmgw.VirtualKey{{Key: "vk-test", Name: "test"}},
	}); err != nil {
		t.Fatal(err)
	}
	if err := gw.EnsureInternal(ctx, "text-embedding-3-small"); err != nil {
		t.Fatal(err)
	}
	return gw
}
