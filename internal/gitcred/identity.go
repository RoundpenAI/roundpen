package gitcred

import (
	"strings"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

// Identity is the commit author (and optional GPG signing) injected into a guest.
type Identity struct {
	Name          string
	Email         string
	GPGPrivateKey string // ASCII-armored secret key; empty = leave guest unsigned
	GPGKeyID      string // fingerprint or key id; empty = first secret key after import
	SignCommits   bool
}

// Empty reports whether there is nothing to write for authorship or signing.
func (id Identity) Empty() bool {
	return strings.TrimSpace(id.Name) == "" &&
		strings.TrimSpace(id.Email) == "" &&
		strings.TrimSpace(id.GPGPrivateKey) == ""
}

// IdentityFromUser maps profile fields onto a guest git identity.
// gitName/gitEmail win; otherwise fullname/email, then username for the name.
func IdentityFromUser(u *storage.User) Identity {
	if u == nil {
		return Identity{}
	}
	name := strings.TrimSpace(u.GitName)
	if name == "" {
		name = strings.TrimSpace(u.FullName)
	}
	if name == "" {
		name = strings.TrimSpace(u.Username)
	}
	email := strings.TrimSpace(u.GitEmail)
	if email == "" {
		email = strings.TrimSpace(u.Email)
	}
	key := strings.TrimSpace(u.GPGPrivateKey)
	return Identity{
		Name:          name,
		Email:         email,
		GPGPrivateKey: key,
		GPGKeyID:      strings.TrimSpace(u.GPGKeyID),
		SignCommits:   u.GPGSign && key != "",
	}
}
