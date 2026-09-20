package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

func TestIsPublicPath(t *testing.T) {
	public := []struct{ method, path string }{
		{http.MethodGet, "/health"},
		{http.MethodGet, "/v1/ready"},
		{http.MethodPost, "/v1/auth/login"},
		{http.MethodPost, "/v1/auth/register"},
		{http.MethodGet, "/v1/auth/oauth/providers"},
		{http.MethodGet, "/v1/auth/oauth/gitea-git-eaxi-com/start"},
		{http.MethodGet, "/v1/auth/oauth/gitea-git-eaxi-com/callback"},
		{http.MethodGet, "/settings/accounts"},
	}
	for _, tc := range public {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if !isPublicPath(req) {
			t.Errorf("%s %s should be public", tc.method, tc.path)
		}
	}

	private := []struct{ method, path string }{
		{http.MethodGet, "/v1/auth/user"},
		{http.MethodPost, "/v1/auth/logout"},
		{http.MethodPost, "/v1/auth/oauth/providers"},
		{http.MethodGet, "/v1/me/identities"},
		{http.MethodPost, "/v1/me/identities/link/gitea-git-eaxi-com"},
		{http.MethodDelete, "/v1/me/identities/id-1"},
		{http.MethodGet, "/v1/admin/oauth/providers"},
		{http.MethodPut, "/v1/admin/oauth/providers"},
		{http.MethodDelete, "/v1/admin/oauth/providers/gitea-git-eaxi-com"},
	}
	for _, tc := range private {
		req := httptest.NewRequest(tc.method, tc.path, nil)
		if isPublicPath(req) {
			t.Errorf("%s %s must stay authenticated", tc.method, tc.path)
		}
	}
}

func newWhoamiMux(users storage.UserStore, sessions storage.SessionStore) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/whoami", func(w http.ResponseWriter, r *http.Request) {
		u := GetUser(r.Context())
		if u == nil {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(u.Username))
	})
	return Middleware(users, sessions)(mux)
}

func seedSession(t *testing.T, users storage.UserStore, sessions storage.SessionStore, username string, expiry time.Time) string {
	t.Helper()
	if err := users.Upsert(t.Context(), storage.User{Username: username, Role: storage.RoleUser}); err != nil {
		t.Fatal(err)
	}
	plain, hash, err := NewSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	if err := sessions.Create(t.Context(), storage.Session{
		ID: "sess-" + username, UserID: username, TokenHash: hash,
		ExpiresAt: expiry, CreatedAt: now, LastSeenAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	return plain
}

func getWithHeader(h http.Handler, name, value string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", "/v1/whoami", nil)
	if name != "" {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestMiddleware_BearerSessionToken(t *testing.T) {
	users := storage.NewMemoryUserStore()
	sessions := storage.NewMemorySessionStore()
	token := seedSession(t, users, sessions, "bob", time.Now().Add(time.Hour))
	h := newWhoamiMux(users, sessions)

	rec := getWithHeader(h, "Authorization", "Bearer "+token)
	if rec.Code != http.StatusOK || rec.Body.String() != "bob" {
		t.Fatalf("status=%d body=%q, want 200 bob", rec.Code, rec.Body.String())
	}
}

func TestMiddleware_BearerSessionTokenExpired(t *testing.T) {
	users := storage.NewMemoryUserStore()
	sessions := storage.NewMemorySessionStore()
	token := seedSession(t, users, sessions, "bob", time.Now().Add(-time.Minute))
	h := newWhoamiMux(users, sessions)

	rec := getWithHeader(h, "Authorization", "Bearer "+token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

func TestMiddleware_SessionTokenOnlyViaBearer(t *testing.T) {
	users := storage.NewMemoryUserStore()
	sessions := storage.NewMemorySessionStore()
	token := seedSession(t, users, sessions, "bob", time.Now().Add(time.Hour))
	h := newWhoamiMux(users, sessions)

	rec := getWithHeader(h, "X-API-Key", token)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("X-API-Key with a session token: status=%d, want 401", rec.Code)
	}
}

func TestMiddleware_BearerUnknownTokenRejected(t *testing.T) {
	users := storage.NewMemoryUserStore()
	sessions := storage.NewMemorySessionStore()
	_ = seedSession(t, users, sessions, "bob", time.Now().Add(time.Hour))
	h := newWhoamiMux(users, sessions)

	rec := getWithHeader(h, "Authorization", "Bearer "+strings.Repeat("ab", 32))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d, want 401", rec.Code)
	}
}

// Native chat clients authenticate the WebSocket upgrade with a Bearer session
// token (no cookie jar on device), so the middleware must accept it there too.
func TestMiddleware_WSUpgradeWithBearerSessionToken(t *testing.T) {
	users := storage.NewMemoryUserStore()
	sessions := storage.NewMemorySessionStore()
	token := seedSession(t, users, sessions, "bob", time.Now().Add(time.Hour))

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/agent-sessions/{id}/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		user := GetUser(r.Context())
		if user == nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, []byte("hello "+user.Username))
	})
	srv := httptest.NewServer(Middleware(users, sessions)(mux))
	defer srv.Close()
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/agent-sessions/any/ws"

	if _, resp, err := websocket.DefaultDialer.Dial(wsURL, nil); err == nil {
		t.Fatal("dial without credentials should fail")
	} else if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no-credential status = %v, want 401", resp)
	}

	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, http.Header{"Authorization": []string{"Bearer " + token}})
	if err != nil {
		t.Fatalf("dial with bearer session token: %v (status %v)", err, resp)
	}
	defer conn.Close()
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(msg) != "hello bob" {
		t.Fatalf("frame = %q, want %q", msg, "hello bob")
	}
}

func TestMiddleware_BearerAPIKeyStillWorks(t *testing.T) {
	users := storage.NewMemoryUserStore()
	sessions := storage.NewMemorySessionStore()
	if err := users.Upsert(t.Context(), storage.User{
		Username: "bob", APIKey: "rp-bobkey", Role: storage.RoleUser,
	}); err != nil {
		t.Fatal(err)
	}
	h := newWhoamiMux(users, sessions)

	rec := getWithHeader(h, "Authorization", "Bearer rp-bobkey")
	if rec.Code != http.StatusOK || rec.Body.String() != "bob" {
		t.Fatalf("status=%d body=%q, want 200 bob", rec.Code, rec.Body.String())
	}
}
