package project

import (
	"strings"
	"testing"
)

func TestExtractReviewResultFenced(t *testing.T) {
	text := "Here is my review.\n\n```json\n" +
		`{"verdict":"request_changes","summary":"needs work","findings":[{"severity":"error","path":"a.go","line":12,"note":"nil deref"}]}` +
		"\n```\n"
	r := ExtractReviewResult(text)
	if r == nil || r.Verdict != VerdictRequestChanges {
		t.Fatalf("got %+v", r)
	}
	if len(r.Findings) != 1 || r.Findings[0].Path != "a.go" || r.Findings[0].Line != 12 {
		t.Fatalf("findings = %+v", r.Findings)
	}
}

func TestExtractReviewResultApproveAliases(t *testing.T) {
	r := ExtractReviewResult(`{"verdict":"lgtm","summary":"ok","findings":[]}`)
	if r == nil || r.Verdict != VerdictApprove {
		t.Fatalf("got %+v", r)
	}
}

func TestExtractReviewResultIgnoresNoise(t *testing.T) {
	if ExtractReviewResult("no json here", "still nothing") != nil {
		t.Fatal("expected nil")
	}
}

func TestFormatChangesBrief(t *testing.T) {
	brief := FormatChangesBrief(&ReviewResult{
		Summary: "tighten null checks",
		Findings: []ReviewFinding{
			{Severity: "error", Path: "a.go", Line: 3, Note: "nil deref"},
		},
	}, "Add login")
	for _, want := range []string{"Add login", "tighten null checks", "a.go:3", "nil deref"} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief missing %q: %q", want, brief)
		}
	}
}
