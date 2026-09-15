package automode

import (
	"strings"
	"testing"
)

func TestRecentDigestKeepsUserAndAssistantOnly(t *testing.T) {
	msgs := []DigestMessage{
		{Role: "user", Content: "first ask"},
		{Role: "tool", Content: "SECRET TOOL OUTPUT"},
		{Role: "assistant", Content: "working on it"},
		{Role: "permission", Content: "permission row"},
		{Role: "user", Content: "force push this branch"},
	}
	got := RecentDigest(msgs, 4000)
	for _, want := range []string{"user: first ask", "assistant: working on it", "user: force push this branch"} {
		if !strings.Contains(got, want) {
			t.Fatalf("digest missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "SECRET TOOL OUTPUT") || strings.Contains(got, "permission row") {
		t.Fatalf("non-conversation rows leaked into digest:\n%s", got)
	}
	if strings.Index(got, "first ask") > strings.Index(got, "force push this branch") {
		t.Fatalf("digest not chronological:\n%s", got)
	}
}

func TestRecentDigestBudgetsFromTheEnd(t *testing.T) {
	long := strings.Repeat("x", 300)
	msgs := []DigestMessage{
		{Role: "user", Content: "oldest " + long},
		{Role: "assistant", Content: "middle " + long},
		{Role: "user", Content: "newest " + long},
	}
	got := RecentDigest(msgs, 500)
	if !strings.Contains(got, "newest") {
		t.Fatalf("most recent message dropped:\n%s", got)
	}
	if strings.Contains(got, "oldest") {
		t.Fatalf("budget should drop oldest first:\n%s", got)
	}
	if n := len([]rune(got)); n > 500 {
		t.Fatalf("digest exceeds budget: %d runes", n)
	}
}

func TestRecentDigestEmpty(t *testing.T) {
	if got := RecentDigest(nil, 100); got != "" {
		t.Fatalf("expected empty digest, got %q", got)
	}
}
