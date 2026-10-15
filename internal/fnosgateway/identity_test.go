//go:build fnos

package fnosgateway

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

func TestFnosUsername(t *testing.T) {
	cases := []struct{ in, want string }{
		{"1000", "fnos-1000"},
		{"1000 ", "fnos-1000"},
		{"UID-42", "fnos-uid-42"},
		{"a.b", "fnos-ab"},
		{"", "fnos-unknown"},
		{"   ", "fnos-unknown"},
		{"??", "fnos-unknown"},
	}
	for _, tc := range cases {
		if got := fnosUsername(tc.in); got != tc.want {
			t.Errorf("fnosUsername(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestRoleForIsAdmin(t *testing.T) {
	cases := []struct {
		in   string
		want storage.UserRole
	}{
		{"true", storage.RoleAdmin},
		{"TRUE", storage.RoleAdmin},
		{" true ", storage.RoleAdmin},
		{"false", storage.RoleUser},
		{"1", storage.RoleUser},
		{"", storage.RoleUser},
	}
	for _, tc := range cases {
		if got := roleForIsAdmin(tc.in); got != tc.want {
			t.Errorf("roleForIsAdmin(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A derived name held by a non-fnOS account must never be taken over.
func TestGatewayIdentity_FailClosedOnForeignAccount(t *testing.T) {
	users := storage.NewMemoryUserStore()
	sessions := storage.NewMemorySessionStore()
	if err := users.Upsert(t.Context(), storage.User{
		Username: "fnos-1000", Role: storage.RoleUser, AuthProvider: "local",
		PasswordHash: "hash", FullName: "Local Owner",
	}); err != nil {
		t.Fatal(err)
	}

	nextCalled := false
	h := gatewayIdentity(users, sessions)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	req.Header.Set("X-Trim-Isadmin", "true")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if nextCalled {
		t.Fatal("next handler ran on a fail-closed request")
	}
	got, err := users.GetByUsername(t.Context(), "fnos-1000")
	if err != nil {
		t.Fatal(err)
	}
	if got.AuthProvider != "local" || got.FullName != "Local Owner" || got.Role != storage.RoleUser {
		t.Fatalf("foreign account was modified: %+v", got)
	}
}

func TestGatewayIdentity_CreatesAndSignsIn(t *testing.T) {
	users := storage.NewMemoryUserStore()
	sessions := storage.NewMemorySessionStore()

	var gotUser *storage.User
	var gotCookie bool
	downstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser = auth.GetUser(r.Context())
		_, cErr := r.Cookie(auth.SessionCookieName)
		gotCookie = cErr == nil
		w.WriteHeader(http.StatusOK)
	})
	h := gatewayIdentity(users, sessions)(auth.Middleware(users, sessions)(downstream))

	req := httptest.NewRequest(http.MethodGet, "/v1/whoami", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	req.Header.Set("X-Trim-Isadmin", "true")
	req.Header.Set("X-Trim-Username", "Zhang San")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%q, want 200", rec.Code, rec.Body.String())
	}
	if !gotCookie {
		t.Fatal("session cookie was not injected into the request")
	}
	if gotUser == nil {
		t.Fatal("downstream auth middleware did not resolve a user")
	}
	if gotUser.Username != "fnos-1000" || gotUser.Role != storage.RoleAdmin {
		t.Fatalf("resolved user = %+v, want fnos-1000 admin", gotUser)
	}
	if !strings.HasPrefix(rec.Header().Get("Set-Cookie"), auth.SessionCookieName+"=") {
		t.Fatalf("Set-Cookie = %q", rec.Header().Get("Set-Cookie"))
	}
	stored, err := users.GetByUsername(t.Context(), "fnos-1000")
	if err != nil {
		t.Fatal(err)
	}
	if stored.AuthProvider != "fnos" || stored.FullName != "Zhang San" || stored.APIKey == "" {
		t.Fatalf("stored user = %+v", stored)
	}
}

func TestGatewayIdentity_ReconcilesAdminRole(t *testing.T) {
	users := storage.NewMemoryUserStore()
	sessions := storage.NewMemorySessionStore()
	if err := users.Upsert(t.Context(), storage.User{
		Username: "fnos-2000", Role: storage.RoleAdmin, AuthProvider: "fnos",
	}); err != nil {
		t.Fatal(err)
	}

	h := gatewayIdentity(users, sessions)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Trim-Userid", "2000")
	req.Header.Set("X-Trim-Isadmin", "false")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	got, err := users.GetByUsername(t.Context(), "fnos-2000")
	if err != nil {
		t.Fatal(err)
	}
	if got.Role != storage.RoleUser {
		t.Fatalf("role = %q, want user", got.Role)
	}
}

// countingSessions records how many sessions were minted.
type countingSessions struct {
	storage.SessionStore
	created int
}

func (c *countingSessions) Create(ctx context.Context, s storage.Session) error {
	c.created++
	return c.SessionStore.Create(ctx, s)
}

// A request carrying a valid session must not mint another one.
func TestGatewayIdentity_ReusesExistingSession(t *testing.T) {
	users := storage.NewMemoryUserStore()
	if err := users.Upsert(t.Context(), storage.User{
		Username: "fnos-1000", Role: storage.RoleUser, AuthProvider: "fnos",
	}); err != nil {
		t.Fatal(err)
	}
	sessions := &countingSessions{SessionStore: storage.NewMemorySessionStore()}
	plain, hash, err := auth.NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := sessions.Create(t.Context(), storage.Session{
		ID: "sess-existing", UserID: "fnos-1000", TokenHash: hash,
		ExpiresAt: now.Add(time.Hour), CreatedAt: now, LastSeenAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	sessions.created = 0 // ignore the seeded row

	h := gatewayIdentity(users, sessions)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Trim-Userid", "1000")
	req.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: plain})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if sessions.created != 0 {
		t.Fatalf("minted %d new sessions for a request with a valid cookie, want 0", sessions.created)
	}
}

func TestGatewayIdentity_SkipsPublicAuthEndpoints(t *testing.T) {
	users := storage.NewMemoryUserStore()
	sessions := &countingSessions{SessionStore: storage.NewMemorySessionStore()}
	nextCalled := false
	h := gatewayIdentity(users, sessions)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		nextCalled = true
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader("{}"))
	req.Header.Set("X-Trim-Userid", "1000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if !nextCalled {
		t.Fatal("public auth endpoint was intercepted instead of passed through")
	}
	if sessions.created != 0 {
		t.Fatalf("minted %d sessions for a public auth endpoint, want 0", sessions.created)
	}
	if _, err := users.GetByUsername(t.Context(), "fnos-1000"); err == nil {
		t.Fatal("gateway provisioned a user for a public auth endpoint")
	}
}

func TestStripGatewayPrefix(t *testing.T) {
	cases := []struct{ prefix, path, want string }{
		{"/app/roundpen", "/app/roundpen/v1/whoami", "/v1/whoami"},
		{"/app/roundpen", "/app/roundpen", "/"},
		{"/app/roundpen", "/v1/whoami", "/v1/whoami"},
		{"", "/v1/whoami", "/v1/whoami"},
	}
	for _, tc := range cases {
		var got string
		next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { got = r.URL.Path })
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		stripGatewayPrefix(tc.prefix, next).ServeHTTP(httptest.NewRecorder(), req)
		if got != tc.want {
			t.Errorf("stripGatewayPrefix(%q) %q => %q, want %q", tc.prefix, tc.path, got, tc.want)
		}
	}
}
