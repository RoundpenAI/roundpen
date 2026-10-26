package proactive

import (
	"strings"
	"testing"
)

func TestFormatTextSkipsStubs(t *testing.T) {
	d := Digest{Findings: []Finding{
		{Source: SourceGmail, Skipped: "not connected"},
		{Source: SourceIssues, Title: "ISS-1 · Ship login", Detail: "in_progress"},
		{Source: SourceGitHub, Title: "Fix CI", URL: "https://github.com/o/r/pull/1"},
	}}
	text := d.FormatText(8)
	if text == "" {
		t.Fatal("expected digest text")
	}
	if strings.Contains(text, "not connected") {
		t.Fatalf("stub leaked: %s", text)
	}
	if !strings.Contains(text, "ISS-1") || !strings.Contains(text, "Fix CI") {
		t.Fatalf("missing findings: %s", text)
	}
}

func TestFormatTextEmptyWhenOnlyStubs(t *testing.T) {
	d := Digest{Findings: []Finding{
		{Source: SourceGmail, Skipped: "x"},
		{Source: SourceOnThisDay, Skipped: "y"},
	}}
	if got := d.FormatText(8); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}

func TestDigestHashStable(t *testing.T) {
	a := []Finding{{Source: SourceIssues, Title: "A", URL: "/issues/ISS-1"}}
	b := []Finding{{Source: SourceIssues, Title: "A", URL: "/issues/ISS-1"}, {Source: SourceGmail, Skipped: "no"}}
	if DigestHash(a) != DigestHash(b) {
		t.Fatal("hash should ignore stubs")
	}
}
