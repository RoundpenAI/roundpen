package agentenv_test

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/agentenv"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/workspace"
)

func TestConfigDefaults(t *testing.T) {
	p := &agentenv.Provisioner{Config: agentenv.Config{}}
	if p.Config.PublicURL != "" {
		t.Fatal("expected empty")
	}
	env := map[string]string{
		"ROUNDPEN_URL":       "http://example",
		"OPENAI_BASE_URL":    "http://example/llmgw/openai",
		"ANTHROPIC_BASE_URL": "http://example/llmgw/anthropic",
	}
	for _, k := range []string{"ROUNDPEN_URL", "OPENAI_BASE_URL", "ANTHROPIC_BASE_URL"} {
		if !strings.Contains(env[k], "http") {
			t.Fatalf("%s missing", k)
		}
	}
}

func TestApplyDefaultModel(t *testing.T) {
	env := map[string]string{"ANTHROPIC_DEFAULT_SONNET_MODEL": "keep-me"}
	agentenv.ApplyDefaultModel(env, " nvidia/nemotron-3.5-lightning:free ")
	if env["ANTHROPIC_MODEL"] != "nvidia/nemotron-3.5-lightning:free" {
		t.Fatalf("model: %q", env["ANTHROPIC_MODEL"])
	}
	if env["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "keep-me" {
		t.Fatalf("preserved: %q", env["ANTHROPIC_DEFAULT_SONNET_MODEL"])
	}
	if env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "nvidia/nemotron-3.5-lightning:free" {
		t.Fatalf("opus: %q", env["ANTHROPIC_DEFAULT_OPUS_MODEL"])
	}
	agentenv.ApplyDefaultModel(env, "")
	if env["ANTHROPIC_MODEL"] != "nvidia/nemotron-3.5-lightning:free" {
		t.Fatal("empty model should not clear")
	}
}

type stubSandboxes struct {
	sandbox.Manager
	got sandbox.CreateRequest
}

func (s *stubSandboxes) Create(_ context.Context, req sandbox.CreateRequest) (*sandbox.Sandbox, error) {
	s.got = req
	return &sandbox.Sandbox{ID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", WorkspaceID: req.WorkspaceID}, nil
}

func (s *stubSandboxes) WriteFile(context.Context, string, string, io.Reader) error {
	return nil
}

func TestProvisionUsesUserWorkspace(t *testing.T) {
	stub := &stubSandboxes{}
	p := &agentenv.Provisioner{Sandboxes: stub, Config: agentenv.Config{PublicURL: "http://127.0.0.1:19001"}}
	res, err := p.Provision(context.Background(), "sess-1", "claude", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	want := workspace.UserWorkspaceID("Admin")
	if want != "user-admin" {
		t.Fatalf("id helper: %q", want)
	}
	if stub.got.WorkspaceID != want {
		t.Fatalf("WorkspaceID: %q", stub.got.WorkspaceID)
	}
	if res.Sandbox.WorkspaceID != want {
		t.Fatalf("result: %q", res.Sandbox.WorkspaceID)
	}
}
