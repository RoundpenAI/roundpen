package storage

import (
	"context"
	"os"
	"testing"
)

func TestMemoryUserStore_APIKeyAndEmail(t *testing.T) {
	ctx := context.Background()
	s := NewMemoryUserStore()
	u := User{Username: "alice", Email: "Alice@Example.com", APIKey: "rp-1", Role: RoleUser}
	if err := s.Upsert(ctx, u); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetByAPIKey(ctx, "rp-1")
	if err != nil || got.Username != "alice" {
		t.Fatalf("GetByAPIKey: %v %#v", err, got)
	}
	stored, err := s.GetByUsername(ctx, "alice")
	if err != nil || stored.APIKey != HashAPIKey("rp-1") {
		t.Fatalf("stored key should be hashed: %#v err=%v", stored, err)
	}
	got, err = s.GetByEmail(ctx, "alice@example.com")
	if err != nil || got.Username != "alice" {
		t.Fatalf("GetByEmail: %v %#v", err, got)
	}
	if _, err := s.GetByUsername(ctx, "missing"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestPgUserStore_RoundTrip(t *testing.T) {
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

	users := NewUserStore(db)
	sessions := NewSessionStore(db)
	u := User{Username: "pg-test-user", Email: "pg-test@example.com", APIKey: "rp-pg-test", Role: RoleAdmin, PasswordHash: "hash"}
	t.Cleanup(func() { _ = users.Delete(ctx, u.Username) })
	if err := users.Upsert(ctx, u); err != nil {
		t.Fatal(err)
	}
	got, err := users.GetByAPIKey(ctx, "rp-pg-test")
	if err != nil || got.Email != "pg-test@example.com" {
		t.Fatalf("GetByAPIKey: %v %#v", err, got)
	}
	got, err = users.GetByEmail(ctx, "PG-TEST@example.com")
	if err != nil || got.Username != "pg-test-user" {
		t.Fatalf("GetByEmail: %v %#v", err, got)
	}

	sess := Session{ID: "sess-1", UserID: u.Username, TokenHash: "tokhash", ExpiresAt: got.CreatedAt.Add(sessionTTL)}
	if sess.CreatedAt.IsZero() {
		sess.CreatedAt = got.UpdatedAt
		sess.LastSeenAt = got.UpdatedAt
		sess.ExpiresAt = got.UpdatedAt.Add(sessionTTL)
	}
	if err := sessions.Create(ctx, sess); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sessions.Delete(ctx, sess.ID) })
	found, err := sessions.GetByTokenHash(ctx, "tokhash")
	if err != nil || found.UserID != u.Username {
		t.Fatalf("GetByTokenHash: %v %#v", err, found)
	}
}
