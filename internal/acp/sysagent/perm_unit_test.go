package sysagent

import (
	"context"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

func newTestAgent(t *testing.T) (*Agent, string) {
	t.Helper()
	a := New(Deps{})
	sid := "s1"
	a.mu.Lock()
	a.sessions[sid] = &session{
		allowTools: map[string]bool{},
		denyTools:  map[string]bool{},
	}
	a.mu.Unlock()
	return a, sid
}

func TestApplyToolPerm(t *testing.T) {
	a, sid := newTestAgent(t)

	if !a.applyToolPerm(sid, "Bash", "allow") {
		t.Fatal("allow should permit the call")
	}
	if _, cached := a.toolPermCached(sid, "Bash"); cached {
		t.Fatal("allow must not be cached for the session")
	}

	if !a.applyToolPerm(sid, "Write", "allow_tool") {
		t.Fatal("allow_tool should permit the call")
	}
	// Session-scoped allow is cached and survives later denies of other tools.
	allowed, cached := a.toolPermCached(sid, "Write")
	if !allowed || !cached {
		t.Fatalf("Write should be session-allowed, got allowed=%v cached=%v", allowed, cached)
	}

	if a.applyToolPerm(sid, "WebFetch", "reject") {
		t.Fatal("reject should deny the call")
	}
	if _, cached = a.toolPermCached(sid, "WebFetch"); cached {
		t.Fatal("reject-once must not be cached")
	}

	if !a.applyToolPerm(sid, "Edit", "allow_all") {
		t.Fatal("allow_all should permit the call")
	}
	allowed, _ = a.toolPermCached(sid, "AnythingElse")
	if !allowed {
		t.Fatal("allow_all should cover every tool")
	}

	if a.applyToolPerm(sid, "Bash", "reject_tool") {
		t.Fatal("reject_tool should deny the call")
	}
	allowed, cached = a.toolPermCached(sid, "Bash")
	if allowed || !cached {
		t.Fatal("Bash should be session-denied")
	}

	// Unknown sessions have no state to record; the user's explicit choice is
	// honored directly.
	if !a.applyToolPerm("missing", "Bash", "allow_tool") {
		t.Fatal("missing session should honor an explicit allow choice")
	}
	if !a.applyToolPerm("missing", "Bash", "allow") {
		t.Fatal("missing session should honor an explicit allow choice")
	}
	if a.applyToolPerm("missing", "Bash", "reject_tool") {
		t.Fatal("missing session should honor an explicit reject choice")
	}
}

func TestToolPermCachedMissingSession(t *testing.T) {
	a, _ := newTestAgent(t)
	if _, cached := a.toolPermCached("missing", "Bash"); cached {
		t.Fatal("missing session must report no cached decision")
	}
}

func TestInteractorPlanModeAndAskUserGuard(t *testing.T) {
	a, sid := newTestAgent(t)
	i := a.interactor(sid)

	if i.InPlanMode() {
		t.Fatal("plan mode should start off")
	}
	i.SetPlanMode(true)
	if !i.InPlanMode() {
		t.Fatal("SetPlanMode(true) did not stick")
	}
	// Session isolation: another session is unaffected.
	other := "s2"
	a.mu.Lock()
	a.sessions[other] = &session{allowTools: map[string]bool{}, denyTools: map[string]bool{}}
	a.mu.Unlock()
	if a.interactor(other).InPlanMode() {
		t.Fatal("plan mode leaked across sessions")
	}
	// setPlanMode on a missing session is a no-op, not a panic.
	a.interactor("missing").SetPlanMode(true)

	// askUser without a connection errors instead of panicking.
	if _, err := i.AskUser(context.Background(), tools.AskQuestion{
		Question: "q?",
		Options:  []tools.AnswerOption{{Label: "A"}, {Label: "B"}},
	}); err == nil {
		t.Fatal("askUser without a connection should fail")
	}
}
