package issuescan

import (
	"context"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/issue"
)

type memIssues struct {
	rows []issue.Issue
}

func (m *memIssues) List(_ context.Context, _ string, f issue.ListFilter) ([]issue.Issue, error) {
	var out []issue.Issue
	for _, it := range m.rows {
		if f.Status != "" && it.Status != f.Status {
			continue
		}
		out = append(out, it)
	}
	return out, nil
}

func (m *memIssues) Transition(_ context.Context, _, keyOrID, to string) (*issue.Issue, error) {
	for i := range m.rows {
		if m.rows[i].Key == keyOrID {
			m.rows[i].Status = to
			m.rows[i].UpdatedAt = time.Now().UTC()
			cp := m.rows[i]
			return &cp, nil
		}
	}
	return nil, issue.ErrNotFound
}

func TestRequeueStaleClaims(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	store := &memIssues{rows: []issue.Issue{
		{Key: "ISS-1", Status: issue.StatusClaimed, UpdatedAt: now.Add(-3 * time.Hour), Title: "stale"},
		{Key: "ISS-2", Status: issue.StatusClaimed, UpdatedAt: now.Add(-30 * time.Minute), Title: "fresh"},
		{Key: "ISS-3", Status: issue.StatusRunning, UpdatedAt: now.Add(-5 * time.Hour), Title: "running"},
	}}
	actions, err := RequeueStaleClaims(context.Background(), store, "u", now, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != 1 || !actions[0].Did || actions[0].Key != "ISS-1" {
		t.Fatalf("actions=%+v", actions)
	}
	if store.rows[0].Status != issue.StatusTriaged {
		t.Fatalf("want triaged, got %s", store.rows[0].Status)
	}
}

func TestBoardDigestEmpty(t *testing.T) {
	text, hash, err := BoardDigest(context.Background(), &memIssues{}, "u", 4)
	if err != nil || text != "" || hash != "" {
		t.Fatalf("empty board: %q %q %v", text, hash, err)
	}
}
