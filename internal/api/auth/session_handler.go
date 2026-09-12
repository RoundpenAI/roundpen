package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

const (
	loginFailLimit  = 10
	loginFailWindow = 15 * time.Minute
)

// SessionHandler serves register/login/logout/password/apikey rotate.
type SessionHandler struct {
	users             storage.UserStore
	sessions          storage.SessionStore
	allowRegistration func() bool
	limiter           *loginLimiter
}

// NewSessionHandler constructs a session handler. Public registration is off when allowRegistration is nil.
func NewSessionHandler(users storage.UserStore, sessions storage.SessionStore, allowRegistration func() bool) *SessionHandler {
	if allowRegistration == nil {
		allowRegistration = func() bool { return false }
	}
	return &SessionHandler{
		users:             users,
		sessions:          sessions,
		allowRegistration: allowRegistration,
		limiter:           newLoginLimiter(),
	}
}

type registerRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Username string `json:"username"`
	FullName string `json:"fullname"`
}

func (h *SessionHandler) Register(w http.ResponseWriter, r *http.Request) {
	if !h.allowRegistration() {
		writeErr(w, http.StatusForbidden, "public registration is disabled")
		return
	}
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Password) < minPasswordLen {
		writeErr(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	email := ""
	if strings.TrimSpace(req.Email) != "" {
		var err error
		email, err = normalizeEmail(req.Email)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "invalid email")
			return
		}
		if _, err := h.users.GetByEmail(r.Context(), email); err == nil {
			writeErr(w, http.StatusConflict, "email already registered")
			return
		} else if err != storage.ErrNotFound {
			writeErr(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	username := strings.TrimSpace(req.Username)
	if username == "" && email != "" {
		username = usernameFromEmail(email)
	}
	if !validUsername.MatchString(username) {
		writeErr(w, http.StatusBadRequest, "invalid username")
		return
	}
	username, err := uniqueUsername(r.Context(), h.users, username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	id, err := NewID()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	user := storage.User{
		Username:     username,
		Email:        email,
		FullName:     strings.TrimSpace(req.FullName),
		APIKey:       APIKeyPrefix + id,
		Role:         storage.RoleUser,
		PasswordHash: hash,
		AuthProvider: "local",
	}
	if err := h.users.Upsert(r.Context(), user); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if err := h.issueSession(w, r, &user); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	masked := user
	masked.APIKey = maskAPIKey(masked.APIKey)
	writeJSON(w, http.StatusCreated, masked)
}

type loginRequest struct {
	User     string `json:"user"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *SessionHandler) Login(w http.ResponseWriter, r *http.Request) {
	ip := clientIP(r)
	if h.limiter.blocked(ip) {
		writeErr(w, http.StatusTooManyRequests, "too many login attempts")
		return
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ident := strings.TrimSpace(firstNonEmpty(req.User, req.Username, req.Email))
	if ident == "" || req.Password == "" {
		writeErr(w, http.StatusBadRequest, "user and password are required")
		return
	}
	user, err := lookupLoginUser(r.Context(), h.users, ident)
	if err != nil {
		h.limiter.fail(ip)
		writeErr(w, http.StatusUnauthorized, "invalid user or password")
		return
	}
	if user.PasswordHash == "" {
		writeErr(w, http.StatusUnauthorized, "password not set; ask an admin to reset")
		return
	}
	if !CheckPassword(user.PasswordHash, req.Password) {
		h.limiter.fail(ip)
		writeErr(w, http.StatusUnauthorized, "invalid user or password")
		return
	}
	h.limiter.clear(ip)
	if err := h.issueSession(w, r, user); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	masked := *user
	masked.APIKey = maskAPIKey(masked.APIKey)
	writeJSON(w, http.StatusOK, map[string]any{"user": masked})
}

func lookupLoginUser(ctx context.Context, users storage.UserStore, ident string) (*storage.User, error) {
	if strings.Contains(ident, "@") {
		email, err := normalizeEmail(ident)
		if err == nil {
			u, err := users.GetByEmail(ctx, email)
			if err == nil {
				return u, nil
			}
			if err != storage.ErrNotFound {
				return nil, err
			}
		}
	}
	u, err := users.GetByUsername(ctx, ident)
	if err == nil {
		return u, nil
	}
	if err != storage.ErrNotFound {
		return nil, err
	}
	if email, nerr := normalizeEmail(ident); nerr == nil {
		return users.GetByEmail(ctx, email)
	}
	return nil, storage.ErrNotFound
}

func (h *SessionHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookieName); err == nil && c.Value != "" && h.sessions != nil {
		if sess, err := h.sessions.GetByTokenHash(r.Context(), HashSessionToken(c.Value)); err == nil {
			_ = h.sessions.Delete(r.Context(), sess.ID)
		}
	}
	clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

func (h *SessionHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.NewPassword) < minPasswordLen {
		writeErr(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	fresh, err := h.users.GetByUsername(r.Context(), user.Username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if fresh.PasswordHash != "" && !CheckPassword(fresh.PasswordHash, req.CurrentPassword) {
		writeErr(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}
	hash, err := HashPassword(req.NewPassword)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	fresh.PasswordHash = hash
	if err := h.users.Upsert(r.Context(), *fresh); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if h.sessions != nil {
		_ = h.sessions.DeleteByUser(r.Context(), fresh.Username)
	}
	if err := h.issueSession(w, r, fresh); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *SessionHandler) RotateAPIKey(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	fresh, err := h.users.GetByUsername(r.Context(), user.Username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	id, err := NewID()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	fresh.APIKey = APIKeyPrefix + id
	if err := h.users.Upsert(r.Context(), *fresh); err != nil {
		writeErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"username": fresh.Username,
		"apiKey":   fresh.APIKey,
	})
}

func (h *SessionHandler) issueSession(w http.ResponseWriter, r *http.Request, user *storage.User) error {
	if h.sessions == nil {
		return nil
	}
	plain, hash, err := NewSessionToken()
	if err != nil {
		return err
	}
	id, err := NewID()
	if err != nil {
		return err
	}
	now := time.Now()
	sess := storage.Session{
		ID:         id,
		UserID:     user.Username,
		TokenHash:  hash,
		ExpiresAt:  now.Add(SessionTTL),
		CreatedAt:  now,
		LastSeenAt: now,
		UserAgent:  r.UserAgent(),
		IP:         clientIP(r),
	}
	if err := h.sessions.Create(r.Context(), sess); err != nil {
		return err
	}
	setSessionCookie(w, r, plain, int(SessionTTL.Seconds()))
	return nil
}

func normalizeEmail(raw string) (string, error) {
	email := strings.TrimSpace(strings.ToLower(raw))
	if email == "" {
		return "", errInvalidEmail
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return "", errInvalidEmail
	}
	return email, nil
}

type invalidEmail struct{}

func (invalidEmail) Error() string { return "invalid email" }

var errInvalidEmail invalidEmail

var (
	validUsername = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,64}$`)
	nonUsername   = regexp.MustCompile(`[^a-zA-Z0-9._-]+`)
)

func usernameFromEmail(email string) string {
	local := email
	if i := strings.Index(email, "@"); i > 0 {
		local = email[:i]
	}
	s := nonUsername.ReplaceAllString(local, "_")
	s = strings.Trim(s, "._-")
	if s == "" || !validUsername.MatchString(s) {
		s = "user"
	}
	if len(s) > 64 {
		s = s[:64]
	}
	return s
}

func uniqueUsername(ctx context.Context, users storage.UserStore, base string) (string, error) {
	if _, err := users.GetByUsername(ctx, base); err == storage.ErrNotFound {
		return base, nil
	} else if err != nil {
		return "", err
	}
	for i := 2; i < 1000; i++ {
		candidate := base
		suffix := "-" + strconv.Itoa(i)
		if len(candidate)+len(suffix) > 64 {
			candidate = candidate[:64-len(suffix)]
		}
		candidate += suffix
		if _, err := users.GetByUsername(ctx, candidate); err == storage.ErrNotFound {
			return candidate, nil
		} else if err != nil {
			return "", err
		}
	}
	return "", errors.New("username exhausted")
}

func clientIP(r *http.Request) string {
	return httpx.DefaultTrust.ClientIP(r)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

type loginLimiter struct {
	mu    sync.Mutex
	fails map[string][]time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{fails: make(map[string][]time.Time)}
}

func (l *loginLimiter) blocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(ip)
	return len(l.fails[ip]) >= loginFailLimit
}

func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.pruneLocked(ip)
	l.fails[ip] = append(l.fails[ip], time.Now())
}

func (l *loginLimiter) clear(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, ip)
}

func (l *loginLimiter) pruneLocked(ip string) {
	cutoff := time.Now().Add(-loginFailWindow)
	times := l.fails[ip]
	i := 0
	for _, t := range times {
		if t.After(cutoff) {
			times[i] = t
			i++
		}
	}
	if i == 0 {
		delete(l.fails, ip)
		return
	}
	l.fails[ip] = times[:i]
}
