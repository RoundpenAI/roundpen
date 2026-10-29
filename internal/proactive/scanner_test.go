package proactive

import (
	"context"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/issue"
)

type stubIssues struct {
	rows []issue.Issue
}

func (s stubIssues) List(_ context.Context, _ string, f issue.ListFilter) ([]issue.Issue, error) {
	var out []issue.Issue
	for _, it := range s.rows {
		if f.Status == "" || it.Status == f.Status {
			out = append(out, it)
		}
	}
	return out, nil
}

type stubGmail struct {
	hits []GmailHit
	err  error
}

func (s stubGmail) SearchGmail(context.Context, string, string, int) ([]GmailHit, error) {
	return s.hits, s.err
}

func TestScanUserAggregatesSources(t *testing.T) {
	sc := &Scanner{
		Issues: stubIssues{rows: []issue.Issue{{
			Key: "ISS-9", Title: "Backlog", Status: issue.StatusRunning,
		}}},
		Gmail: stubGmail{hits: []GmailHit{{Subject: "Hello", From: "a@b.c"}}},
		GitHub: GitHubTokenFunc(func(context.Context, string) (string, error) {
			return "", nil // soft skip
		}),
		OnThisDay: nil,
	}
	d, err := sc.ScanUser(context.Background(), "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := Actionable(d.Findings)
	if len(got) < 2 {
		t.Fatalf("want issues+gmail, got %+v", d.Findings)
	}
	var sources []string
	for _, f := range got {
		sources = append(sources, f.Source)
	}
	if !contains(sources, SourceIssues) || !contains(sources, SourceGmail) {
		t.Fatalf("sources=%v findings=%+v", sources, got)
	}
	// OnThisDay extension point should leave a skip marker when unset.
	foundSkip := false
	for _, f := range d.Findings {
		if f.Source == SourceOnThisDay && f.Skipped != "" {
			foundSkip = true
		}
	}
	if !foundSkip {
		t.Fatalf("expected on_this_day stub, got %+v", d.Findings)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}
