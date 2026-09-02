package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/bcrypt"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

const (
	bcryptCost     = 12
	minPasswordLen = 8
)

var errPasswordTooShort = fmt.Errorf("password must be at least %d characters", minPasswordLen)

// HashPassword returns a bcrypt hash of plain (cost 12).
func HashPassword(plain string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(plain), bcryptCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// CheckPassword reports whether plain matches a bcrypt hash.
func CheckPassword(hash, plain string) bool {
	if hash == "" || plain == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// assignLoginPassword hashes requested (or a generated password) onto user.
// Empty requested generates a 16-char password. Plaintext is returned once.
func assignLoginPassword(user *storage.User, requested string) (plain string, err error) {
	if requested != "" {
		if len(requested) < minPasswordLen {
			return "", errPasswordTooShort
		}
		plain = requested
	} else {
		plain, err = randomPassword(16)
		if err != nil {
			return "", err
		}
	}
	hash, err := HashPassword(plain)
	if err != nil {
		return "", err
	}
	user.PasswordHash = hash
	if user.AuthProvider == "" {
		user.AuthProvider = "local"
	}
	return plain, nil
}

func randomPassword(n int) (string, error) {
	const alphabet = "abcdefghijkmnopqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}

// NewID generates a random hex identifier (32 chars).
func NewID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
