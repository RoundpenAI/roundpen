package webpush

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

func pushTestDB(t *testing.T) *storage.DB {
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
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.MigrateEmbedded(ctx); err != nil {
		t.Fatal(err)
	}
	return db
}

func pushTestUser(t *testing.T, db *storage.DB) string {
	t.Helper()
	ctx := context.Background()
	user := fmt.Sprintf("push-user-%d", time.Now().UnixNano())
	if _, err := db.SQL.ExecContext(ctx, `INSERT INTO users (username, api_key, role) VALUES ($1,$1,'user')`, user); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = db.SQL.ExecContext(ctx, `DELETE FROM users WHERE username=$1`, user)
	})
	return user
}

func TestStoreUpsertListDelete(t *testing.T) {
	db := pushTestDB(t)
	user := pushTestUser(t, db)
	store := &Store{DB: db.SQL}
	ctx := context.Background()

	ok, err := store.HasAny(ctx, user)
	if err != nil || ok {
		t.Fatalf("HasAny before upsert = %v, %v", ok, err)
	}

	sub, err := store.Upsert(ctx, user, UpsertInput{
		Endpoint:  "https://push.example/ep-1",
		P256dh:    "p256dh-key",
		Auth:      "auth-key",
		UserAgent: "test-agent",
	})
	if err != nil {
		t.Fatal(err)
	}
	if sub.Endpoint != "https://push.example/ep-1" || sub.UserID != user {
		t.Fatalf("upsert result = %+v", sub)
	}

	ok, err = store.HasAny(ctx, user)
	if err != nil || !ok {
		t.Fatalf("HasAny after upsert = %v, %v", ok, err)
	}

	list, err := store.ListByUser(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].P256dh != "p256dh-key" {
		t.Fatalf("list = %+v", list)
	}

	// Upsert same endpoint updates keys / ownership.
	if _, err := store.Upsert(ctx, user, UpsertInput{
		Endpoint: "https://push.example/ep-1",
		P256dh:   "p256dh-key-2",
		Auth:     "auth-key-2",
	}); err != nil {
		t.Fatal(err)
	}
	list, err = store.ListByUser(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].P256dh != "p256dh-key-2" {
		t.Fatalf("after update list = %+v", list)
	}

	if err := store.Delete(ctx, user, "https://push.example/ep-1"); err != nil {
		t.Fatal(err)
	}
	ok, err = store.HasAny(ctx, user)
	if err != nil || ok {
		t.Fatalf("HasAny after delete = %v, %v", ok, err)
	}
}

func TestStoreRejectsIncompleteInput(t *testing.T) {
	db := pushTestDB(t)
	user := pushTestUser(t, db)
	store := &Store{DB: db.SQL}
	_, err := store.Upsert(context.Background(), user, UpsertInput{Endpoint: "https://x"})
	if err != errInvalidSubscription {
		t.Fatalf("want errInvalidSubscription, got %v", err)
	}
}
