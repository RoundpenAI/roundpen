package agentenv_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/agentenv"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
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

func TestApplyLLMEnv(t *testing.T) {
	env := map[string]string{"ANTHROPIC_DEFAULT_SONNET_MODEL": "keep-me"}
	agentenv.ApplyLLMEnv(env, "http://127.0.0.1:19001/", "vk-test", agentenv.LLMEnv{
		AnthropicProvider: "glimm-anthropic",
		OpenAIProvider:    "deepseek",
		Model:             " nvidia/nemotron-3.5-lightning:free ",
		ModeModels:        map[string]string{"llm.plan": "planner-model"},
		Extra:             map[string]string{"ROUNDPEN_LLM_PROVIDER": "deepseek"},
	})
	if env["ANTHROPIC_BASE_URL"] != "http://127.0.0.1:19001/llmgw/glimm-anthropic" {
		t.Fatalf("anthropic base: %q", env["ANTHROPIC_BASE_URL"])
	}
	if env["OPENAI_BASE_URL"] != "http://127.0.0.1:19001/llmgw/deepseek" {
		t.Fatalf("openai base: %q", env["OPENAI_BASE_URL"])
	}
	if env["ANTHROPIC_MODEL"] != "nvidia/nemotron-3.5-lightning:free" {
		t.Fatalf("model: %q", env["ANTHROPIC_MODEL"])
	}
	if env["ANTHROPIC_DEFAULT_SONNET_MODEL"] != "keep-me" {
		t.Fatalf("preserved: %q", env["ANTHROPIC_DEFAULT_SONNET_MODEL"])
	}
	if env["ANTHROPIC_DEFAULT_OPUS_MODEL"] != "planner-model" {
		t.Fatalf("plan pin: %q", env["ANTHROPIC_DEFAULT_OPUS_MODEL"])
	}
	if env["ROUNDPEN_LLM_PROVIDER"] != "deepseek" {
		t.Fatalf("extra: %q", env["ROUNDPEN_LLM_PROVIDER"])
	}

	// An empty env plan leaves whatever the operator set alone.
	untouched := map[string]string{"ANTHROPIC_BASE_URL": "http://elsewhere"}
	agentenv.ApplyLLMEnv(untouched, "http://127.0.0.1:19001", "vk", agentenv.LLMEnv{})
	if untouched["ANTHROPIC_BASE_URL"] != "http://elsewhere" {
		t.Fatalf("empty plan must not write: %q", untouched["ANTHROPIC_BASE_URL"])
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

func TestProvisionOwnModelsWithholdsGatewayEnv(t *testing.T) {
	stub := &stubSandboxes{}
	p := &agentenv.Provisioner{Sandboxes: stub, Config: agentenv.Config{
		PublicURL:   "http://127.0.0.1:19001",
		VirtualKey:  "vk-test",
		LLMEnv:      agentenv.LLMEnv{OpenAIProvider: "openai", Model: "some-model"},
		ModelSource: storage.ModelSourceOwn,
	}}
	res, err := p.Provision(context.Background(), "sess-1", "claude", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"ROUNDPEN_URL", "ROUNDPEN_API_KEY"} {
		if _, ok := res.Env[k]; !ok {
			t.Fatalf("control-plane env %s must survive: %+v", k, res.Env)
		}
	}
	for _, k := range []string{
		"OPENAI_BASE_URL", "ANTHROPIC_BASE_URL", "OPENAI_API_KEY",
		"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "ANTHROPIC_MODEL",
	} {
		if v, ok := res.Env[k]; ok {
			t.Fatalf("%s must not be injected in own mode: %q", k, v)
		}
	}
}

func TestProxyEnv(t *testing.T) {
	if env := agentenv.ProxyEnv("  ", "x"); env != nil {
		t.Fatalf("empty proxy must skip injection: %+v", env)
	}
	env := agentenv.ProxyEnv("http://proxy.local:7890", "10.0.0.5")
	if env["HTTPS_PROXY"] != "http://proxy.local:7890" || env["https_proxy"] != "http://proxy.local:7890" {
		t.Fatalf("proxy vars: %+v", env)
	}
	noProxy := env["NO_PROXY"]
	for _, want := range []string{"localhost", "127.0.0.1", "::1", "10.0.0.5"} {
		if !strings.Contains(noProxy, want) {
			t.Fatalf("NO_PROXY %q missing %q", noProxy, want)
		}
	}
	if env["no_proxy"] != noProxy {
		t.Fatalf("lowercase NO_PROXY mismatch: %+v", env)
	}
}

func TestProvisionInjectsProxyEnv(t *testing.T) {
	stub := &stubSandboxes{}
	p := &agentenv.Provisioner{Sandboxes: stub, Config: agentenv.Config{
		PublicURL: "http://10.0.0.5:19001",
		ProxyURL:  "socks5://10.0.0.9:1080",
	}}
	res, err := p.Provision(context.Background(), "sess-1", "claude", "Admin")
	if err != nil {
		t.Fatal(err)
	}
	if res.Env["HTTPS_PROXY"] != "socks5://10.0.0.9:1080" {
		t.Fatalf("proxy env: %+v", res.Env)
	}
	if !strings.Contains(res.Env["NO_PROXY"], "10.0.0.5") {
		t.Fatalf("control-plane host must bypass the proxy: %q", res.Env["NO_PROXY"])
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
