package storage_test

import (
	"context"
	"os"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

// TestLegacyColumnsRetired proves the migration moved per-user proxy
// selections into setting_bindings and dropped the old columns and table.
func TestLegacyColumnsRetired(t *testing.T) {
	dsn := os.Getenv("ROUNDPEN_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set ROUNDPEN_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	db, err := storage.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.MigrateEmbedded(ctx); err != nil {
		t.Fatal(err)
	}
	var cols int
	if err := db.SQL.QueryRowContext(ctx, `SELECT count(*) FROM information_schema.columns
		WHERE table_name='users' AND column_name IN ('agent_proxy','browser_proxy')`).Scan(&cols); err != nil {
		t.Fatal(err)
	}
	if cols != 0 {
		t.Fatalf("users still has %d proxy columns", cols)
	}
	var upstreams string
	if err := db.SQL.QueryRowContext(ctx, `SELECT coalesce(to_regclass('llmgw_upstreams')::text,'')`).Scan(&upstreams); err != nil {
		t.Fatal(err)
	}
	if upstreams != "" {
		t.Fatalf("llmgw_upstreams still exists: %s", upstreams)
	}
}
