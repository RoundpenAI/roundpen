package userenv

import (
	"context"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

func TestEnsureAgentUsesImageOverride(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{}
	svc := &Service{Store: &memSlots{}, Sandboxes: boxes}
	svc.SetAgentImage("ghcr.io/roundpenai/code-agent:manual")

	if _, err := svc.EnsureAgent(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if got := boxes.lastCreate.Image; got != "ghcr.io/roundpenai/code-agent:manual" {
		t.Fatalf("image = %q, want the override", got)
	}
	if got := boxes.lastCreate.TemplateID; got != "code-agent" {
		t.Fatalf("template = %q, want the template to keep supplying resources", got)
	}

	// The override is not a qcow2 disk, so the second ensure adopts the
	// existing sandbox instead of treating it as the wrong engine.
	if _, err := svc.EnsureAgent(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if boxes.creates != 1 {
		t.Fatalf("creates = %d, want the sandbox reused", boxes.creates)
	}
}

func TestEnsureAgentKeepsTemplateImageWithoutOverride(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{}
	svc := &Service{Store: &memSlots{}, Sandboxes: boxes}

	if _, err := svc.EnsureAgent(ctx, "alice"); err != nil {
		t.Fatal(err)
	}
	if got := boxes.lastCreate.Image; got != "" {
		t.Fatalf("image = %q, want the template's own image", got)
	}
}

func TestUpgradeAgentRefreshesOverrideImage(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{refreshChanged: true, refreshDigest: "sha256:new"}
	boxes.put(&sandbox.Sandbox{ID: "agent-1", Name: "agent-alice", Status: sandbox.StatusRunning})
	svc, slots := newUpgradeService(boxes)
	svc.SetAgentImage("ghcr.io/roundpenai/code-agent:manual")
	if err := slots.Upsert(ctx, "alice", SlotAgent, "agent-1", "code-agent"); err != nil {
		t.Fatal(err)
	}

	res, err := svc.UpgradeAgent(ctx, "alice", false)
	if err != nil {
		t.Fatal(err)
	}
	if boxes.refreshRef != "ghcr.io/roundpenai/code-agent:manual" {
		t.Fatalf("refreshed %q, want the override", boxes.refreshRef)
	}
	if res.Status != "upgraded" || res.Image != "ghcr.io/roundpenai/code-agent:manual" {
		t.Fatalf("res = %+v", res)
	}
	if got := boxes.lastCreate.Image; got != "ghcr.io/roundpenai/code-agent:manual" {
		t.Fatalf("rebuilt image = %q, want the override", got)
	}
}
