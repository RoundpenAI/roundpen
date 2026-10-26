package proactive

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/assistant"
)

type stubGrants struct {
	grants []assistant.DirectoryGrant
}

func (s stubGrants) ListPhotoGrants(context.Context, string) ([]assistant.DirectoryGrant, error) {
	return s.grants, nil
}

func TestOnThisDayNameMatch(t *testing.T) {
	dir := t.TempDir()
	// Fixed "today" for the test clock.
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	name := "vacation-2019-10-26.jpg"
	if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	o := &OnThisDay{Grants: stubGrants{grants: []assistant.DirectoryGrant{{Path: dir, Mode: "read"}}}}
	hits, err := o.Scan(context.Background(), "alice", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Title != name {
		t.Fatalf("hits=%+v", hits)
	}
}

func TestOnThisDayNoGrants(t *testing.T) {
	o := &OnThisDay{Grants: stubGrants{}}
	hits, err := o.Scan(context.Background(), "alice", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Skipped == "" {
		t.Fatalf("want skip, got %+v", hits)
	}
}
