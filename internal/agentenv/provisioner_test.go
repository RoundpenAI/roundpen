package agentenv_test

import (
	"context"
	"encoding/base64"
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
	ex  sandbox.ExecRequest
	err error
}

func (s *stubSandboxes) Create(_ context.Context, req sandbox.CreateRequest) (*sandbox.Sandbox, error) {
	s.got = req
	return &sandbox.Sandbox{ID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", WorkspaceID: req.WorkspaceID}, nil
}

func (s *stubSandboxes) Exec(_ context.Context, _ string, req sandbox.ExecRequest) (*sandbox.ExecResult, error) {
	s.ex = req
	if s.err != nil {
		return nil, s.err
	}
	return &sandbox.ExecResult{ExitCode: 0}, nil
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

func TestProvisionWritesEnvToHomeNotWorkspace(t *testing.T) {
	stub := &stubSandboxes{}
	p := &agentenv.Provisioner{Sandboxes: stub, Config: agentenv.Config{PublicURL: "http://127.0.0.1:19001"}}
	if _, err := p.Provision(context.Background(), "sess-1", "claude", "Admin"); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(stub.ex.Cmd, " ")
	for _, want := range []string{"$HOME/.roundpen", `"$D/env"`, "base64 -d"} {
		if !strings.Contains(got, want) {
			t.Fatalf("exec %q missing %q", got, want)
		}
	}
	if strings.Contains(got, "workspace/.roundpen") {
		t.Fatalf("env file must not live under the workspace: %q", got)
	}
	// Payload decodes to the injected env (including the API key).
	payload := stub.ex.Cmd[len(stub.ex.Cmd)-1]
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("payload not base64: %v", err)
	}
	for _, want := range []string{"export ROUNDPEN_API_KEY=", "export ROUNDPEN_SANDBOX_ID=", "export ROUNDPEN_USER=\"Admin\""} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("decoded env missing %q: %q", want, raw)
		}
	}
}
