package connectors

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

func TestClientGrantAndConnectState(t *testing.T) {
	ctx := context.Background()
	store, db := pgStore(t)
	userStore := storage.NewUserStore(db)
	user := storage.User{Username: "conn-pg-user", Email: "conn-pg@example.com", APIKey: "rp-conn-pg", Role: storage.RoleUser}
	if err := userStore.Upsert(ctx, user); err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	t.Cleanup(func() { _ = userStore.Delete(ctx, user.Username) })

	svc := &Service{Store: store}

	if _, err := svc.GetClient(ctx, user.Username); !errors.Is(err, ErrNoClient) {
		t.Fatalf("expected ErrNoClient, got %v", err)
	}

	c, err := svc.SaveClient(ctx, user.Username, "cid-1", "sec-1")
	if err != nil {
		t.Fatalf("save client: %v", err)
	}
	if c.ClientID != "cid-1" || c.ClientSecret != "sec-1" {
		t.Fatalf("client = %+v", c)
	}

	hasClient, grant, err := svc.Status(ctx, user.Username)
	if err != nil || !hasClient || grant != nil {
		t.Fatalf("status hasClient=%v grant=%v err=%v", hasClient, grant, err)
	}

	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	svc.Now = func() time.Time { return now }
	if err := store.CreateState(ctx, State{
		State: "st-1", UserID: user.Username, Verifier: "ver",
		RedirectURI: "https://example.test/v1/connectors/google/callback",
		RedirectTo:  "/settings/connectors",
		ExpiresAt:   now.Add(10 * time.Minute),
		CreatedAt:   now,
	}); err != nil {
		t.Fatalf("create state: %v", err)
	}
	st, err := store.TakeState(ctx, "st-1")
	if err != nil || st.Verifier != "ver" {
		t.Fatalf("take state: %+v err=%v", st, err)
	}
	if _, err := store.TakeState(ctx, "st-1"); !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("state should be single-use, got %v", err)
	}

	g, err := store.UpsertGrant(ctx, Grant{
		UserID: user.Username, Kind: KindGoogle, Subject: "sub-1", Email: "a@example.com",
		AccessToken: "access", RefreshToken: "refresh", Scopes: DefaultScopes,
	})
	if err != nil {
		t.Fatalf("upsert grant: %v", err)
	}
	if g.Email != "a@example.com" {
		t.Fatalf("grant email = %q", g.Email)
	}

	tok, err := svc.AccessToken(ctx, user.Username)
	if err != nil || tok != "access" {
		t.Fatalf("access token = %q err=%v", tok, err)
	}

	if err := svc.Disconnect(ctx, user.Username); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if _, err := svc.AccessToken(ctx, user.Username); !errors.Is(err, ErrNotConnected) {
		t.Fatalf("expected ErrNotConnected, got %v", err)
	}

	// Client should still be present after disconnect.
	c2, err := svc.GetClient(ctx, user.Username)
	if err != nil || c2.ClientID != "cid-1" {
		t.Fatalf("client after disconnect: %+v err=%v", c2, err)
	}

	if err := svc.DeleteClient(ctx, user.Username); err != nil {
		t.Fatalf("delete client: %v", err)
	}
	if _, err := svc.GetClient(ctx, user.Username); !errors.Is(err, ErrNoClient) {
		t.Fatalf("expected ErrNoClient after delete, got %v", err)
	}
}

