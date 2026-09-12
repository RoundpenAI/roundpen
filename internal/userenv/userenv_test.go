package userenv

import (
	"context"
	"errors"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/workspace"
)

func TestSanitizeUser(t *testing.T) {
	if got := sanitizeUser("Alice_Bob!"); got != "alice-bob-" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeUser(""); got != "user" {
		t.Fatalf("empty: %q", got)
	}
}

func TestGatewayEnv(t *testing.T) {
	s := &Service{}
	if s.gatewayEnv() != nil {
		t.Fatal("empty public URL should skip injection")
	}
	s.SetGateway("http://127.0.0.1:9527/", "vk-test")
	env := s.gatewayEnv()
	if env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:9527/llmgw/anthropic" {
		t.Fatalf("anthropic: %s", env["ANTHROPIC_BASE_URL"])
	}
	if env["OPENAI_BASE_URL"] != "http://127.0.0.1:9527/llmgw/openai" {
		t.Fatalf("openai: %s", env["OPENAI_BASE_URL"])
	}
	if env["ANTHROPIC_API_KEY"] != "vk-test" || env["ANTHROPIC_AUTH_TOKEN"] != "vk-test" {
		t.Fatalf("key: %+v", env)
	}
	if env["ANTHROPIC_MODEL"] != "" {
		t.Fatalf("unexpected model: %q", env["ANTHROPIC_MODEL"])
	}
	s.Config.DefaultModel = func() string { return "nvidia/nemotron-3.5-lightning:free" }
	env = s.gatewayEnv()
	if env["ANTHROPIC_MODEL"] != "nvidia/nemotron-3.5-lightning:free" {
		t.Fatalf("default model: %q", env["ANTHROPIC_MODEL"])
	}
}

func TestEnsureBrowserCreates(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{}
	svc := &Service{Store: &memSlots{}, Sandboxes: boxes}

	sb, err := svc.EnsureBrowser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if sb.Name != "browser-alice" || sb.Status != sandbox.StatusRunning {
		t.Fatalf("got %+v", sb)
	}
	if boxes.creates != 1 {
		t.Fatalf("creates=%d", boxes.creates)
	}

	again, err := svc.EnsureBrowser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != sb.ID || boxes.creates != 1 {
		t.Fatalf("second ensure created a new sandbox: %s vs %s creates=%d", again.ID, sb.ID, boxes.creates)
	}
}

func TestEnsureBrowserResumesUnmappedName(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{byID: map[string]*sandbox.Sandbox{
		"old": {ID: "old", Name: "browser-alice", Status: sandbox.StatusStopped},
	}}
	svc := &Service{Store: &memSlots{}, Sandboxes: boxes}

	sb, err := svc.EnsureBrowser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if sb.ID != "old" {
		t.Fatalf("should reuse existing name, got %s", sb.ID)
	}
	if sb.Status != sandbox.StatusRunning {
		t.Fatalf("status %s", sb.Status)
	}
	if boxes.creates != 0 {
		t.Fatalf("should not create, creates=%d", boxes.creates)
	}
	mapped, err := svc.Store.Get(ctx, "alice", SlotBrowser)
	if err != nil || mapped == nil || mapped.SandboxID != "old" {
		t.Fatalf("mapping: %+v err=%v", mapped, err)
	}
}

func TestEnsureBrowserReplacesFailed(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{byID: map[string]*sandbox.Sandbox{
		"dead": {ID: "dead", Name: "browser-alice", Status: sandbox.StatusFailed},
	}}
	svc := &Service{Store: &memSlots{}, Sandboxes: boxes}

	sb, err := svc.EnsureBrowser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if sb.ID == "dead" {
		t.Fatal("should replace failed sandbox")
	}
	if boxes.creates != 1 {
		t.Fatalf("creates=%d", boxes.creates)
	}
	if _, err := boxes.Get(ctx, "dead"); !errors.Is(err, sandbox.ErrNotFound) {
		t.Fatalf("failed sandbox should be deleted: %v", err)
	}
}

func TestEnsureBrowserSurfacesConnectError(t *testing.T) {
	ctx := context.Background()
	boom := errors.New("qemu start failed")
	boxes := &fakeSandboxes{
		byID:       map[string]*sandbox.Sandbox{"old": {ID: "old", Name: "browser-alice", Status: sandbox.StatusStopped}},
		connectErr: boom,
	}
	svc := &Service{Store: &memSlots{}, Sandboxes: boxes}

	_, err := svc.EnsureBrowser(ctx, "alice")
	if !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
	if boxes.creates != 0 {
		t.Fatalf("must not recreate on connect failure, creates=%d", boxes.creates)
	}
}

func TestListDiscoversUnmappedBrowser(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{byID: map[string]*sandbox.Sandbox{
		"old": {
			ID: "old", Name: "browser-alice", Status: sandbox.StatusStopped,
			Metadata: map[string]string{"templateID": "browser-desktop"},
		},
	}}
	svc := &Service{Store: &memSlots{}, Sandboxes: boxes}

	list, err := svc.List(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	var browser EnvView
	for _, v := range list {
		if v.Slot == SlotBrowser {
			browser = v
		}
	}
	if browser.Status != string(sandbox.StatusStopped) || browser.SandboxID != "old" {
		t.Fatalf("browser view: %+v", browser)
	}
}

func TestEnsureAgentRecreatesNonDockerImage(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{byID: map[string]*sandbox.Sandbox{
		"old-qemu": {
			ID:     "old-qemu",
			Name:   "agent-alice",
			Status: sandbox.StatusRunning,
			Image:  "images/agent-qemu/out/agent.qcow2",
		},
	}}
	svc := &Service{Store: &memSlots{}, Sandboxes: boxes}
	if err := svc.Store.Upsert(ctx, "alice", SlotAgent, "old-qemu", "agent-claude"); err != nil {
		t.Fatal(err)
	}

	sb, err := svc.EnsureAgent(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if sb.ID == "old-qemu" {
		t.Fatal("should replace qemu agent sandbox with Docker")
	}
	if sb.ID != workspace.AgentSandboxID("alice") {
		t.Fatalf("stable id: got %q", sb.ID)
	}
	if boxes.creates != 1 {
		t.Fatalf("creates=%d", boxes.creates)
	}
	if _, err := boxes.Get(ctx, "old-qemu"); !errors.Is(err, sandbox.ErrNotFound) {
		t.Fatalf("qemu sandbox should be deleted: %v", err)
	}
}
