package tools

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("short", 100); got != "short" {
		t.Fatalf("short string must be returned unchanged: %q", got)
	}
	if got := truncateRunes(strings.Repeat("x", 50), 10); got != strings.Repeat("x", 10)+"…" {
		t.Fatalf("got %q", got)
	}
}

func TestTruncateRunesCJK(t *testing.T) {
	got := truncateRunes(strings.Repeat("中", 100), 10)
	if !utf8.ValidString(got) {
		t.Fatalf("output must be valid UTF-8: %q", got)
	}
	if len(got) > 12 {
		t.Fatalf("output too long: %d bytes", len(got))
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("missing ellipsis: %q", got)
	}
}

func TestTruncateRunesNonUTF8KeepsContent(t *testing.T) {
	s := "caf\xe9" + strings.Repeat("x", 150_000)
	got := truncateRunes(s, 100_000)
	if len(got) < 99_000 {
		t.Fatalf("invalid byte near the head must not collapse the output: %d bytes (%q)", len(got), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("missing ellipsis marker")
	}
}
