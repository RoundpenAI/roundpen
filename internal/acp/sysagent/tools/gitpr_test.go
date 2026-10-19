package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

func TestGitPushRefusesDefaultBranch(t *testing.T) {
	ex := &stubExec{
		ws: t.TempDir(),
		queue: []*sandbox.ExecResult{
			{ExitCode: 0, Stdout: []byte("refs/remotes/origin/main\n")},
			{ExitCode: 0, Stdout: []byte("main\n")},
		},
	}
	reg := tools.NewRegistry()
	tools.RegisterGitPR(reg, &tools.AgentBinder{Slots: &stubAgentSlots{id: "sb-1"}, Exec: ex})
	_, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "GitPush",
		json.RawMessage(`{"repo_path":"projects/demo"}`))
	if err == nil || !strings.Contains(err.Error(), "default branch") {
		t.Fatalf("want refuse default branch, got %v", err)
	}
}

func TestCreatePullRequestRejectsGitea(t *testing.T) {
	ex := &stubExec{
		ws: t.TempDir(),
		queue: []*sandbox.ExecResult{
			{ExitCode: 0, Stdout: []byte("https://gitea.example.com/a/b.git\n")},
		},
	}
	reg := tools.NewRegistry()
	tools.RegisterGitPR(reg, &tools.AgentBinder{Slots: &stubAgentSlots{id: "sb-1"}, Exec: ex})
	_, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "CreatePullRequest",
		json.RawMessage(`{"repo_path":"projects/demo","title":"x"}`))
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "gitea") {
		t.Fatalf("want gitea extension error, got %v", err)
	}
}
