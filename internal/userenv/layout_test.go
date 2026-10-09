package userenv

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/workspace"
)

func TestEnsureAgentSeedsWorkspaceLayout(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	boxes := &fakeSandboxes{hostPath: root}
	svc := &Service{Store: &memSlots{}, Sandboxes: boxes}

	sb, err := svc.EnsureAgent(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	host := filepath.Join(root, sb.ID)
	for _, d := range workspace.CanonicalDirs {
		if fi, err := os.Stat(filepath.Join(host, d)); err != nil || !fi.IsDir() {
			t.Fatalf("%s missing after EnsureAgent", d)
		}
	}
	if _, err := os.Stat(filepath.Join(host, workspace.AgentsMDName)); err != nil {
		t.Fatal("AGENTS.md missing after EnsureAgent")
	}
}

func TestEnsureAgentSeedsLayoutBestEffort(t *testing.T) {
	// WorkspaceHostPath failing must not break EnsureAgent.
	ctx := context.Background()
	boxes := &fakeSandboxes{}
	svc := &Service{Store: &memSlots{}, Sandboxes: boxes}
	if _, err := svc.EnsureAgent(ctx, "bob"); err != nil {
		t.Fatal(err)
	}
}
