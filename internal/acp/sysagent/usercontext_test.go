package sysagent

import (
	"strings"
	"testing"
)

func TestFormatUserContextEmpty(t *testing.T) {
	if got := FormatUserContext("", "", "", "", ""); got != "" {
		t.Fatalf("empty profile should yield empty string, got %q", got)
	}
}

func TestFormatUserContextIncludesFields(t *testing.T) {
	got := FormatUserContext("alice", "Alice", "Acme", "Prefers concise answers.", "Asia/Shanghai")
	for _, want := range []string{
		"Preferred name: Alice",
		"Account: alice",
		"Organization: Acme",
		"Timezone: Asia/Shanghai",
		"Prefers concise answers.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
