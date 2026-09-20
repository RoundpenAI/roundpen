package oauth

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

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
	return &Store{DB: db.SQL}, db
}

func TestPgStoreProviderAndIdentityLifecycle(t *testing.T) {
	ctx := context.Background()
	s, db := pgStore(t)

	userStore := storage.NewUserStore(db)
	user := storage.User{Username: "oauth-pg-user", Email: "oauth-pg@example.com", APIKey: "rp-oauth-pg", Role: storage.RoleUser}
	if err := userStore.Upsert(ctx, user); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = userStore.Delete(ctx, user.Username) })

	p, err := Normalize(Provider{Kind: KindGitea, Host: "pg.example.test", ClientID: "cid", ClientSecret: "sec", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.DeleteProvider(ctx, p.ID) })
	saved, err := s.SaveProvider(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if saved.Scopes != DefaultScopes || !saved.Enabled {
		t.Fatalf("saved = %+v", saved)
	}

	expires := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	created, err := s.UpsertIdentity(ctx, IdentityUpsert{
		UserID: user.Username, ProviderID: p.ID, Subject: "42", Login: "octo",
		Email: "octo@example.test", AccessToken: "at-1", RefreshToken: "rt-1", ExpiresAt: &expires,
		Scopes: DefaultScopes, MarkLogin: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.ProviderHost != p.Host || created.ProviderKind != KindGitea {
		t.Fatalf("created = %+v", created)
	}

	// A second local user must not be able to claim the same remote account.
	other := storage.User{Username: "oauth-pg-other", APIKey: "rp-oauth-pg2", Role: storage.RoleUser}
	if err := userStore.Upsert(ctx, other); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = userStore.Delete(ctx, other.Username) })
	if _, err := s.UpsertIdentity(ctx, IdentityUpsert{
		UserID: other.Username, ProviderID: p.ID, Subject: "42", Login: "octo", AccessToken: "at-2",
	}); !errors.Is(err, ErrLinkedElsewhere) {
		t.Fatalf("claiming someone else's identity: err = %v, want ErrLinkedElsewhere", err)
	}

	// Re-authenticating the same subject refreshes tokens.
	if _, err := s.UpsertIdentity(ctx, IdentityUpsert{
		UserID: user.Username, ProviderID: p.ID, Subject: "42", Login: "octo",
		AccessToken: "at-3", ExpiresAt: &expires,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetIdentityBySubject(ctx, p.ID, "42")
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "at-3" || got.RefreshToken != "rt-1" {
		t.Errorf("tokens = %q / %q, want the refresh token preserved", got.AccessToken, got.RefreshToken)
	}

	// A second identity for the same user+provider is rejected by the schema.
	second := p
	second.ID = p.ID + "-two"
	second.Host = "pg2.example.test"
	if _, err := s.SaveProvider(ctx, second); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.DeleteProvider(ctx, second.ID) })
	if _, err := s.UpsertIdentity(ctx, IdentityUpsert{
		UserID: user.Username, ProviderID: second.ID, Subject: "7", Login: "other", AccessToken: "at-4",
	}); !errors.Is(err, ErrAlreadyLinked) {
		t.Fatalf("second identity on the same provider: err = %v, want ErrAlreadyLinked", err)
	}

	expiring, err := s.ListExpiringIdentities(ctx, time.Now().Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, id := range expiring {
		if id.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Error("the expiring identity was not returned")
	}

	if err := s.DeleteIdentity(ctx, user.Username, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetIdentityBySubject(ctx, p.ID, "42"); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("after unlink: err = %v, want not found", err)
	}

	// Deleting the provider cascades to its identities.
	if _, err := s.UpsertIdentity(ctx, IdentityUpsert{
		UserID: user.Username, ProviderID: second.ID, Subject: "7", Login: "other", AccessToken: "at-5",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteProvider(ctx, second.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetIdentityBySubject(ctx, second.ID, "7"); err == nil {
		t.Error("identities should be removed with their provider")
	}
}

func TestPgStoreStateIsSingleUse(t *testing.T) {
	ctx := context.Background()
	s, _ := pgStore(t)

	st := State{
		State: "pg-state-1", ProviderID: "p", Verifier: "v", RedirectURI: "https://console.test/cb",
		RedirectTo: "/", ExpiresAt: time.Now().Add(time.Minute), CreatedAt: time.Now(),
	}
	if err := s.CreateState(ctx, st); err != nil {
		t.Fatal(err)
	}
	got, err := s.TakeState(ctx, st.State)
	if err != nil {
		t.Fatal(err)
	}
	if got.Verifier != "v" || got.RedirectURI != "https://console.test/cb" {
		t.Fatalf("state = %+v", got)
	}
	if _, err := s.TakeState(ctx, st.State); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("second take: err = %v, want not found", err)
	}

	expired := st
	expired.State = "pg-state-expired"
	expired.ExpiresAt = time.Now().Add(-time.Minute)
	if err := s.CreateState(ctx, expired); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TakeState(ctx, expired.State); !errors.Is(err, storage.ErrNotFound) {
		t.Errorf("expired state: err = %v, want not found", err)
	}
}
