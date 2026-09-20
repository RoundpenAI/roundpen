package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

const (
	SessionCookieName = "roundpen_session"
	SessionTTL        = 7 * 24 * time.Hour
	sessionTokenBytes = 32
)

// NewSessionToken returns a random opaque token for the Cookie and its SHA-256 hex for storage.
func NewSessionToken() (plaintext string, hash string, err error) {
	var b [sessionTokenBytes]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", "", err
	}
	plaintext = hex.EncodeToString(b[:])
	return plaintext, HashSessionToken(plaintext), nil
}

// HashSessionToken SHA-256-hex encodes a plaintext session token.
func HashSessionToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

func sessionCookieSecure(r *http.Request) bool {
	return httpx.DefaultTrust.Scheme(r) == "https"
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, plaintext string, maxAge int) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    plaintext,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   sessionCookieSecure(r),
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	setSessionCookie(w, r, "", -1)
}

// IssueSession stores a fresh session for user and sets the session cookie.
// Exported so federated-login callbacks can sign a user in through the exact
// same path as the password login.
func IssueSession(w http.ResponseWriter, r *http.Request, sessions storage.SessionStore, user *storage.User) error {
	if sessions == nil {
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
	if err := sessions.Create(r.Context(), storage.Session{
		ID:         id,
		UserID:     user.Username,
		TokenHash:  hash,
		ExpiresAt:  now.Add(SessionTTL),
		CreatedAt:  now,
		LastSeenAt: now,
		UserAgent:  r.UserAgent(),
		IP:         clientIP(r),
	}); err != nil {
		return err
	}
	setSessionCookie(w, r, plain, int(SessionTTL.Seconds()))
	return nil
}
