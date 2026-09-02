package auth

import "testing"

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("secret123")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "secret123") {
		t.Fatal("expected match")
	}
	if CheckPassword(hash, "wrong") {
		t.Fatal("expected mismatch")
	}
	if CheckPassword("", "secret123") {
		t.Fatal("empty hash must not match")
	}
}
