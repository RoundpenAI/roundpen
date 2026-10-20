package mcp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/secretbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

func pgStore(t *testing.T) (*Store, *storage.DB) {
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
	box, err := secretbox.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return &Store{DB: db.SQL, Box: box}, db
}

func pgUser(t *testing.T, db *storage.DB, username string) string {
	t.Helper()
	ctx := context.Background()
	users := storage.NewUserStore(db)
	err := users.Upsert(ctx, storage.User{
		Username: username, Email: username + "@example.test",
		APIKey: "rp-" + username, Role: storage.RoleUser,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = users.Delete(context.Background(), username) })
	return username
}

func TestPgStoreConnectionLifecycle(t *testing.T) {
	ctx := context.Background()
	s, db := pgStore(t)
	user := pgUser(t, db, "mcp-pg-user")

	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Millisecond)
	synced := time.Now().UTC().Truncate(time.Millisecond)
	conn, err := s.UpsertConnection(ctx, Connection{
		UserID: user, ServerID: "notion", AuthKind: AuthKindOAuth, Status: StatusConnected,
		AccessToken: "at-secret", RefreshToken: "rt-secret", TokenExpiresAt: &expires,
		Scopes: "read", TokenEndpoint: "https://as.test/token",
		ClientID: "cid", ClientSecret: "cs-secret", Resource: "https://mcp.notion.com",
		Tools:         []Tool{{Name: "search", Description: "d", InputSchema: json.RawMessage(`{"type":"object"}`)}},
		ToolsSyncedAt: &synced,
	})
	if err != nil {
		t.Fatal(err)
	}
	if conn.ID == "" || conn.AccessToken != "at-secret" || conn.ClientSecret != "cs-secret" {
		t.Fatalf("round-trip = %+v", conn)
	}
	if len(conn.Tools) != 1 || conn.Tools[0].Name != "search" {
		t.Fatalf("tools = %+v", conn.Tools)
	}
	if conn.TokenExpiresAt == nil || !conn.TokenExpiresAt.Equal(expires) {
		t.Fatalf("expires = %v", conn.TokenExpiresAt)
	}

	got, err := s.GetConnection(ctx, user, "notion")
	if err != nil || got.ID != conn.ID {
		t.Fatalf("get = %+v, %v", got, err)
	}

	// Secrets at rest must be sealed, not plaintext.
	var rawAccess, rawClientSecret string
	err = db.SQL.QueryRowContext(ctx, `
		SELECT access_token, client_secret FROM user_mcp_connections WHERE id=$1`, conn.ID).
		Scan(&rawAccess, &rawClientSecret)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(rawAccess, "enc:v1:") || !strings.HasPrefix(rawClientSecret, "enc:v1:") {
		t.Fatalf("secrets stored unsealed: %q / %q", rawAccess, rawClientSecret)
	}

	// Upsert on the same (user, server) replaces in place.
	if err := s.SaveTokens(ctx, conn.ID, "at2", "rt2", &expires); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetConnection(ctx, user, "notion")
	if err != nil || got.AccessToken != "at2" || got.RefreshToken != "rt2" {
		t.Fatalf("after refresh = %+v, %v", got, err)
	}

	if err := s.SaveTools(ctx, conn.ID, []Tool{{Name: "page_get"}}, synced); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(ctx, conn.ID, StatusNeedsReauth, "expired"); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetConnection(ctx, user, "notion")
	if err != nil || got.Status != StatusNeedsReauth || got.LastError != "expired" || got.Tools[0].Name != "page_get" {
		t.Fatalf("after status = %+v, %v", got, err)
	}

	list, err := s.ListConnections(ctx, user)
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %+v, %v", list, err)
	}

	if err := s.DeleteConnection(ctx, user, "notion"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetConnection(ctx, user, "notion"); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestPgStoreIsolatesUsers(t *testing.T) {
	ctx := context.Background()
	s, db := pgStore(t)
	alice := pgUser(t, db, "mcp-alice")
	bob := pgUser(t, db, "mcp-bob")

	if _, err := s.UpsertConnection(ctx, Connection{
		UserID: alice, ServerID: "exa", AuthKind: AuthKindAPIKey, APIKey: "k1",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetConnection(ctx, bob, "exa"); err == nil {
		t.Fatal("bob must not see alice's connection")
	}
	if _, err := s.UpsertConnection(ctx, Connection{
		UserID: bob, ServerID: "exa", AuthKind: AuthKindAPIKey, APIKey: "k2",
	}); err != nil {
		t.Fatal(err)
	}
	conns, err := s.ListConnections(ctx, alice)
	if err != nil || len(conns) != 1 || conns[0].APIKey != "k1" {
		t.Fatalf("alice = %+v, %v", conns, err)
	}
}

func TestPgStoreStateSingleUse(t *testing.T) {
	ctx := context.Background()
	s, db := pgStore(t)
	user := pgUser(t, db, "mcp-state-user")

	now := time.Now().UTC().Truncate(time.Millisecond)
	err := s.CreateState(ctx, State{
		State: "st-1", UserID: user, ServerID: "notion",
		Verifier: "v", RedirectURI: "https://app.test/cb", RedirectTo: "/settings/apps",
		Resource: "https://mcp.notion.com",
		AuthorizationEndpoint: "https://as.test/auth", TokenEndpoint: "https://as.test/token",
		ClientID: "cid", ClientSecret: "cs-secret", Scopes: "read",
		ExpiresAt: now.Add(10 * time.Minute), CreatedAt: now,
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := s.TakeState(ctx, "st-1")
	if err != nil {
		t.Fatal(err)
	}
	if st.UserID != user || st.Verifier != "v" || st.ClientSecret != "cs-secret" || st.TokenEndpoint != "https://as.test/token" {
		t.Fatalf("state = %+v", st)
	}
	// The state row stores the client secret sealed.
	var raw string
	if err := db.SQL.QueryRowContext(ctx, `SELECT client_secret FROM mcp_oauth_states WHERE state=$1`, "st-1").Scan(&raw); err == nil {
		t.Fatalf("state row must be consumed, got %q", raw)
	}
	if _, err := s.TakeState(ctx, "st-1"); err == nil {
		t.Fatal("state must be single use")
	}
}
