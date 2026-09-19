package storage

import (
	"context"
	"os"
	"testing"
)

// TestMigrateEmbedded_Replay covers the issue tracker DDL: the embedded schema is
// re-executed on every daemon boot, so a second run must succeed and the tables,
// sequences and the circular tasks.plan_doc_id column must all be in place.
func TestMigrateEmbedded_Replay(t *testing.T) {
	dsn := os.Getenv("ROUNDPEN_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set DATABASE_URL or ROUNDPEN_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	db, err := OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatalf("postgres: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := db.MigrateEmbedded(ctx); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := db.MigrateEmbedded(ctx); err != nil {
		t.Fatalf("second migrate (daemon restart replay): %v", err)
	}

	// LIMIT 0 still parses and plans the statement, so a missing table or column fails here.
	for _, q := range []string{
		`SELECT key, status, closed_at, origin FROM issues LIMIT 0`,
		`SELECT key, plan_doc_id, position, done_at FROM tasks LIMIT 0`,
		`SELECT key, version, status, content_md, author_type FROM issue_docs LIMIT 0`,
	} {
		rows, err := db.SQL.QueryContext(ctx, q)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		_ = rows.Close()
	}
	var n int64
	if err := db.SQL.QueryRowContext(ctx, `SELECT nextval('issue_key_seq')`).Scan(&n); err != nil {
		t.Fatalf("issue_key_seq: %v", err)
	}
}
