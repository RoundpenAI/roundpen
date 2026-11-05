package tools_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/hostaccess"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/assistant"
)

func TestScanTVGaps(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"Show.S01E01.mkv", "Show.S01E03.mkv", "Show.S02E01.mkv"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sess := &agentsession.Session{ID: "s1", UserID: "alice", AssistantID: "a1"}
	access := hostaccess.New(
		fakeSessions{sess},
		fakeAssistants{&assistant.Assistant{ID: "a1", DirectoryGrants: []assistant.DirectoryGrant{{Path: dir, Mode: "read"}}}},
		nil,
	)
	reg := tools.NewRegistry()
	tools.RegisterMediaLib(reg, &tools.AgentBinder{Host: access.ForSession("s1")})

	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "ScanTVGaps",
		json.RawMessage(`{"path":`+mustJSON(dir)+`}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"missing":[2]`) && !strings.Contains(out, `"missing": [2]`) {
		t.Fatalf("want S01 missing 2, got %s", out)
	}
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

type fakeSessions struct{ sess *agentsession.Session }

func (f fakeSessions) Get(context.Context, string) (*agentsession.Session, error) {
	return f.sess, nil
}

type fakeAssistants struct{ a *assistant.Assistant }

func (f fakeAssistants) Get(context.Context, string) (*assistant.Assistant, error) {
	return f.a, nil
}
