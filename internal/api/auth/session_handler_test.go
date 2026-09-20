package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

func newSessionTestMux(t *testing.T, allowReg bool) (http.Handler, storage.UserStore) {
	t.Helper()
	users := storage.NewMemoryUserStore()
	sessions := storage.NewMemorySessionStore()
	mux := http.NewServeMux()
	Mount(mux, users, sessions, func() bool { return allowReg })
	return Middleware(users, sessions)(mux), users
}

func TestSessionFlow_RegisterLoginUserLogout(t *testing.T) {
	h, _ := newSessionTestMux(t, true)

	regBody, _ := json.Marshal(map[string]string{
		"email":    "alice@example.com",
		"password": "secret123",
		"fullname": "Alice",
	})
	req := httptest.NewRequest("POST", "/v1/auth/register", bytes.NewReader(regBody))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register status = %d body=%s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	var sessionCookie *http.Cookie
	for _, c := range cookies {
		if c.Name == SessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected roundpen_session cookie")
	}

	req = httptest.NewRequest("GET", "/v1/auth/user", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("auth/user status = %d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest("POST", "/v1/auth/logout", nil)
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d", rec.Code)
	}

	loginBody, _ := json.Marshal(map[string]string{
		"user":     "alice@example.com",
		"password": "secret123",
	})
	req = httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(loginBody))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSessionFlow_LoginWithUsername(t *testing.T) {
	h, users := newSessionTestMux(t, false)
	hash, err := HashPassword("secret123")
	if err != nil {
		t.Fatal(err)
	}
	if err := users.Upsert(t.Context(), storage.User{
		Username:     "bob",
		Email:        "bob@example.com",
		APIKey:       "rp-bobkey",
		Role:         storage.RoleUser,
		PasswordHash: hash,
	}); err != nil {
		t.Fatal(err)
	}

	loginBody, _ := json.Marshal(map[string]string{
		"user":     "bob",
		"password": "secret123",
	})
	req := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(loginBody))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(rec.Result().Cookies()) == 0 {
		t.Fatal("expected session cookie")
	}
}

func TestLoginLimiter_UsernameDimension(t *testing.T) {
	l := newLoginLimiter()
	userKey := loginLimitKeyUser("Victim")
	if l.blocked(userKey) {
		t.Fatal("fresh limiter should not block")
	}
	for i := 0; i < loginFailLimit; i++ {
		l.fail(userKey)
	}
	if !l.blocked(userKey) {
		t.Fatal("username bucket should block after the limit")
	}
	// The key is case-insensitive, so spelling variants share one bucket.
	if !l.blocked(loginLimitKeyUser("victim")) {
		t.Fatal("username key should be case-insensitive")
	}
	// Unrelated usernames and IPs are untouched.
	if l.blocked(loginLimitKeyUser("someone-else")) {
		t.Fatal("unrelated username should not be blocked")
	}
	if l.blocked(loginLimitKeyIP("10.0.0.1")) {
		t.Fatal("unrelated IP should not be blocked")
	}
	l.clear(userKey)
	if l.blocked(userKey) {
		t.Fatal("clear should lift the block")
	}
}

func TestLoginLimiter_MultiKey(t *testing.T) {
	l := newLoginLimiter()
	ipKey := loginLimitKeyIP("10.0.0.5")
	userKey := loginLimitKeyUser("dave")
	for i := 0; i < loginFailLimit; i++ {
		l.fail(ipKey, userKey)
	}
	if !l.blocked(ipKey) || !l.blocked(userKey) {
		t.Fatal("both buckets should block after the limit")
	}
	// blocked reports true when ANY supplied key has tripped.
	if !l.blocked(loginLimitKeyIP("1.2.3.4"), userKey) {
		t.Fatal("blocked should be true when any key trips")
	}
}

// TestLogin_UsernameLimitAcrossIPs proves the per-account bucket throttles a
// distributed brute force: each attempt comes from a fresh IP (so no single IP
// bucket trips), yet the shared username bucket still locks the account.
func TestLogin_UsernameLimitAcrossIPs(t *testing.T) {
	h, users := newSessionTestMux(t, false)
	hash, err := HashPassword("secret123")
	if err != nil {
		t.Fatal(err)
	}
	if err := users.Upsert(t.Context(), storage.User{
		Username: "erin", Email: "erin@example.com", APIKey: "rp-erin",
		Role: storage.RoleUser, PasswordHash: hash,
	}); err != nil {
		t.Fatal(err)
	}

	attempt := func(ip, password string) int {
		body, _ := json.Marshal(map[string]string{"user": "erin", "password": password})
		req := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(body))
		req.RemoteAddr = ip + ":1234"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	for i := 0; i < loginFailLimit; i++ {
		if code := attempt(fmt.Sprintf("10.0.0.%d", i+1), "wrong-password"); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i, code)
		}
	}
	// The next attempt is throttled even from a brand-new IP and even with the
	// correct password, because the username bucket has tripped.
	if code := attempt("10.0.1.99", "secret123"); code != http.StatusTooManyRequests {
		t.Fatalf("throttled status = %d, want 429", code)
	}
}

