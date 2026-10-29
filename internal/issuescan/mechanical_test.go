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

func (m *memIssues) TransitionWith(_ context.Context, _, keyOrID string, req issue.TransitionRequest) (*issue.Issue, error) {
	for i := range m.rows {
		if m.rows[i].Key == keyOrID {
			m.rows[i].Status = req.To
			m.rows[i].UpdatedAt = time.Now().UTC()
			if req.To == issue.StatusTriaged {
				m.rows[i].ClaimOwner = ""
				m.rows[i].ClaimedAt = nil
			}
			cp := m.rows[i]
			return &cp, nil
		}
	}
	return nil, issue.ErrNotFound
}

func (m *memIssues) MarkBoardScanned(_ context.Context, _ string, keys []string, at time.Time) error {
	set := map[string]struct{}{}
	for _, k := range keys {
		set[k] = struct{}{}
	}
	for i := range m.rows {
		if _, ok := set[m.rows[i].Key]; ok {
			t := at
			m.rows[i].LastBoardScanAt = &t
		}
	}
	return nil
}

func TestRequeueStaleClaims(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	past := now.Add(-3 * time.Hour)
	fresh := now.Add(-30 * time.Minute)
	store := &memIssues{rows: []issue.Issue{
		{Key: "ISS-1", Status: issue.StatusClaimed, ClaimedAt: &past, Title: "stale"},
		{Key: "ISS-2", Status: issue.StatusClaimed, ClaimedAt: &fresh, Title: "fresh"},
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

func TestBoardDigestSkipsCooldown(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-time.Hour)
	store := &memIssues{rows: []issue.Issue{
		{Key: "ISS-1", Status: issue.StatusTriaged, Title: "hot", UpdatedAt: now, LastBoardScanAt: &recent},
		{Key: "ISS-2", Status: issue.StatusTriaged, Title: "cold", UpdatedAt: now},
	}}
	text, _, mentioned, err := BoardDigest(context.Background(), store, "u", now, 4, 12*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if text == "" || len(mentioned) != 1 || mentioned[0] != "ISS-2" {
		t.Fatalf("text=%q mentioned=%v", text, mentioned)
	}
}
