package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

func interactiveCtx(ask func(context.Context, tools.AskQuestion) (string, error), startPlan bool) (context.Context, *bool) {
	inPlan := startPlan
	return tools.WithInteractor(context.Background(), &tools.Interactor{
		AskUser:     ask,
		InPlanMode:  func() bool { return inPlan },
		SetPlanMode: func(on bool) { inPlan = on },
	}), &inPlan
}

func TestAskUserQuestion(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterInteractive(reg)

	if _, ok := reg.Get("AskUserQuestion"); !ok {
		t.Fatal("missing AskUserQuestion")
	}

	gotAnswer := ""
	ctx, _ := interactiveCtx(func(_ context.Context, q tools.AskQuestion) (string, error) {
		if len(q.Options) != 2 {
			t.Fatalf("len options=%d", len(q.Options))
		}
		gotAnswer = q.Options[1].Label
		return gotAnswer, nil
	}, false)

	out, err := reg.Call(ctx, tools.Actor{}, "AskUserQuestion", json.RawMessage(`{
		"question": "Which library?",
		"header": "Library",
		"options": [{"label":"A","description":"opt a"},{"label":"B","description":"opt b"}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Which library?") || !strings.Contains(out, gotAnswer) {
		t.Fatalf("out=%q", out)
	}

	// No interactor attached → the tool degrades cleanly.
	if _, err := reg.Call(context.Background(), tools.Actor{}, "AskUserQuestion", json.RawMessage(`{"question":"q?","options":[{"label":"A"},{"label":"B"}]}`)); err == nil {
		t.Fatal("expected error without interactor")
	}

	// Too few options.
	if _, err := reg.Call(ctx, tools.Actor{}, "AskUserQuestion", json.RawMessage(`{"question":"q?","options":[{"label":"A"}]}`)); err == nil {
		t.Fatal("expected error for a single option")
	}
	// Empty question.
	if _, err := reg.Call(ctx, tools.Actor{}, "AskUserQuestion", json.RawMessage(`{"options":[{"label":"A"},{"label":"B"}]}`)); err == nil {
		t.Fatal("expected error for empty question")
	}
	// Header too long.
	if _, err := reg.Call(ctx, tools.Actor{}, "AskUserQuestion", json.RawMessage(`{"question":"q?","header":"`+strings.Repeat("x", 31)+`","options":[{"label":"A"},{"label":"B"}]}`)); err == nil {
		t.Fatal("expected error for long header")
	}
}

func TestEnterPlanMode(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterInteractive(reg)
	if _, ok := reg.Get("EnterPlanMode"); !ok {
		t.Fatal("missing EnterPlanMode")
	}

	ctx, inPlan := interactiveCtx(nil, false)
	out, err := reg.Call(ctx, tools.Actor{}, "EnterPlanMode", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !*inPlan {
		t.Fatal("expected plan mode to be entered")
	}
	if !strings.Contains(out, "plan mode") {
		t.Fatalf("out=%q", out)
	}

	// Idempotent while already in plan mode.
	out2, err := reg.Call(ctx, tools.Actor{}, "EnterPlanMode", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out2, "already") {
		t.Fatalf("out2=%q", out2)
	}

	// No interactor → clean error.
	if _, err := reg.Call(context.Background(), tools.Actor{}, "EnterPlanMode", nil); err == nil {
		t.Fatal("expected error without interactor")
	}
}

func TestExitPlanMode(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterInteractive(reg)
	if _, ok := reg.Get("ExitPlanMode"); !ok {
		t.Fatal("missing ExitPlanMode")
	}

	// Outside plan mode: informational only, mode unchanged.
	ctxOut, inPlan := interactiveCtx(func(_ context.Context, _ tools.AskQuestion) (string, error) {
		t.Fatal("must not ask when not in plan mode")
		return "", nil
	}, false)
	out, err := reg.Call(ctxOut, tools.Actor{}, "ExitPlanMode", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not in plan mode") {
		t.Fatalf("out=%q", out)
	}
	if *inPlan {
		t.Fatal("mode must stay off plan")
	}

	// Approve flow.
	ctxApprove, inPlan := interactiveCtx(func(_ context.Context, q tools.AskQuestion) (string, error) {
		if !strings.Contains(q.Question, "plan") {
			t.Fatalf("question=%q", q.Question)
		}
		return "Approve", nil
	}, true)
	out, err = reg.Call(ctxApprove, tools.Actor{}, "ExitPlanMode", json.RawMessage(`{"plan":"step 1, step 2"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "approved") || !strings.Contains(out, "implementing") {
		t.Fatalf("out=%q", out)
	}
	if *inPlan {
		t.Fatal("plan mode must be disabled after approval")
	}

	// Reject flow stays in plan mode.
	ctxReject, inPlan := interactiveCtx(func(_ context.Context, _ tools.AskQuestion) (string, error) {
		return "Reject", nil
	}, true)
	out, err = reg.Call(ctxReject, tools.Actor{}, "ExitPlanMode", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "rejected") {
		t.Fatalf("out=%q", out)
	}
	if !*inPlan {
		t.Fatal("plan mode must persist after rejection")
	}
}