// TestLogin_OAuthOnlyAccountIsGeneric401 covers an account created via
// federated login (no local password). It must return the same generic 401 as
// a wrong password, not a message that reveals the account exists.
func TestLogin_OAuthOnlyAccountIsGeneric401(t *testing.T) {
	h, users := newSessionTestMux(t, false)
	if err := users.Upsert(t.Context(), storage.User{
		Username: "oauthuser", Email: "oauth@example.com", APIKey: "rp-oauth",
		Role: storage.RoleUser, AuthProvider: "gitea", // PasswordHash intentionally empty
	}); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"user": "oauthuser", "password": "whatever1"})
	req := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d body=%s, want 401", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "invalid user or password") {
		t.Fatalf("body = %s, want the generic message", rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "password not set") {
		t.Fatalf("body = %s leaks account state", rec.Body.String())
	}
}

func TestSessionFlow_APIKeyStillWorks(t *testing.T) {
	h, users := newSessionTestMux(t, false)
	if err := users.Upsert(t.Context(), storage.User{
		Username: "bob", Email: "bob@example.com", APIKey: "rp-bobkey", Role: storage.RoleUser,
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/v1/auth/user", nil)
	req.Header.Set("X-API-Key", "rp-bobkey")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSessionFlow_BearerAPIKey(t *testing.T) {
	h, users := newSessionTestMux(t, false)
	if err := users.Upsert(t.Context(), storage.User{
		Username: "bob", APIKey: "rp-bobkey", Role: storage.RoleUser,
	}); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/v1/auth/user", nil)
	req.Header.Set("Authorization", "Bearer rp-bobkey")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRegister_Disabled(t *testing.T) {
	h, _ := newSessionTestMux(t, false)
	body, _ := json.Marshal(map[string]string{"username": "a", "password": "secret123"})
	req := httptest.NewRequest("POST", "/v1/auth/register", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestHealthIsPublic(t *testing.T) {
	users := storage.NewMemoryUserStore()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	wrapped := Middleware(users, storage.NewMemorySessionStore())(mux)
	req := httptest.NewRequest("GET", "/health", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status = %d", rec.Code)
	}
}

func TestChangePassword(t *testing.T) {
	h, users := newSessionTestMux(t, false)
	hash, err := HashPassword("old-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := users.Upsert(t.Context(), storage.User{
		Username:     "carol",
		Email:        "carol@example.com",
		APIKey:       "rp-carol",
		Role:         storage.RoleUser,
		PasswordHash: hash,
	}); err != nil {
		t.Fatal(err)
	}

	loginBody, _ := json.Marshal(map[string]string{
		"user": "carol", "password": "old-secret",
	})
	req := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(loginBody))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookieName {
			sessionCookie = c
			break
		}
	}
	if sessionCookie == nil {
		t.Fatal("expected session cookie")
	}

	wrongBody, _ := json.Marshal(map[string]string{
		"current_password": "nope", "new_password": "new-secret1",
	})
	req = httptest.NewRequest("POST", "/v1/auth/password", bytes.NewReader(wrongBody))
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong current password status = %d", rec.Code)
	}

	okBody, _ := json.Marshal(map[string]string{
		"current_password": "old-secret", "new_password": "new-secret1",
	})
	req = httptest.NewRequest("POST", "/v1/auth/password", bytes.NewReader(okBody))
	req.AddCookie(sessionCookie)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("change password status = %d body=%s", rec.Code, rec.Body.String())
	}
	fresh, err := users.GetByUsername(t.Context(), "carol")
	if err != nil || !CheckPassword(fresh.PasswordHash, "new-secret1") {
		t.Fatalf("password not updated: err=%v", err)
	}

	loginBody, _ = json.Marshal(map[string]string{
		"user": "carol", "password": "new-secret1",
	})
	req = httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(loginBody))
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("login with new password status = %d", rec.Code)
	}
}

func TestEnsurePassword(t *testing.T) {
	users := storage.NewMemoryUserStore()
	u := storage.User{Username: "admin", Email: "admin@example.com", APIKey: "rp-x", Role: storage.RoleAdmin}
	if err := users.Upsert(t.Context(), u); err != nil {
		t.Fatal(err)
	}
	got, _ := users.GetByUsername(t.Context(), "admin")
	plain, generated, err := EnsurePassword(t.Context(), users, got)
	if err != nil {
		t.Fatal(err)
	}
	if !generated || plain == "" {
		t.Fatal("expected generated password")
	}
	fresh, _ := users.GetByUsername(t.Context(), "admin")
	if !CheckPassword(fresh.PasswordHash, plain) {
		t.Fatal("stored hash does not match generated password")
	}
	_, generated, err = EnsurePassword(t.Context(), users, fresh)
	if err != nil || generated {
		t.Fatal("second call should not regenerate")
	}
}

func TestSandboxRequiresAuth(t *testing.T) {
	h, _ := newSessionTestMux(t, false)
	req := httptest.NewRequest("POST", "/v1/sandboxes", bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}
