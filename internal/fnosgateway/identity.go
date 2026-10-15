//go:build fnos

package fnosgateway

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// fnosAuthProvider marks accounts the fnOS gateway created. A username that
// already exists under any other provider is never taken over.
const fnosAuthProvider = "fnos"

// nonFnosUid strips everything outside the local-username charset from an
// injected fnOS uid. uids are numeric in practice, so this is a defensive
// normalisation rather than a transformation.
var nonFnosUid = regexp.MustCompile(`[^a-z0-9-]+`)

// fnosUsername derives the stable Roundpen username for an fnOS uid. Keying on
// the uid (not the display name) keeps the mapping unique and collision-free.
// The fnos- prefix reserves a namespace the gateway owns.
func fnosUsername(uid string) string {
	s := nonFnosUid.ReplaceAllString(strings.ToLower(strings.TrimSpace(uid)), "")
	s = strings.Trim(s, "-")
	if len(s) > 59 {
		s = strings.Trim(s[:59], "-")
	}
	if s == "" {
		s = "unknown"
	}
	return fnosAuthProvider + "-" + s
}

// roleForIsAdmin maps the injected X-Trim-Isadmin flag onto a Roundpen role.
// Only an explicit "true" grants admin; anything else is a plain user.
func roleForIsAdmin(isadmin string) storage.UserRole {
	if strings.EqualFold(strings.TrimSpace(isadmin), "true") {
		return storage.RoleAdmin
	}
	return storage.RoleUser
}

// isGatewayAuthEndpoint reports whether the request targets a public auth
// endpoint that must handle its own credentials rather than being auto-signed
// in by the gateway identity.
func isGatewayAuthEndpoint(r *http.Request) bool {
	if r.Method != http.MethodPost {
		return false
	}
	switch r.URL.Path {
	case "/v1/auth/login", "/v1/auth/register", "/v1/auth/logout":
		return true
	default:
		return false
	}
}

// gatewayIdentity signs an fnOS-forwarded request in and injects the resulting
// session cookie into the request so the downstream auth middleware resolves
// the user by the exact same path as a password login.
//
// The gateway only runs on the fnOS Unix socket; the injected X-Trim-* headers
// are scrubbed from the TCP listener (see stripGatewayHeaders) so they cannot
// be forged from outside.
func gatewayIdentity(users storage.UserStore, sessions storage.SessionStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Never auto-provision on the public auth endpoints: an explicit
			// password login (e.g. the built-in admin) must not be shadowed by
			// the gateway identity. See plan decision 7.
			if isGatewayAuthEndpoint(r) {
				next.ServeHTTP(w, r)
				return
			}
			uid := strings.TrimSpace(r.Header.Get("X-Trim-Userid"))
			if uid == "" {
				// No gateway identity: fall through to password / API key /
				// existing session auth.
				next.ServeHTTP(w, r)
				return
			}
			ctx := r.Context()
			if users == nil || sessions == nil {
				slog.Warn("fnos gateway auth unavailable", slog.String("path", r.URL.Path))
				httpx.WriteErr(w, http.StatusInternalServerError, "internal server error")
				return
			}
			// An existing valid session is enough: never mint a new one per
			// request (the downstream middleware touches it).
			if c, err := r.Cookie(auth.SessionCookieName); err == nil && c.Value != "" {
				if sess, err := sessions.GetByTokenHash(ctx, auth.HashSessionToken(c.Value)); err == nil {
					if _, err := users.GetByUsername(ctx, sess.UserID); err == nil {
						next.ServeHTTP(w, r)
						return
					}
				}
			}
			user, err := resolveGatewayUser(ctx, users, r)
			if err != nil {
				if errors.Is(err, errAccountConflict) {
					httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
					return
				}
				slog.Warn("fnos gateway user", slog.Any("err", err), slog.String("uid", uid))
				httpx.WriteErr(w, http.StatusInternalServerError, "internal server error")
				return
			}
			plain, err := auth.IssueSession(w, r, sessions, user)
			if err != nil {
				slog.Warn("fnos gateway session", slog.Any("err", err), slog.String("uid", uid))
				httpx.WriteErr(w, http.StatusInternalServerError, "internal server error")
				return
			}
			// Inject into this request so auth.Middleware resolves the user from
			// the cookie it would have received on the next one.
			r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: plain, Path: "/"})
			next.ServeHTTP(w, r)
		})
	}
}

// errAccountConflict signals that the derived username is held by a non-fnOS
// account. The gateway fails closed rather than taking it over.
var errAccountConflict = errors.New("fnos username held by another provider")

func resolveGatewayUser(ctx context.Context, users storage.UserStore, r *http.Request) (*storage.User, error) {
	username := fnosUsername(r.Header.Get("X-Trim-Userid"))
	role := roleForIsAdmin(r.Header.Get("X-Trim-Isadmin"))
	fullName := strings.TrimSpace(r.Header.Get("X-Trim-Username"))

	user, err := users.GetByUsername(ctx, username)
	switch {
	case errors.Is(err, storage.ErrNotFound):
		id, idErr := auth.NewID()
		if idErr != nil {
			return nil, idErr
		}
		created := &storage.User{
			Username:     username,
			Email:        "",
			FullName:     fullName,
			Role:         role,
			APIKey:       auth.APIKeyPrefix + id,
			AuthProvider: fnosAuthProvider,
		}
		if err := users.Upsert(ctx, *created); err != nil {
			return nil, err
		}
		return created, nil
	case err != nil:
		return nil, err
	case user.AuthProvider != fnosAuthProvider:
		// A local (or other-provider) account owns this name: never overwrite.
		return nil, errAccountConflict
	default:
		// Reconcile the admin bit only when it actually changed.
		if user.Role != role {
			user.Role = role
			if err := users.Upsert(ctx, *user); err != nil {
				return nil, err
			}
		}
		return user, nil
	}
}
