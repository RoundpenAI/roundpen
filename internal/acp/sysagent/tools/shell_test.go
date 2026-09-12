package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

type stubAgentSlots struct {
	id    string
	user  string
	calls int
	fail  error
}

func (s *stubAgentSlots) EnsureAgent(_ context.Context, userID string) (*sandbox.Sandbox, error) {
	s.calls++
	s.user = userID
	if s.fail != nil {
		return nil, s.fail
	}
	return &sandbox.Sandbox{ID: s.id, Name: "agent-alice", Status: sandbox.StatusRunning}, nil
}

type stubExec struct {
	id      string
	cmd     []string
	workdir string
	ws      string
	res     *sandbox.ExecResult
	fail    error
}

func (s *stubExec) Exec(_ context.Context, id string, req sandbox.ExecRequest) (*sandbox.ExecResult, error) {
	s.id = id
	s.cmd = append([]string(nil), req.Cmd...)
	s.workdir = req.WorkDir
	if s.fail != nil {
		return nil, s.fail
	}
	if s.res != nil {
		return s.res, nil
	}
	return &sandbox.ExecResult{ExitCode: 0, Stdout: []byte("ok\n")}, nil
}

func (s *stubExec) WorkspaceHostPath(_ context.Context, _ string) (string, error) {
	return s.ws, nil
}

func TestBashUsesAgentSlot(t *testing.T) {
	slots := &stubAgentSlots{id: "sb-agent"}
	ex := &stubExec{ws: t.TempDir()}
	reg := tools.NewRegistry()
	tools.RegisterShell(reg, &tools.AgentBinder{Slots: slots, Exec: ex})

	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "Bash", json.RawMessage(`{"command":"git --version"}`))
	if err != nil {
		t.Fatal(err)
	}
	if slots.user != "alice" || slots.calls != 1 {
		t.Fatalf("slots user=%q calls=%d", slots.user, slots.calls)
	}
	if ex.id != "sb-agent" || ex.workdir != "/workspace" {
		t.Fatalf("exec id=%q workdir=%q", ex.id, ex.workdir)
	}
	if len(ex.cmd) != 3 || ex.cmd[0] != "/bin/sh" || ex.cmd[2] != "git --version" {
		t.Fatalf("cmd=%q", ex.cmd)
	}
	if !strings.Contains(out, `"exitCode":0`) || !strings.Contains(out, "ok") {
		t.Fatalf("out=%s", out)
	}
}

func TestBashRequiresCommand(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterShell(reg, &tools.AgentBinder{
		Slots: &stubAgentSlots{id: "sb"},
		Exec:  &stubExec{ws: t.TempDir()},
	})
	if _, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "Bash", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestBashNonZeroExit(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterShell(reg, &tools.AgentBinder{
		Slots: &stubAgentSlots{id: "sb"},
		Exec: &stubExec{
			ws:  t.TempDir(),
			res: &sandbox.ExecResult{ExitCode: 128, Stderr: []byte("fatal: not a git repo\n")},
		},
	})
	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "Bash", json.RawMessage(`{"command":"git status"}`))
	if err == nil {
		t.Fatal("expected exit error")
	}
	if !strings.Contains(out, `"exitCode":128`) || !strings.Contains(out, "not a git repo") {
		t.Fatalf("out=%s err=%v", out, err)
	}
}

func TestBashDescriptionHasNoSandbox(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterShell(reg, &tools.AgentBinder{
		Slots: &stubAgentSlots{id: "sb"},
		Exec:  &stubExec{ws: t.TempDir()},
	})
	for _, tool := range reg.List() {
		if strings.Contains(strings.ToLower(tool.Name), "sandbox") {
			t.Fatalf("name %q", tool.Name)
		}
		if strings.Contains(strings.ToLower(tool.Description), "sandbox") {
			t.Fatalf("desc %q", tool.Description)
		}
		if tool.Name == "roundpen_ensure_agent" {
			t.Fatal("ensure tool must not be registered")
		}
	}
}
