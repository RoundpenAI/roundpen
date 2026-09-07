package browsetask

import (
	"strings"
	"testing"
)

func TestPromptAndTitle(t *testing.T) {
	p := PromptFor(KindExplore, "http://127.0.0.1:19000/chats", "try the session ×")
	if !strings.Contains(p, "browser_explore") || !strings.Contains(p, "session ×") {
		t.Fatalf("explore prompt: %s", p)
	}
	v := PromptFor(KindVerify, "http://example.com/", "login form rejects blank password")
	if !strings.Contains(v, "criteria") || !strings.Contains(v, "blank password") {
		t.Fatalf("verify prompt: %s", v)
	}
	if TitleFor(KindExplore, "http://127.0.0.1:19000/chats") != "Explore 127.0.0.1:19000" {
		t.Fatalf("title=%q", TitleFor(KindExplore, "http://127.0.0.1:19000/chats"))
	}
	if _, err := NormalizeKind("VERIFY"); err != nil {
		t.Fatal(err)
	}
	if _, err := NormalizeKind("nope"); err == nil {
		t.Fatal("expected kind error")
	}
}