func TestAccessTokenRefreshesNearExpiry(t *testing.T) {
	ctx := context.Background()
	store, db := pgStore(t)
	userStore := storage.NewUserStore(db)
	user := storage.User{Username: "conn-refresh-user", Email: "conn-refresh@example.com", APIKey: "rp-conn-ref", Role: storage.RoleUser}
	if err := userStore.Upsert(ctx, user); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	t.Cleanup(func() { _ = userStore.Delete(ctx, user.Username) })

	if _, err := store.SaveClient(ctx, user.Username, "cid", "sec"); err != nil {
		t.Fatalf("save client: %v", err)
	}
	exp := time.Date(2026, 10, 1, 12, 2, 0, 0, time.UTC)
	g, err := store.UpsertGrant(ctx, Grant{
		UserID: user.Username, Kind: KindGoogle, Subject: "sub",
		AccessToken: "old", RefreshToken: "rt", TokenExpiresAt: &exp, Scopes: "s",
	})
	if err != nil {
		t.Fatalf("grant: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Errorf("grant_type=%q", r.Form.Get("grant_type"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "new-access",
			"expires_in":   3600,
			"scope":        "s",
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	// Patch token URL via a custom Service that uses a rewritten transport.
	// We call refresh directly through AccessToken by temporarily swapping constants
	// is not possible; instead exercise SaveGrantTokens + needsRefresh path with a stub HTTP
	// that intercepts oauth2.googleapis.com — use Service.refresh via AccessToken with
	// a custom RoundTripper.
	svc := &Service{
		Store: store,
		Now:   func() time.Time { return time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC) },
		HTTP: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			if strings.Contains(req.URL.Host, "googleapis.com") && req.Method == http.MethodPost {
				rr := httptest.NewRecorder()
				mux.ServeHTTP(rr, req)
				return rr.Result(), nil
			}
			return nil, errors.New("unexpected request " + req.URL.String())
		})},
	}

	tok, err := svc.AccessToken(ctx, user.Username)
	if err != nil {
		t.Fatalf("AccessToken: %v", err)
	}
	if tok != "new-access" {
		t.Fatalf("token = %q", tok)
	}
	g2, err := store.GetGrant(ctx, user.Username, KindGoogle)
	if err != nil {
		t.Fatalf("get grant: %v", err)
	}
	if g2.AccessToken != "new-access" {
		t.Fatalf("persisted token = %q", g2.AccessToken)
	}
	_ = g
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSafeRedirect(t *testing.T) {
	if got := safeRedirect(""); got != "/settings/connectors" {
		t.Fatalf("empty = %q", got)
	}
	if got := safeRedirect("https://evil.example/"); got != "/settings/connectors" {
		t.Fatalf("absolute = %q", got)
	}
	if got := safeRedirect("/settings/connectors?x=1"); got != "/settings/connectors?x=1" {
		t.Fatalf("ok = %q", got)
	}
}

type relayInstall struct{}

func (relayInstall) MintToken() (string, error) { return "rp1.test.1.sig", nil }
func (relayInstall) PublicKeyHex() string       { return strings.Repeat("ab", 32) }

func TestRelayConnectAndRefresh(t *testing.T) {
	ctx := context.Background()
	store, db := pgStore(t)
	userStore := storage.NewUserStore(db)
	user := storage.User{Username: "conn-relay-user", Email: "conn-relay@example.com", APIKey: "rp-conn-relay", Role: storage.RoleUser}
	if err := userStore.Upsert(ctx, user); err != nil {
		t.Fatalf("upsert user: %v", err)
	}
	t.Cleanup(func() { _ = userStore.Delete(ctx, user.Username) })

	var sawState string
	relay := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer rp1.test.1.sig" {
			t.Errorf("auth = %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/oauth/google/start":
			var body struct {
				Return string `json:"return"`
				State  string `json:"state"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			sawState = body.State
			_ = json.NewEncoder(w).Encode(map[string]string{
				"authorizeUrl": "https://accounts.google.com/o/oauth2/v2/auth?state=sealed",
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/tickets/"):
			if r.URL.Query().Get("state") != sawState {
				t.Errorf("ticket state = %q", r.URL.Query().Get("state"))
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"provider": "google", "subject": "sub-relay", "email": "a@gmail.com",
				"accessToken": "ya29.access", "refreshToken": "1//refresh",
				"scope": DefaultScopes, "expiresIn": 3600,
			})
		case r.Method == http.MethodPost && r.URL.Path == "/oauth/google/refresh":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"accessToken": "ya29.new", "expiresIn": 3600,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(relay.Close)

	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	svc := &Service{
		Store:   store,
		Relay:   relay.URL,
		Install: relayInstall{},
		HTTP:    relay.Client(),
		Now:     func() time.Time { return now },
	}
	raw, err := svc.StartConnect(ctx, user.Username, "https", "console.test", "/settings/connectors")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, "accounts.google.com") || sawState == "" {
		t.Fatalf("authorize %q state %q", raw, sawState)
	}
	redirectTo, err := svc.HandleCloudCallback(ctx, "abc", sawState)
	if err != nil {
		t.Fatal(err)
	}
	if redirectTo != "/settings/connectors" {
		t.Fatalf("redirect %q", redirectTo)
	}
	g, err := store.GetGrant(ctx, user.Username, KindGoogle)
	if err != nil {
		t.Fatal(err)
	}
	if g.Source != GrantSourceRelay || g.Subject != "sub-relay" || g.AccessToken != "ya29.access" {
		t.Fatalf("grant %+v", g)
	}

	svc.Now = func() time.Time { return now.Add(2 * time.Hour) }
	tok, err := svc.AccessToken(ctx, user.Username)
	if err != nil {
		t.Fatal(err)
	}
	if tok != "ya29.new" {
		t.Fatalf("refreshed %q", tok)
	}
}

func TestCallbackURL(t *testing.T) {
	got := CallbackURL("https", "roundpen.example")
	want := "https://roundpen.example/v1/connectors/google/callback"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
