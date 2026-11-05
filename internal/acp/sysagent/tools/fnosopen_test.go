package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/assistant"
	"github.com/RoundpenAI/roundpen/internal/hostaccess"
)

func TestOpenHostPathEmitsClientAction(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sess := &agentsession.Session{ID: "s1", UserID: "alice", AssistantID: "a1"}
	access := hostaccess.New(
		fakeSessions{sess},
		fakeAssistants{&assistant.Assistant{
			ID: "a1",
			DirectoryGrants: []assistant.DirectoryGrant{{Path: dir, Mode: "read"}},
		}},
		nil,
	)
	reg := tools.NewRegistry()
	tools.RegisterFnosOpen(reg, &tools.AgentBinder{Host: access.ForSession("s1")})

	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "OpenHostPath",
		mustRaw(map[string]any{"path": file, "action": "open"}))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, tools.ClientActionKey) || !strings.Contains(out, "fnos.open") {
		t.Fatalf("out=%s", out)
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	act, _ := payload[tools.ClientActionKey].(map[string]any)
	if act["path"] != file {
		t.Fatalf("action=%v", act)
	}
}

func mustRaw(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
