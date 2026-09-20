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
		httpx.WriteErr(w, http.StatusForbidden, "public registration is disabled")
		return
	}
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.Password) < minPasswordLen {
		httpx.WriteErr(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	email := ""
	if strings.TrimSpace(req.Email) != "" {
		var err error
		email, err = normalizeEmail(req.Email)
		if err != nil {
			httpx.WriteErr(w, http.StatusBadRequest, "invalid email")
			return
		}
		if _, err := h.users.GetByEmail(r.Context(), email); err == nil {
			httpx.WriteErr(w, http.StatusConflict, "email already registered")
			return
		} else if err != storage.ErrNotFound {
			httpx.WriteErr(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	username := strings.TrimSpace(req.Username)
	if username == "" && email != "" {
		username = usernameFromEmail(email)
	}
	if !validUsername.MatchString(username) {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid username")
		return
	}
	username, err := uniqueUsername(r.Context(), h.users, username)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "internal server error")
		return
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	id, err := NewID()
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "internal server error")
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
		httpx.WriteErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if err := h.issueSession(w, r, &user); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	masked := user
	masked.APIKey = maskAPIKey(masked.APIKey)
	httpx.WriteJSON(w, http.StatusCreated, masked)
}

type loginRequest struct {
	User     string `json:"user"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *SessionHandler) Login(w http.ResponseWriter, r *http.Request) {
	ipKey := loginLimitKeyIP(clientIP(r))
	if h.limiter.blocked(ipKey) {
		httpx.WriteErr(w, http.StatusTooManyRequests, "too many login attempts")
		return
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ident := strings.TrimSpace(firstNonEmpty(req.User, req.Username, req.Email))
	if ident == "" || req.Password == "" {
		httpx.WriteErr(w, http.StatusBadRequest, "user and password are required")
		return
	}
	userKey := loginLimitKeyUser(ident)
	if h.limiter.blocked(userKey) {
		httpx.WriteErr(w, http.StatusTooManyRequests, "too many login attempts")
		return
	}
	user, err := lookupLoginUser(r.Context(), h.users, ident)
	if err != nil {
		// Spend a bcrypt comparison so a missing user is indistinguishable
		// from a wrong password by response time.
		CheckPassword(dummyPasswordHash(), req.Password)
		h.limiter.fail(ipKey, userKey)
		httpx.WriteErr(w, http.StatusUnauthorized, "invalid user or password")
		return
	}
	hash := user.PasswordHash
	if hash == "" {
		// OAuth-only account with no local password: compare against the dummy
		// hash so timing and the response match a wrong-password attempt rather
		// than revealing that the account exists.
		hash = dummyPasswordHash()
	}
	if !CheckPassword(hash, req.Password) {
		h.limiter.fail(ipKey, userKey)
		httpx.WriteErr(w, http.StatusUnauthorized, "invalid user or password")
		return
	}
	h.limiter.clear(ipKey, userKey)
	if err := h.issueSession(w, r, user); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	masked := *user
	masked.APIKey = maskAPIKey(masked.APIKey)
	httpx.WriteJSON(w, http.StatusOK, map[string]any{"user": masked})
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
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var req changePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.WriteErr(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.NewPassword) < minPasswordLen {
		httpx.WriteErr(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	fresh, err := h.users.GetByUsername(r.Context(), user.Username)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if fresh.PasswordHash != "" && !CheckPassword(fresh.PasswordHash, req.CurrentPassword) {
		httpx.WriteErr(w, http.StatusUnauthorized, "current password is incorrect")
		return
	}
	hash, err := HashPassword(req.NewPassword)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	fresh.PasswordHash = hash
	if err := h.users.Upsert(r.Context(), *fresh); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if h.sessions != nil {
		_ = h.sessions.DeleteByUser(r.Context(), fresh.Username)
	}
	if err := h.issueSession(w, r, fresh); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *SessionHandler) RotateAPIKey(w http.ResponseWriter, r *http.Request) {
	user := GetUser(r.Context())
	if user == nil {
		httpx.WriteErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	fresh, err := h.users.GetByUsername(r.Context(), user.Username)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	id, err := NewID()
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	fresh.APIKey = APIKeyPrefix + id
	if err := h.users.Upsert(r.Context(), *fresh); err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "internal server error")
		return
	}
	httpx.WriteJSON(w, http.StatusOK, map[string]string{
		"username": fresh.Username,
		"apiKey":   fresh.APIKey,
	})
}

func (h *SessionHandler) issueSession(w http.ResponseWriter, r *http.Request, user *storage.User) error {
	return IssueSession(w, r, h.sessions, user)
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
	return SanitizeUsername(local)
}

// SanitizeUsername maps an arbitrary string (a remote login, an email local
// part) onto the charset local usernames allow.
func SanitizeUsername(raw string) string {
	s := nonUsername.ReplaceAllString(strings.TrimSpace(raw), "_")
	s = strings.Trim(s, "._-")
	if len(s) > 64 {
		s = strings.Trim(s[:64], "._-")
	}
	if s == "" || !validUsername.MatchString(s) {
		s = "user"
	}
	return s
}

// UniqueUsername returns base, or base-2, base-3, … when already taken.
func UniqueUsername(ctx context.Context, users storage.UserStore, base string) (string, error) {
	return uniqueUsername(ctx, users, base)
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

// loginLimitKeyIP namespaces the per-source-IP bucket.
func loginLimitKeyIP(ip string) string { return "ip:" + ip }

// loginLimitKeyUser namespaces the per-account bucket, so an attacker rotating
// IPs cannot brute-force a single username without tripping this dimension.
func loginLimitKeyUser(ident string) string {
	return "user:" + strings.ToLower(strings.TrimSpace(ident))
}

// blocked reports whether any of the given buckets has hit the limit.
func (l *loginLimiter) blocked(keys ...string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		l.pruneLocked(k)
		if len(l.fails[k]) >= loginFailLimit {
			return true
		}
	}
	return false
}

// fail records an attempt against every bucket at once.
func (l *loginLimiter) fail(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for _, k := range keys {
		l.pruneLocked(k)
		l.fails[k] = append(l.fails[k], now)
	}
}

// clear drops every bucket (called on a successful login).
func (l *loginLimiter) clear(keys ...string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, k := range keys {
		delete(l.fails, k)
	}
}

func (l *loginLimiter) pruneLocked(key string) {
	cutoff := time.Now().Add(-loginFailWindow)
	times := l.fails[key]
	i := 0
	for _, t := range times {
		if t.After(cutoff) {
			times[i] = t
			i++
		}
	}
	if i == 0 {
		delete(l.fails, key)
		return
	}
	l.fails[key] = times[:i]
}
