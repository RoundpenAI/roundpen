package preview

import (
	"testing"
	"time"
)

func TestTokenIssueAndLookup(t *testing.T) {
	s := NewStore(time.Minute)
	tok, exp, err := s.Issue("sb-1", 3000)
	if err != nil || tok == "" {
		t.Fatalf("issue: %v %q", err, tok)
	}
	if exp.Before(time.Now()) {
		t.Fatal("expires in the past")
	}
	sid, port, ok := s.Lookup(tok)
	if !ok || sid != "sb-1" || port != 3000 {
		t.Fatalf("lookup: ok=%v sid=%s port=%d", ok, sid, port)
	}
	if _, _, ok := s.Lookup("nope"); ok {
		t.Fatal("expected miss")
	}
}

func TestTokenExpiry(t *testing.T) {
	s := NewStore(10 * time.Millisecond)
	tok, _, err := s.Issue("sb-1", 80)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, _, ok := s.Lookup(tok); ok {
		t.Fatal("expected expired")
	}
}
