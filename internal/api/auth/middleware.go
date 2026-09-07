package auth

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

type contextKey string

const (
	userContextKey      contextKey = "user"
	apiKeyContextKey    contextKey = "api_key"
	sessionIDContextKey contextKey = "session_id"
)

// WithUser returns a new context with the User.
func WithUser(ctx context.Context, user *storage.User) context.Context {
	return context.WithValue(ctx, userContextKey, user)
}

// GetUser retrieves the User from context.
func GetUser(ctx context.Context) *storage.User {
	if v, ok := ctx.Value(userContextKey).(*storage.User); ok {
		return v
	}
	return nil
}

// GetAPIKey retrieves the API key from context.
func GetAPIKey(ctx context.Context) string {
	if v, ok := ctx.Value(apiKeyContextKey).(string); ok {
		return v
	}
	return ""
}

func isPublicPath(r *http.Request) bool {
	path := r.URL.Path
	if path == "/health" || path == "/v1/ready" {
		return true
	}
	if strings.HasPrefix(path, "/llmgw/") {
		return true
	}
	// Preview proxy and desktop VNC WS validate their own short-lived token.
	if strings.HasPrefix(path, "/p/") {
		return true
	}
	if path == "/v1/me/environments/browser/desktop/ws" {
		return true
	}
	if r.Method == http.MethodPost {
		switch path {
		case "/v1/auth/register", "/v1/auth/login":
			return true
		}
	}
	// Console SPA + static assets (auth enforced in the browser).
	if isConsolePath(r) {
		return true
	}
	return false
}

// isConsolePath reports whether this request should hit the embedded UI shell.
// API routes (/sandboxes, /v1/*, /p/*, /llmgw/*, /health) stay authenticated.
func isConsolePath(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	path := r.URL.Path
	switch {
	case path == "/health":
		return false
	case path == "/sandboxes" || strings.HasPrefix(path, "/sandboxes/"):
		return false
	case strings.HasPrefix(path, "/v1/"):
		return false
	case strings.HasPrefix(path, "/p/"):
		return false
	case strings.HasPrefix(path, "/llmgw/"):
		return false
	default:
		return true
	}
}

func extractAPIKey(r *http.Request) string {
	got := r.Header.Get("X-API-Key")
	if got == "" {
		got = r.Header.Get("X-API-KEY")
	}
	if got == "" {
		if a := r.Header.Get("Authorization"); strings.HasPrefix(strings.ToLower(a), "bearer ") {
			got = strings.TrimSpace(a[7:])
		}
	}
	return got
}

// Middleware authenticates Cookie session, then X-API-Key / Bearer.
func Middleware(users storage.UserStore, sessions storage.SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isPublicPath(r) {
				next.ServeHTTP(w, r)
				return
			}
			if users == nil {
				next.ServeHTTP(w, r)
				return
			}

			user, key, sessionID := resolveAuth(r, users, sessions)
			if user == nil {
				slog.Warn("unauthorized", slog.String("path", r.URL.Path), slog.String("remote", r.RemoteAddr))
				writeErr(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			ctx := WithUser(r.Context(), user)
			if key != "" {
				ctx = context.WithValue(ctx, apiKeyContextKey, key)
			}
			if sessionID != "" {
				ctx = context.WithValue(ctx, sessionIDContextKey, sessionID)
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func resolveAuth(r *http.Request, users storage.UserStore, sessions storage.SessionStore) (*storage.User, string, string) {
	if sessions != nil {
		if c, err := r.Cookie(SessionCookieName); err == nil && c.Value != "" {
			sess, err := sessions.GetByTokenHash(r.Context(), HashSessionToken(c.Value))
			if err == nil {
				user, err := users.GetByUsername(r.Context(), sess.UserID)
				if err == nil {
					_ = sessions.Touch(r.Context(), sess.ID)
					return user, user.APIKey, sess.ID
				}
			}
		}
	}

	key := extractAPIKey(r)
	if key == "" {
		return nil, "", ""
	}
	user, err := users.GetByAPIKey(r.Context(), key)
	if err == nil {
		return user, key, ""
	}
	return nil, "", ""
}

// RequireAdmin enforces that the authenticated user has the admin role.
func RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := GetUser(r.Context())
		if user == nil || user.Role != storage.RoleAdmin {
			writeErr(w, http.StatusForbidden, "forbidden")
			return
		}
		next(w, r)
	}
}
