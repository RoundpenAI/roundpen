package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"
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
	if r.TLS != nil {
		return true
	}
	return r.Header.Get("X-Forwarded-Proto") == "https"
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
