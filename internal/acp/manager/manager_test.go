package manager_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	acpclient "github.com/RoundpenAI/roundpen/internal/acp/client"
	"github.com/RoundpenAI/roundpen/internal/acp/manager"
	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/workspace"
)

type noopMgr struct{}

func (noopMgr) Create(context.Context, sandbox.CreateRequest) (*sandbox.Sandbox, error) {
	return nil, nil
}
func (noopMgr) Get(context.Context, string) (*sandbox.Sandbox, error) { return nil, nil }
func (noopMgr) List(context.Context, sandbox.ListFilter) ([]*sandbox.Sandbox, error) {
	return nil, nil
}
func (noopMgr) Resolve(context.Context, sandbox.ResolveRequest) (*sandbox.Sandbox, error) {
	return nil, nil
}
func (noopMgr) Stop(context.Context, string) error   { return nil }
func (noopMgr) Delete(context.Context, string) error { return nil }
func (noopMgr) SetTimeout(context.Context, string, time.Duration) (*sandbox.Sandbox, error) {
	return nil, nil
}
func (noopMgr) Connect(context.Context, string) (*sandbox.Sandbox, bool, error) {
	return nil, false, nil
}
func (noopMgr) Refresh(context.Context, string) (*sandbox.Sandbox, error) { return nil, nil }
func (noopMgr) Rename(context.Context, string, string) (*sandbox.Sandbox, error) {
	return nil, nil
}
func (noopMgr) Update(context.Context, string, sandbox.UpdateRequest) (*sandbox.Sandbox, error) {
	return nil, nil
}
func (noopMgr) Exec(context.Context, string, sandbox.ExecRequest) (*sandbox.ExecResult, error) {
	return nil, nil
}
func (noopMgr) ListFiles(context.Context, string, string) ([]workspace.DirEntry, error) {
	return nil, nil
}
func (noopMgr) StatFile(context.Context, string, string) (*workspace.FileStat, error) {
	return nil, nil
}
func (noopMgr) ReadFile(context.Context, string, string) (io.ReadCloser, error) {
	return io.NopCloser(nil), nil
}
func (noopMgr) WriteFile(context.Context, string, string, io.Reader) error { return nil }
func (noopMgr) RemoveFile(context.Context, string, string) error           { return nil }
func (noopMgr) WorkspaceHostPath(context.Context, string) (string, error) {
	return "", nil
}
func (noopMgr) Dial(context.Context, string, int) (net.Conn, error) { return nil, nil }
func (noopMgr) Touch(context.Context, string) error                 { return nil }
func (noopMgr) AttachTerminal(context.Context, string, string, sandbox.TerminalOpts, io.Reader, io.Writer) error {
	return nil
}
func (noopMgr) ResizeTerminal(context.Context, string, string, uint16, uint16) error {
	return nil
}
func (noopMgr) AttachExec(context.Context, string, sandbox.AttachExecOpts, io.Reader, io.Writer, io.Writer) error {
	return nil
}

var _ sandbox.Manager = noopMgr{}

func TestManager_SysadminPromptNoSandbox(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/llmgw/openai/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "hello from sysagent"}, "finish_reason": "stop"},
			},
		})
	}))
	defer llm.Close()

	m := manager.New(slog.Default(), noopMgr{}, providers.Default(), manager.SysDeps{
		LoopbackBase: llm.URL,
		LLMKey:       "vk-test",
		DefaultModel: func() string { return "gpt-test" },
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	rt, err := m.Start(ctx, "sess-1", "", "sysadmin", manager.StartOpts{
		AutoApprove: true,
		Actor:       manager.Actor{Username: "u", Role: "user", APIKey: "k"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop("sess-1")

	var saw atomic.Bool
	rt.SetEventHandler(func(ev acpclient.Event) {
		if ev.Type == "agent_message" && ev.Text != "" {
			saw.Store(true)
		}
	})

	stop, err := m.Prompt(ctx, "sess-1", "hi")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if stop == "" {
		t.Fatal("empty stop reason")
	}
	if !saw.Load() {
		t.Fatal("expected agent_message event")
	}
}

func TestProviders_NeedsSandbox(t *testing.T) {
	sys, ok := providers.ByID(providers.Default(), "sysadmin")
	if !ok || providers.NeedsSandbox(sys) {
		t.Fatalf("sysadmin should not need sandbox: %+v", sys)
	}
	stdio, ok := providers.ByID(providers.Default(), "stdio")
	if !ok || !providers.NeedsSandbox(stdio) {
		t.Fatalf("stdio should need sandbox: %+v", stdio)
	}
	claude, ok := providers.ByID(providers.Default(), "claude")
	if !ok || !providers.NeedsSandbox(claude) || claude.TemplateID != "code-agent" || claude.Command != "claude-agent-acp" {
		t.Fatalf("claude provider: %+v", claude)
	}
}
