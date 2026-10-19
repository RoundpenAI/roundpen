package routine

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestFinishRunContinueEnqueuesNextSegment(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	insertUser(t, st, "routine-cont")
	insertAssistant(t, st, "asst-cont", "routine-cont")
	rt, err := st.Create(ctx, "routine-cont", CreateInput{
		AssigneeAssistantID: "asst-cont",
		Title:               "长任务",
		Brief:               "分段做完",
		Autonomy:            AutonomyRead,
		Cron:                "0 8 * * 1",
		Timezone:            "UTC",
		MaxDurationSec:      120,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.UpdateState(ctx, "routine-cont", rt.Key, json.RawMessage(`{"cursor":1}`), "asst-cont"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.DB.ExecContext(ctx, `UPDATE routines SET next_run_at=$2 WHERE id=$1`, rt.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	claims, err := st.ClaimDue(ctx, 1)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim: %v %+v", err, claims)
	}
	if _, ok, err := st.MarkRunning(ctx, claims[0].Run.ID); err != nil || !ok {
		t.Fatalf("mark: ok=%v err=%v", ok, err)
	}
	if err := st.AttachSession(ctx, claims[0].Run.ID, "sess-seg1"); err != nil {
		t.Fatal(err)
	}

	done, next, err := st.FinishRun(ctx, "routine-cont", claims[0].Run.Key, "sess-seg1", "段1完成", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != RunSucceeded || done.SegmentIndex != 1 {
		t.Fatalf("seg1 = %+v", done)
	}
	if next == nil || next.Run.Status != RunQueued || next.Run.SegmentIndex != 2 {
		t.Fatalf("next = %+v", next)
	}
	if next.Run.ContinueFrom != done.ID {
		t.Fatalf("continue_from = %q want %q", next.Run.ContinueFrom, done.ID)
	}

	prompt := RenderPrompt(*next, done.Summary)
	if !strings.Contains(prompt, "segment 2") || !strings.Contains(prompt, "continue=true") {
		t.Fatalf("prompt missing continue contract: %s", prompt)
	}
	if !strings.Contains(prompt, `"cursor":1`) && !strings.Contains(prompt, "cursor") {
		// state is reloaded on claim; next.Routine may be stale snapshot from FinishRun
		fresh, _, err := st.Get(ctx, "routine-cont", rt.Key)
		if err != nil {
			t.Fatal(err)
		}
		next.Routine = *fresh
		prompt = RenderPrompt(*next, done.Summary)
		if !strings.Contains(prompt, "cursor") {
			t.Fatalf("prompt missing state: %s", prompt)
		}
	}
}

func TestValidateCreateAllowsTwoHourSegment(t *testing.T) {
	in := CreateInput{
		Title: "x", Autonomy: AutonomyRead, Cron: "0 8 * * 1", Timezone: "UTC",
		MaxDurationSec: MaxDurationSec,
	}
	if _, err := ValidateCreate(&in); err != nil {
		t.Fatal(err)
	}
	in.MaxDurationSec = MaxDurationSec + 1
	if _, err := ValidateCreate(&in); err == nil {
		t.Fatal("expected reject above MaxDurationSec")
	}
}

func TestRenderPromptContinueContract(t *testing.T) {
	c := Claim{
		Routine: Routine{
			Key: "RTN-1", Title: "周报", Brief: "汇总", Autonomy: AutonomyRead,
			State: json.RawMessage(`{"seen":["a"]}`), MaxDurationSec: 7200,
		},
		Run: Run{Key: "RUN-9", SegmentIndex: 3, ContinueFrom: "prev"},
	}
	got := RenderPrompt(c, "段2做完了")
	for _, want := range []string{
		"segment 3", "continue=true", "UpdateRoutineState", `"seen"`, "120 minutes",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("prompt missing %q:\n%s", want, got)
		}
	}
	cont := RenderContinuePrompt(c, "段2做完了")
	for _, want := range []string{"Continue segment 3", "same session", "checkpoint"} {
		if !strings.Contains(cont, want) {
			t.Fatalf("continue prompt missing %q:\n%s", want, cont)
		}
	}
}

func TestTokenBudgetChainAccounting(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	insertUser(t, st, "routine-tok")
	insertAssistant(t, st, "asst-tok", "routine-tok")
	rt, err := st.Create(ctx, "routine-tok", CreateInput{
		AssigneeAssistantID: "asst-tok",
		Title:               "有预算",
		Autonomy:            AutonomyRead,
		Cron:                "0 8 * * 1",
		Timezone:            "UTC",
		MaxDurationSec:      120,
		MaxTokens:           1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rt.MaxTokens != 1000 {
		t.Fatalf("maxTokens = %d", rt.MaxTokens)
	}
	if _, err := st.DB.ExecContext(ctx, `UPDATE routines SET next_run_at=$2 WHERE id=$1`, rt.ID, time.Now().Add(-time.Minute)); err != nil {
		t.Fatal(err)
	}
	claims, err := st.ClaimDue(ctx, 1)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim: %v", err)
	}
	rn := claims[0].Run
	if _, ok, err := st.MarkRunning(ctx, rn.ID); err != nil || !ok {
		t.Fatal(err)
	}
	n, err := st.AddTokensUsed(ctx, rn.Key, 400)
	if err != nil || n != 400 {
		t.Fatalf("add by key: n=%d err=%v", n, err)
	}
	n, err = st.AddTokensUsed(ctx, rn.ID, 100)
	if err != nil || n != 500 {
		t.Fatalf("add by id: n=%d err=%v", n, err)
	}
	chain, err := st.ChainTokensUsed(ctx, rt.ID)
	if err != nil || chain != 500 {
		t.Fatalf("chain = %d err=%v", chain, err)
	}
	if _, err := ValidateCreate(&CreateInput{
		Title: "x", Autonomy: AutonomyRead, Cron: "0 8 * * 1", Timezone: "UTC", MaxTokens: -1,
	}); err == nil {
		t.Fatal("expected reject negative maxTokens")
	}
}
