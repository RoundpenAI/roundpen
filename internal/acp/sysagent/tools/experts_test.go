package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/expertrun"
	"github.com/RoundpenAI/roundpen/internal/acp/providers"
	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/authz"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

type stubEnvLister struct {
	views []userenv.EnvView
	err   error
}

func (s stubEnvLister) List(context.Context, string) ([]userenv.EnvView, error) {
	return s.views, s.err
}

type fakeExpertRunner struct {
	res   expertrun.Result
	err   error
	busy  bool
	probe error
}

func (f *fakeExpertRunner) Run(context.Context, expertrun.Request) (expertrun.Result, error) {
	return f.res, f.err
}
func (f *fakeExpertRunner) Busy(string) bool { return f.busy }
func (f *fakeExpertRunner) ProbeAuth(context.Context, string, authz.Actor, string, string) error {
	return f.probe
}

func TestListExperts_ClaudeReadyCodexUnavailableAttach(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterExperts(reg, &tools.ExpertBinder{
		Envs: stubEnvLister{views: []userenv.EnvView{
			{Slot: userenv.SlotAgent, Status: "absent"},
		}},
		SessionID: "sess-1",
		Runner:    &fakeExpertRunner{},
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
	if byID["claude"].State != tools.ExpertReady {
		t.Fatalf("claude=%+v", byID["claude"])
	}
	if byID["codex"].State != tools.ExpertUnavailable || byID["codex"].Reason != "attach_exec_not_enabled" {
		t.Fatalf("codex=%+v", byID["codex"])
	}
}

func TestDelegateToExpert_RealRun(t *testing.T) {
	reg := tools.NewRegistry()
	runner := &fakeExpertRunner{res: expertrun.Result{
		Status:       expertrun.StatusSucceeded,
		Summary:      "implemented feature",
		ChangedPaths: []string{"main.go"},
		PRIntent:     map[string]any{"action": "open_pr", "branch": "feat/x"},
	}}
	tools.RegisterExperts(reg, &tools.ExpertBinder{
		Envs:      stubEnvLister{views: []userenv.EnvView{{Slot: userenv.SlotAgent, Status: "running"}}},
		SessionID: "sess-2",
		Runner:    runner,
	})
	raw, _ := json.Marshal(map[string]any{
		"expertId": "claude",
		"goal":     "add feature",
		"repoPath": "projects/demo",
		"paths":    []string{"main.go"},
	})
	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "DelegateToExpert", raw)
	if err != nil {
		t.Fatal(err)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["outcome"] != "succeeded" || resp["mode"] != "user" {
		t.Fatalf("%s", out)
	}
	if resp["deliveryHint"] == nil || !strings.Contains(resp["deliveryHint"].(string), "CreatePullRequest") {
		t.Fatalf("missing deliveryHint: %s", out)
	}
	paths, _ := resp["changedPaths"].([]any)
	if len(paths) != 1 {
		t.Fatalf("paths=%v", paths)
	}
}

func TestDelegateToExpert_AutoClaude(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterExperts(reg, &tools.ExpertBinder{
		Envs:   stubEnvLister{views: []userenv.EnvView{{Slot: userenv.SlotAgent, Status: "running"}}},
		Runner: &fakeExpertRunner{res: expertrun.Result{Status: "succeeded", Summary: "ok"}},
	})
	raw, _ := json.Marshal(map[string]any{"goal": "fix flaky test"})
	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "DelegateToExpert", raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"mode":"auto"`) || !strings.Contains(out, `"expertId":"claude"`) {
		t.Fatalf("%s", out)
	}
}

func TestDelegateToExpert_NeedsAuthResult(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterExperts(reg, &tools.ExpertBinder{
		Envs: stubEnvLister{views: []userenv.EnvView{{Slot: userenv.SlotAgent, Status: "running"}}},
		Runner: &fakeExpertRunner{
			res: expertrun.Result{Status: expertrun.StatusNeedsAuth, Summary: "login", DeepLink: "/settings"},
			err: expertrun.ErrNeedsAuth,
		},
	})
	raw, _ := json.Marshal(map[string]any{"expertId": "claude", "goal": "x"})
	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "DelegateToExpert", raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"outcome":"needs_auth"`) || !strings.Contains(out, "deepLink") {
		t.Fatalf("%s", out)
	}
}

func TestTestExpertConnection_NeedsAuth(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterExperts(reg, &tools.ExpertBinder{
		Runner: &fakeExpertRunner{probe: errors.New("not logged in")},
	})
	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "TestExpertConnection", json.RawMessage(`{"expertId":"claude"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "needs_auth") || !strings.Contains(out, "deepLink") {
		t.Fatalf("%s", out)
	}
}

func TestDelegateToExpert_CodexNotEnabled(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterExperts(reg, &tools.ExpertBinder{
		Envs:   stubEnvLister{views: []userenv.EnvView{{Slot: userenv.SlotAgent, Status: "running"}}},
		Runner: &fakeExpertRunner{res: expertrun.Result{Status: "succeeded"}},
	})
	raw, _ := json.Marshal(map[string]any{"expertId": "codex", "goal": "x"})
	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "DelegateToExpert", raw)
	// codex is unavailable in list → structured unavailable body
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "unavailable") {
		t.Fatalf("%s", out)
	}
}

func TestCodingExpertsHelper(t *testing.T) {
	got := providers.CodingExperts(providers.Default())
	if len(got) < 2 || got[0].ID != "claude" || got[1].ID != "codex" {
		t.Fatalf("%+v", got)
	}
}

func TestRegisterSession_IncludesExperts(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterSession(reg, tools.SessionSurface{
		Roundpen:  &tools.RoundpenBinder{Envs: stubEnvLister{}},
		SessionID: "s",
		ExpertRun: &fakeExpertRunner{},
	})
	for _, name := range []string{"ListExperts", "DelegateToExpert", "TestExpertConnection"} {
		if _, ok := reg.Get(name); !ok {
			t.Fatalf("missing %s", name)
		}
	}
}
