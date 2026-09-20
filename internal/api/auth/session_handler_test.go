package auth

import (
	"bytes"
	"encoding/json"
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

// seedLoginUser creates a user with a known password and API key.
func seedLoginUser(t *testing.T, users storage.UserStore, username, password, apiKey string) {
	t.Helper()
	hash, err := HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	if err := users.Upsert(t.Context(), storage.User{
		Username:     username,
		Email:        username + "@example.com",
		APIKey:       apiKey,
		Role:         storage.RoleUser,
		PasswordHash: hash,
	}); err != nil {
		t.Fatal(err)
	}
}

func postLogin(t *testing.T, h http.Handler, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/v1/auth/login", bytes.NewReader(raw))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func loginSessionToken(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var resp struct {
		SessionToken string `json:"sessionToken"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	return resp.SessionToken
}

func TestLogin_ReturnsSessionTokenWhenRequested(t *testing.T) {
	h, users := newSessionTestMux(t, false)
	seedLoginUser(t, users, "bob", "secret123", "rp-bobkey")

	rec := postLogin(t, h, map[string]any{
		"user": "bob", "password": "secret123", "returnSessionToken": true,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
	token := loginSessionToken(t, rec)
	if len(token) != 64 {
		t.Fatalf("sessionToken = %q, want 64 hex chars", token)
	}
	if strings.Contains(rec.Body.String(), "rp-bobkey") {
		t.Fatal("login response must never contain the plaintext API key")
	}

	req := httptest.NewRequest("GET", "/v1/auth/user", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Fatalf("bearer session status = %d body=%s", rec2.Code, rec2.Body.String())
	}
}

func TestLogin_OmitsSessionTokenByDefault(t *testing.T) {
	h, users := newSessionTestMux(t, false)
	seedLoginUser(t, users, "bob", "secret123", "rp-bobkey")

	rec := postLogin(t, h, map[string]any{"user": "bob", "password": "secret123"})
	if rec.Code != http.StatusOK {
		t.Fatalf("login status = %d body=%s", rec.Code, rec.Body.String())
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["sessionToken"]; ok {
		t.Fatal("sessionToken must be omitted unless explicitly requested")
	}
}

func TestLogin_FailedPasswordNeverReturnsSessionToken(t *testing.T) {
	h, users := newSessionTestMux(t, false)
	seedLoginUser(t, users, "bob", "secret123", "rp-bobkey")

	rec := postLogin(t, h, map[string]any{
		"user": "bob", "password": "wrong-pass", "returnSessionToken": true,
	})
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if strings.Contains(rec.Body.String(), "sessionToken") {
		t.Fatalf("failed login leaked a token: %s", rec.Body.String())
	}
}

func TestLogin_RateLimitUnaffectedByReturnSessionToken(t *testing.T) {
	h, users := newSessionTestMux(t, false)
	seedLoginUser(t, users, "bob", "secret123", "rp-bobkey")

	for i := 0; i < loginFailLimit; i++ {
		rec := postLogin(t, h, map[string]any{
			"user": "bob", "password": "wrong-pass", "returnSessionToken": true,
		})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d body=%s", i, rec.Code, rec.Body.String())
		}
	}
	rec := postLogin(t, h, map[string]any{
		"user": "bob", "password": "secret123", "returnSessionToken": true,
	})
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
}

func TestLogout_RevokesBearerSession(t *testing.T) {
	h, users := newSessionTestMux(t, false)
	seedLoginUser(t, users, "bob", "secret123", "rp-bobkey")

	rec := postLogin(t, h, map[string]any{
		"user": "bob", "password": "secret123", "returnSessionToken": true,
	})
	token := loginSessionToken(t, rec)
	if token == "" {
		t.Fatalf("no session token in %s", rec.Body.String())
	}

	req := httptest.NewRequest("POST", "/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d body=%s", rec2.Code, rec2.Body.String())
	}

	req = httptest.NewRequest("GET", "/v1/auth/user", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req)
	if rec3.Code != http.StatusUnauthorized {
		t.Fatalf("revoked token status = %d, want 401", rec3.Code)
	}
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
