package storage

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

func TestPgSandboxStore_RecreateAfterSoftDelete(t *testing.T) {
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
		t.Fatalf("migrate: %v", err)
	}

	const id = "pg-probe-stable"
	_, _ = db.SQL.ExecContext(ctx, `DELETE FROM sandboxes WHERE id=$1`, id)
	t.Cleanup(func() { _, _ = db.SQL.ExecContext(ctx, `DELETE FROM sandboxes WHERE id=$1`, id) })

	store := NewSandboxStore(db)
	now := time.Now().UTC()
	sb := &sandbox.Sandbox{
		ID: id, Name: "pg-probe-agent", Owner: "admin",
		Status: sandbox.StatusRunning, Image: "probe:img",
		WorkspaceID: "user-admin", CreatedAt: now, UpdatedAt: now, LastActiveAt: now,
	}
	if err := store.Insert(ctx, sb); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := store.SoftDelete(ctx, id, now.Add(time.Minute)); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	// Stable-id slots (agent workspaces) are deleted and recreated with the
	// same id; the soft-deleted row must be revived, not rejected.
	sb.CreatedAt = now.Add(2 * time.Minute)
	sb.UpdatedAt = sb.CreatedAt
	if err := store.Insert(ctx, sb); err != nil {
		t.Fatalf("re-insert after soft delete: %v", err)
	}
	got, err := store.Get(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "pg-probe-agent" || got.Status != sandbox.StatusRunning {
		t.Fatalf("got %#v", got)
	}
}
