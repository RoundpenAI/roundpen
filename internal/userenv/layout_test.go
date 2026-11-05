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

func TestEnsureAgentRecreatesOnStaleWorkspaceMount(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{staleExecLeft: 1}
	svc := &Service{Store: &memSlots{}, Sandboxes: boxes}

	sb, err := svc.EnsureAgent(ctx, "carol")
	if err != nil {
		t.Fatal(err)
	}
	if boxes.creates < 2 {
		t.Fatalf("want recreate after stale mount, creates=%d", boxes.creates)
	}
	if boxes.deletes < 1 {
		t.Fatalf("want delete of stale container, deletes=%d", boxes.deletes)
	}
	if sb == nil || sb.ID == "" {
		t.Fatal("missing sandbox")
	}
	// Probe on the fresh container must have succeeded (no leftover stale quota).
	if boxes.staleExecLeft != 0 {
		t.Fatalf("staleExecLeft=%d", boxes.staleExecLeft)
	}
}
