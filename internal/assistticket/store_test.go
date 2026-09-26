package assistticket

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

func testDB(t *testing.T) *storage.DB {
	t.Helper()
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
	return db
}

// fixture inserts a user plus an assistant row: tickets reference both.
func fixture(t *testing.T, db *storage.DB) (string, string) {
	t.Helper()
	ctx := context.Background()
	user := "ticket-" + uuid.NewString()[:8]
	assistantID := uuid.NewString()
	if _, err := db.SQL.ExecContext(ctx, `
		INSERT INTO users (username, email, role, api_key, created_at, updated_at)
		VALUES ($1, $2, 'user', $3, now(), now())`,
		user, user+"@example.com", "rp-"+user,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.SQL.ExecContext(ctx, `
		INSERT INTO assistants (id, user_id, name, created_at, updated_at)
		VALUES ($1, $2, 'Ada', now(), now())`,
		assistantID, user,
	); err != nil {
		t.Fatalf("insert assistant: %v", err)
	}
	t.Cleanup(func() {
		// Cascades remove the assistant and its tickets.
		_, _ = db.SQL.ExecContext(ctx, `DELETE FROM users WHERE username=$1`, user)
	})
	return user, assistantID
}

func TestStore_CancelPendingTicket(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user, assistantID := fixture(t, db)
	store := &Store{DB: db.SQL}

	tk, err := store.Create(ctx, user, CreateInput{
		AssistantID: assistantID,
		SessionID:   "s-1",
		Kind:        KindPermission,
		Title:       "需要确认：Bash",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.Cancel(ctx, tk.ID, "权限请求已取消")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusCancelled || got.ResolutionNote != "权限请求已取消" || got.ResolvedAt == nil {
		t.Fatalf("cancelled ticket = %+v", got)
	}

	pending, err := store.ListPendingByUser(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending after cancel = %d, want 0", len(pending))
	}
}

func TestStore_CancelKeepsResolvedOutcome(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user, assistantID := fixture(t, db)
	store := &Store{DB: db.SQL}

	tk, err := store.Create(ctx, user, CreateInput{
		AssistantID: assistantID,
		Kind:        KindPermission,
		Title:       "需要确认：Bash",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Resolve(ctx, tk.ID, ResAllowOnce, "allow"); err != nil {
		t.Fatal(err)
	}

	got, err := store.Cancel(ctx, tk.ID, "权限请求已取消")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusResolved || got.Resolution != ResAllowOnce {
		t.Fatalf("resolved ticket overwritten by cancel: %+v", got)
	}
}

func TestStore_CancelUnknownTicket(t *testing.T) {
	db := testDB(t)
	store := &Store{DB: db.SQL}

	if _, err := store.Cancel(context.Background(), uuid.NewString(), "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cancel unknown ticket err = %v, want ErrNotFound", err)
	}
}

func TestStore_CancelStalePending(t *testing.T) {
	db := testDB(t)
	ctx := context.Background()
	user, assistantID := fixture(t, db)
	store := &Store{DB: db.SQL}

	old, err := store.Create(ctx, user, CreateInput{
		AssistantID: assistantID,
		Kind:        KindPermission,
		Title:       "需要确认：Bash",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Backdate the leftover row: a boot after a restart must close it.
	if _, err := db.SQL.ExecContext(ctx, `
		UPDATE assist_tickets SET created_at = now() - interval '2 hours' WHERE id=$1`,
		old.ID,
	); err != nil {
		t.Fatal(err)
	}
	fresh, err := store.Create(ctx, user, CreateInput{
		AssistantID: assistantID,
		Kind:        KindPermission,
		Title:       "需要确认：Edit",
	})
	if err != nil {
		t.Fatal(err)
	}

	n, err := store.CancelStalePending(ctx, time.Now().UTC().Add(-time.Hour), "重启")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("cancelled rows = %d, want 1", n)
	}
	got, err := store.Get(ctx, old.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusCancelled || got.ResolutionNote != "重启" {
		t.Fatalf("stale ticket = %+v, want cancelled", got)
	}
	kept, err := store.Get(ctx, fresh.ID)
	if err != nil {
		t.Fatal(err)
	}
	if kept.Status != StatusPending {
		t.Fatalf("fresh ticket = %+v, want still pending", kept)
	}
}
