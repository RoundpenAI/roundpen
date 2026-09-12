package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

func TestGlobRunsFind(t *testing.T) {
	slots := &stubAgentSlots{id: "sb"}
	ex := &stubExec{ws: t.TempDir(), res: &sandbox.ExecResult{
		ExitCode: 0, Stdout: []byte("/workspace/src/a.ts\n/workspace/src/b.ts\n"),
	}}
	reg := tools.NewRegistry()
	tools.RegisterSearch(reg, &tools.AgentBinder{Slots: slots, Exec: ex})
	out, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "Glob",
		json.RawMessage(`{"pattern":"**/*.ts"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.ts") {
		t.Fatalf("out=%s", out)
	}
	if slots.calls != 1 {
		t.Fatalf("ensure calls=%d", slots.calls)
	}
	if len(ex.cmd) < 3 || ex.cmd[0] != "/bin/sh" {
		t.Fatalf("cmd=%q", ex.cmd)
	}
}

func TestGrepRequiresPattern(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterSearch(reg, &tools.AgentBinder{
		Slots: &stubAgentSlots{id: "sb"},
		Exec:  &stubExec{ws: t.TempDir()},
	})
	if _, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "Grep",
		json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error")
	}
}

func TestGrepNoMatches(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterSearch(reg, &tools.AgentBinder{
		Slots: &stubAgentSlots{id: "sb"},
		Exec: &stubExec{
			ws:  t.TempDir(),
			res: &sandbox.ExecResult{ExitCode: 1},
		},
	})
	out, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "Grep",
		json.RawMessage(`{"pattern":"zzz"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "No matches") {
		t.Fatalf("out=%s", out)
	}
}
