package agentsession

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

func TestMergeToolMeta(t *testing.T) {
	got := MergeToolMeta(
		ToolMeta{ToolID: "t1", Title: "Read", Status: "pending", Input: map[string]any{"path": "a.go"}},
		ToolMeta{ToolID: "t1", Status: "completed", Output: "ok"},
	)
	if got.Title != "Read" || got.Status != "completed" {
		t.Fatalf("title/status: %+v", got)
	}
	in, _ := got.Input.(map[string]any)
	if in["path"] != "a.go" {
		t.Fatalf("input lost: %#v", got.Input)
	}
	if got.Output != "ok" || got.Type != "tool_call" {
		t.Fatalf("output/type: %+v", got)
	}
}

func TestStore_UpsertToolMessage(t *testing.T) {
	dsn := os.Getenv("ROUNDPEN_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set ROUNDPEN_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	db, err := storage.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.MigrateEmbedded(ctx); err != nil {
		t.Fatal(err)
	}

	user := "toolhist-" + uuid.NewString()[:8]
	if _, err := db.SQL.ExecContext(ctx, `
		INSERT INTO users (username, email, role, api_key, created_at, updated_at)
		VALUES ($1, $2, 'user', $3, now(), now())
		ON CONFLICT (username) DO NOTHING`,
		user, user+"@example.com", "rp-"+user,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.SQL.ExecContext(ctx, `DELETE FROM users WHERE username=$1`, user)
	})

	store := &Store{DB: db.SQL}
	sess, err := store.Create(ctx, user, "tools", "sysadmin", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.SQL.ExecContext(ctx, `DELETE FROM agent_sessions WHERE id=$1`, sess.ID)
	})

	if _, err := store.AddMessage(ctx, sess.ID, "user", "hi", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertToolMessage(ctx, sess.ID, ToolMeta{
		ToolID: "tc-1", Title: "Read", Status: "pending",
		Input: map[string]any{"path": "main.go"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpsertToolMessage(ctx, sess.ID, ToolMeta{
		ToolID: "tc-1", Status: "completed", Output: "package main",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMessage(ctx, sess.ID, RoleThought, "considering files", map[string]string{"type": "thought"}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMessage(ctx, sess.ID, RolePermission, "Read · allow", PermissionMeta{
		Type: "permission", Title: "Read", OptionID: "allow", Outcome: "selected",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMessage(ctx, sess.ID, RoleAssistant, "done", map[string]string{"type": "assistant"}); err != nil {
		t.Fatal(err)
	}

	msgs, err := store.ListMessages(ctx, sess.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 5 {
		t.Fatalf("want 5 classified messages, got %d", len(msgs))
	}
	wantRoles := []string{RoleUser, RoleTool, RoleThought, RolePermission, RoleAssistant}
	for i, role := range wantRoles {
		if msgs[i].Role != role {
			t.Fatalf("msg %d role=%s want %s", i, msgs[i].Role, role)
		}
	}
	if msgs[1].Role != "tool" {
		t.Fatalf("want tool row, got %#v", msgs[1])
	}
	var meta ToolMeta
	if err := json.Unmarshal(msgs[1].Meta, &meta); err != nil {
		t.Fatal(err)
	}
	if meta.ToolID != "tc-1" || meta.Title != "Read" || meta.Status != "completed" {
		t.Fatalf("meta: %+v", meta)
	}
	if meta.Output != "package main" {
		t.Fatalf("output: %#v", meta.Output)
	}
}

func TestStore_DeleteRemovesFromList(t *testing.T) {
	dsn := os.Getenv("ROUNDPEN_TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		t.Skip("set ROUNDPEN_TEST_DATABASE_URL")
	}
	ctx := context.Background()
	db, err := storage.OpenPostgres(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.MigrateEmbedded(ctx); err != nil {
		t.Fatal(err)
	}

	user := "del-" + uuid.NewString()[:8]
	if _, err := db.SQL.ExecContext(ctx, `
		INSERT INTO users (username, email, role, api_key, created_at, updated_at)
		VALUES ($1, $2, 'user', $3, now(), now())
		ON CONFLICT (username) DO NOTHING`,
		user, user+"@example.com", "rp-"+user,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = db.SQL.ExecContext(ctx, `DELETE FROM users WHERE username=$1`, user)
	})

	store := &Store{DB: db.SQL}
	sess, err := store.Create(ctx, user, "gone", "sysadmin", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.AddMessage(ctx, sess.ID, RoleUser, "hi", nil); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(ctx, sess.ID); err != nil {
		t.Fatal(err)
	}
	list, err := store.ListByUser(ctx, user, 20)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range list {
		if s.ID == sess.ID {
			t.Fatal("deleted session still listed")
		}
	}
	if _, err := store.Get(ctx, sess.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete: %v", err)
	}
}
