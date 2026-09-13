package userenv

import (
	"context"
	"errors"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/config"
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

func TestEnsureBrowserExternalProvider(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{}
	svc := &Service{Store: &memSlots{}, Sandboxes: boxes}
	svc.Cfg = &config.Config{CDP: config.CDPConfig{Provider: config.CDPProviderRemote, Endpoint: "ws://10.10.1.3:3000/chrome"}}

	target, err := svc.EnsureBrowser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if target.Managed || target.Sandbox != nil {
		t.Fatalf("remote provider must not create a sandbox: %+v", target)
	}
	if target.Key != "browser-alice" {
		t.Fatalf("key = %q, want browser-alice", target.Key)
	}
	if target.Provider != config.CDPProviderRemote {
		t.Fatalf("provider = %q", target.Provider)
	}
	if boxes.creates != 0 {
		t.Fatalf("creates=%d", boxes.creates)
	}

	svc.Cfg = &config.Config{CDP: config.CDPConfig{Provider: config.CDPProviderRemote}}
	if _, err := svc.EnsureBrowser(ctx, "alice"); err == nil {
		t.Fatal("remote provider without an endpoint must fail")
	}
}

func TestEnsureBrowserManagedProvider(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{}
	svc := &Service{Store: &memSlots{}, Sandboxes: boxes}
	svc.Cfg = &config.Config{CDP: config.CDPConfig{Provider: config.CDPProviderDocker}}

	target, err := svc.EnsureBrowser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !target.Managed || target.Sandbox == nil {
		t.Fatalf("docker provider must create a sandbox: %+v", target)
	}
	if target.Key != target.Sandbox.ID {
		t.Fatalf("key = %q, want %q", target.Key, target.Sandbox.ID)
	}
	if target.Provider != config.CDPProviderDocker {
		t.Fatalf("provider = %q", target.Provider)
	}
}

func TestEnsureBrowserCreates(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{}
	svc := &Service{Store: &memSlots{}, Sandboxes: boxes}

	target, err := svc.EnsureBrowser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if target.Sandbox == nil || target.Sandbox.Name != "browser-alice" || target.Sandbox.Status != sandbox.StatusRunning {
		t.Fatalf("got %+v", target)
	}
	if boxes.creates != 1 {
		t.Fatalf("creates=%d", boxes.creates)
	}

	again, err := svc.EnsureBrowser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if again.Sandbox == nil || again.Sandbox.ID != target.Sandbox.ID || boxes.creates != 1 {
		t.Fatalf("second ensure created a new sandbox: %+v vs %+v creates=%d", again, target, boxes.creates)
	}
}

func TestEnsureBrowserResumesUnmappedName(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{byID: map[string]*sandbox.Sandbox{
		"old": {ID: "old", Name: "browser-alice", Status: sandbox.StatusStopped},
	}}
	svc := &Service{Store: &memSlots{}, Sandboxes: boxes}

	target, err := svc.EnsureBrowser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if target.Sandbox == nil || target.Sandbox.ID != "old" {
		t.Fatalf("should reuse existing name, got %+v", target)
	}
	if target.Sandbox.Status != sandbox.StatusRunning {
		t.Fatalf("status %s", target.Sandbox.Status)
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

	target, err := svc.EnsureBrowser(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if target.Sandbox == nil || target.Sandbox.ID == "dead" {
		t.Fatalf("should replace failed sandbox: %+v", target)
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

func TestListBrowserExternalIgnoresStaleMapping(t *testing.T) {
	ctx := context.Background()
	boxes := &fakeSandboxes{byID: map[string]*sandbox.Sandbox{
		"stale": {ID: "stale", Name: "browser-alice", Status: sandbox.StatusRunning},
	}}
	svc := &Service{Store: &memSlots{}, Sandboxes: boxes}
	svc.Cfg = &config.Config{CDP: config.CDPConfig{Provider: config.CDPProviderRemote, Endpoint: "ws://10.10.1.3:3000/chrome"}}
	if err := svc.Store.Upsert(ctx, "alice", SlotBrowser, "stale", "browser"); err != nil {
		t.Fatal(err)
	}

	browser := browserView(t, svc, "alice")
	if browser.Status != "external" {
		t.Fatalf("status = %q, want external: %+v", browser.Status, browser)
	}
	if browser.SandboxID != "" {
		t.Fatalf("stale container must not be reported: %+v", browser)
	}
	if browser.Provider != config.CDPProviderRemote {
		t.Fatalf("provider = %q", browser.Provider)
	}
}

func TestListBrowserDockerNoMappingAbsent(t *testing.T) {
	svc := &Service{Store: &memSlots{}, Sandboxes: &fakeSandboxes{}}
	svc.Cfg = &config.Config{CDP: config.CDPConfig{Provider: config.CDPProviderDocker}}

	browser := browserView(t, svc, "alice")
	if browser.Status != "absent" {
		t.Fatalf("status = %q, want absent: %+v", browser.Status, browser)
	}
	if browser.Provider != config.CDPProviderDocker {
		t.Fatalf("provider = %q", browser.Provider)
	}
}

func browserView(t *testing.T, svc *Service, userID string) EnvView {
	t.Helper()
	list, err := svc.List(context.Background(), userID)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range list {
		if v.Slot == SlotBrowser {
			return v
		}
	}
	t.Fatal("browser slot missing from list")
	return EnvView{}
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
