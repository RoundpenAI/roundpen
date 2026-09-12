package multi

import (
	"context"
	"os"
	"testing"
	"time"
)

// Live check of archive-based workspace listing against a running sandbox
// container ("admin" for roundpen-admin). Needs a local Docker daemon:
//
//	ROUNDPEN_TEST_SANDBOX_ID=admin go test ./internal/backend/multi -run TestLiveListWorkspaceDir -v
func TestLiveListWorkspaceDir(t *testing.T) {
	id := os.Getenv("ROUNDPEN_TEST_SANDBOX_ID")
	if id == "" {
		t.Skip("set ROUNDPEN_TEST_SANDBOX_ID=<sandbox id> to run")
	}
	b := New(Options{DisableQEMU: true})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	entries, err := b.ListWorkspaceDir(ctx, id, ".")
	if err != nil {
		t.Fatalf("ListWorkspaceDir: %v", err)
	}
	t.Logf("entries: %+v", entries)
	found := false
	for _, e := range entries {
		if e.Name == ".roundpen" && e.IsDir {
			found = true
		}
	}
	if !found {
		t.Fatalf("want .roundpen dir in %+v", entries)
	}
	if _, err := b.ListWorkspaceDir(ctx, id, "nope-does-not-exist"); !os.IsNotExist(err) {
		t.Fatalf("missing path: want not-exist, got %v", err)
	}
}
