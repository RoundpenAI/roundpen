package storage_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

// testDSN returns the integration database URL, or "" when the suite is
// running without one.
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("ROUNDPEN_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set DATABASE_URL or ROUNDPEN_TEST_DATABASE_URL")
	}
	return dsn
}

// scratchDB creates a throwaway database and returns a connection to it. It
// is dropped when the test finishes, so migrations can be exercised from an
// empty schema — the state a first boot and an upgrade both start from.
func scratchDB(t *testing.T, dsn, suffix string) *storage.DB {
	t.Helper()
	ctx := context.Background()
	admin, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	name := fmt.Sprintf("roundpen_mig_%s_%d", suffix, time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, `CREATE DATABASE `+name); err != nil {
		t.Skipf("cannot create a scratch database (%v); grant CREATEDB to run this test", err)
	}
	db, err := storage.OpenPostgres(ctx, swapDatabase(dsn, name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = db.Close()
		if conn, err := sql.Open("pgx", dsn); err == nil {
			defer conn.Close()
			_, _ = conn.ExecContext(context.Background(), `DROP DATABASE IF EXISTS `+name)
		}
	})
	return db
}

// swapDatabase replaces the database name in a postgres URL.
func swapDatabase(dsn, name string) string {
	if i := strings.LastIndex(dsn, "/"); i >= 0 {
		rest := dsn[i+1:]
		if q := strings.IndexAny(rest, "?"); q >= 0 {
			return dsn[:i+1] + name + rest[q:]
		}
		return dsn[:i+1] + name
	}
	return dsn
}

// TestMigrateOnEmptyDatabase is the first-boot path: the schema must apply to
// a database that has nothing in it, including the step that copies per-user
// proxy selections out of columns that do not exist yet.
func TestMigrateOnEmptyDatabase(t *testing.T) {
	db := scratchDB(t, testDSN(t), "empty")
	if err := db.MigrateEmbedded(context.Background()); err != nil {
		t.Fatalf("migrate on an empty database: %v", err)
	}
	var tables int
	if err := db.SQL.QueryRowContext(context.Background(), `SELECT count(*) FROM information_schema.tables
		WHERE table_name IN ('setting_items','setting_bindings','users')`).Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 3 {
		t.Fatalf("expected the core tables, got %d", tables)
	}
}

// TestMigrateFromPreItemsSchema is the upgrade path: a database created before
// the multi-item rework keeps its per-user proxy selections, which must land in
// setting_bindings, and loses the retired columns and table.
func TestMigrateFromPreItemsSchema(t *testing.T) {
	ctx := context.Background()
	db := scratchDB(t, testDSN(t), "preitems")
	// Verbatim users DDL from the release that predates the rework.
	legacy := []string{
		`CREATE TABLE users (
			username        TEXT PRIMARY KEY,
			email           TEXT NOT NULL DEFAULT '',
			fullname        TEXT NOT NULL DEFAULT '',
			org_name        TEXT NOT NULL DEFAULT '',
			api_key         TEXT NOT NULL,
			role            TEXT NOT NULL DEFAULT 'user',
			password_hash   TEXT,
			auth_provider   TEXT NOT NULL DEFAULT 'local',
			model_source    TEXT NOT NULL DEFAULT 'gateway',
			agent_proxy     TEXT NOT NULL DEFAULT '',
			browser_proxy   TEXT NOT NULL DEFAULT '',
			created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
			updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
		)`,
		`INSERT INTO users (username, api_key, agent_proxy, browser_proxy) VALUES ('bob', 'rp-test', 'us', 'jp')`,
		`CREATE TABLE llmgw_upstreams (provider TEXT PRIMARY KEY, base_url TEXT NOT NULL, api_key TEXT NOT NULL)`,
	}
	for _, stmt := range legacy {
		if _, err := db.SQL.ExecContext(ctx, stmt); err != nil {
			t.Fatalf("seed legacy schema: %v", err)
		}
	}
	if err := db.MigrateEmbedded(ctx); err != nil {
		t.Fatalf("migrate a pre-items database: %v", err)
	}

	var slot, itemID string
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT slot, item_id FROM setting_bindings WHERE scope='user:bob' AND slot='proxy.agent'`).
		Scan(&slot, &itemID); err != nil {
		t.Fatalf("copied binding: %v", err)
	}
	if itemID != "us" {
		t.Fatalf("agent proxy binding = %q", itemID)
	}
	if err := db.SQL.QueryRowContext(ctx,
		`SELECT item_id FROM setting_bindings WHERE scope='user:bob' AND slot='proxy.browser'`).
		Scan(&itemID); err != nil || itemID != "jp" {
		t.Fatalf("browser proxy binding = %q err=%v", itemID, err)
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
