package sandbox_test

import (
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

func TestNormalizeCategory(t *testing.T) {
	if got := sandbox.NormalizeCategory("  Browser  "); got != "Browser" {
		t.Fatalf("got %q", got)
	}
	if got := sandbox.NormalizeCategory(""); got != "" {
		t.Fatalf("empty got %q", got)
	}
	long := strings.Repeat("x", 40)
	if got := sandbox.NormalizeCategory(long); len([]rune(got)) != 32 {
		t.Fatalf("category length = %d, want 32", len([]rune(got)))
	}
	if got := sandbox.NormalizeCategory("Code\x00Lab"); got != "CodeLab" {
		t.Fatalf("control chars: got %q", got)
	}
}

func TestNormalizeName(t *testing.T) {
	if got := sandbox.NormalizeName("  my lab "); got != "my lab" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("n", 80)
	if got := sandbox.NormalizeName(long); len([]rune(got)) != 64 {
		t.Fatalf("name length = %d, want 64", len([]rune(got)))
	}
	if got := sandbox.NormalizeName("Demo\x7fBox"); got != "DemoBox" {
		t.Fatalf("control chars: got %q", got)
	}
}
