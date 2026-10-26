package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

type stubEnvLister struct {
	views []userenv.EnvView
	err   error
}

func (s stubEnvLister) List(context.Context, string) ([]userenv.EnvView, error) {
	return s.views, s.err
}

func TestListExperts_ClaudeAndCodexReady(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterExperts(reg, &tools.ExpertBinder{
		Envs: stubEnvLister{views: []userenv.EnvView{
			{Slot: userenv.SlotAgent, Status: "absent"},
		}},
		SessionID: "sess-1",
	})
	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "ListExperts", nil)
	if err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Experts []tools.ExpertView `json:"experts"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatal(err)
	}
	byID := map[string]tools.ExpertView{}
	for _, e := range payload.Experts {
		byID[e.ID] = e
	}
	for _, id := range []string{"claude", "codex"} {
		e, ok := byID[id]
		if !ok {
			t.Fatalf("missing expert %q in %s", id, out)
		}
		if e.State != tools.ExpertReady {
			t.Fatalf("%s state=%q want ready", id, e.State)
		}
	}
	if _, ok := byID["sysadmin"]; ok {
		t.Fatal("sysadmin must not appear as expert")
	}
	if _, ok := byID["stdio"]; ok {
		t.Fatal("stdio placeholder must not appear as expert")
	}
}

func TestDelegateToExpert_AutoStub(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterExperts(reg, &tools.ExpertBinder{
		Envs:      stubEnvLister{views: []userenv.EnvView{{Slot: userenv.SlotAgent, Status: "running"}}},
		SessionID: "sess-2",
	})
	raw, _ := json.Marshal(map[string]any{"goal": "fix the flaky test"})
	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "DelegateToExpert", raw)
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["outcome"] != "stubbed" || resp["mode"] != "auto" {
		t.Fatalf("got %#v", resp)
	}
	if resp["expertId"] != "claude" {
		t.Fatalf("auto should pick first ready (claude), got %v", resp["expertId"])
	}
	if resp["handoff"] != "one_shot" {
		t.Fatalf("handoff=%v", resp["handoff"])
	}
}

func TestDelegateToExpert_UserPickUnknown(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterExperts(reg, &tools.ExpertBinder{SessionID: "s"})
	raw, _ := json.Marshal(map[string]any{"expertId": "nope", "goal": "x"})
	_, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "DelegateToExpert", raw)
	if err == nil || !strings.Contains(err.Error(), "unknown expert") {
		t.Fatalf("err=%v", err)
	}
}

func TestDelegateToExpert_NeedsReady(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterExperts(reg, &tools.ExpertBinder{
		Envs: stubEnvLister{views: []userenv.EnvView{
			{Slot: userenv.SlotAgent, Status: "missing"},
		}},
	})
	raw, _ := json.Marshal(map[string]any{"expertId": "claude", "goal": "x"})
	_, err := reg.Call(context.Background(), tools.Actor{Username: "bob"}, "DelegateToExpert", raw)
	if err == nil || !strings.Contains(err.Error(), "unavailable") {
		t.Fatalf("err=%v", err)
	}
}

func TestCodingExpertsHelper(t *testing.T) {
	got := providers.CodingExperts(providers.Default())
	if len(got) < 2 {
		t.Fatalf("want claude+codex, got %+v", got)
	}
	if got[0].ID != "claude" || got[1].ID != "codex" {
		t.Fatalf("order/ids: %+v", got)
	}
}

func TestRegisterSession_IncludesExperts(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterSession(reg, tools.SessionSurface{
		Roundpen:  &tools.RoundpenBinder{Envs: stubEnvLister{}},
		SessionID: "s",
	})
	for _, name := range []string{"ListExperts", "DelegateToExpert"} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("missing %s", name)
		}
	}
}
