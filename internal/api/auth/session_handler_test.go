package auth

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
