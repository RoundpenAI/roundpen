package gitcred

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

func TestIdentityFromUserFallbacks(t *testing.T) {
	id := IdentityFromUser(&storage.User{
		Username: "alice", Email: "a@b.com", FullName: "Alice",
	})
	if id.Name != "Alice" || id.Email != "a@b.com" {
		t.Fatalf("got %+v", id)
	}
	id = IdentityFromUser(&storage.User{
		Username: "bob", Email: "b@c.com",
		GitName: "Bob Git", GitEmail: "git@c.com",
		GPGPrivateKey: "KEY", GPGSign: true,
	})
	if id.Name != "Bob Git" || id.Email != "git@c.com" || !id.SignCommits {
		t.Fatalf("got %+v", id)
	}
	id = IdentityFromUser(&storage.User{Username: "carol", GPGSign: true})
	if id.Name != "carol" || id.SignCommits {
		t.Fatalf("sign without key must be false: %+v", id)
	}
}
