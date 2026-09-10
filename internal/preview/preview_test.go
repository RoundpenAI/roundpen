package preview

import (
	"testing"
	"time"
)

func TestTokenIssueAndLookup(t *testing.T) {
	s := NewStore(time.Minute)
	tok, exp, err := s.Issue("sb-1", 3000, "alice")
	if err != nil || tok == "" {
		t.Fatalf("issue: %v %q", err, tok)
	}
	if exp.Before(time.Now()) {
		t.Fatal("expires in the past")
	}
	sid, port, owner, ok := s.Lookup(tok)
	if !ok || sid != "sb-1" || port != 3000 || owner != "alice" {
		t.Fatalf("lookup: ok=%v sid=%s port=%d owner=%s", ok, sid, port, owner)
	}
	if _, _, _, ok := s.Lookup("nope"); ok {
		t.Fatal("expected miss")
	}
}

func TestTokenExpiry(t *testing.T) {
	s := NewStore(10 * time.Millisecond)
	tok, _, err := s.Issue("sb-1", 80, "alice")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, _, _, ok := s.Lookup(tok); ok {
		t.Fatal("expected expired")
	}
}
