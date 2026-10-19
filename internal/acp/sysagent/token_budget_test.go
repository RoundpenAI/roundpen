package sysagent

import (
	"testing"

	acp "github.com/coder/acp-go-sdk"
)

func TestTokenBudgetExceeded(t *testing.T) {
	a := New(Deps{TokenBudget: 100})
	if a.tokenBudgetExceeded() {
		t.Fatal("empty usage should be under budget")
	}
	a.tokensUsed = 99
	if a.tokenBudgetExceeded() {
		t.Fatal("99 of 100 should still be under")
	}
	a.tokensUsed = 100
	if !a.tokenBudgetExceeded() {
		t.Fatal("100 of 100 should exceed")
	}
	a.ResetTokenAccounting(50)
	if a.TokensUsed() != 0 || a.deps.TokenBudget != 50 {
		t.Fatalf("reset: used=%d budget=%d", a.TokensUsed(), a.deps.TokenBudget)
	}
	a.SetTokenBudget(0)
	a.tokensUsed = 9999
	if a.tokenBudgetExceeded() {
		t.Fatal("zero budget means unlimited")
	}
}

func TestResumeSessionRebinds(t *testing.T) {
	a := New(Deps{})
	const sid = "sess-resume"
	if _, err := a.ResumeSession(t.Context(), acp.ResumeSessionRequest{SessionId: acp.SessionId(sid)}); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	s := a.sessions[sid]
	a.mu.Unlock()
	if s == nil || s.id != sid {
		t.Fatal("session not registered")
	}
	s.cancel = func() {}
	if _, err := a.ResumeSession(t.Context(), acp.ResumeSessionRequest{SessionId: acp.SessionId(sid)}); err != nil {
		t.Fatal(err)
	}
	if s.cancel != nil {
		t.Fatal("cancel should be cleared")
	}
	if _, err := a.ResumeSession(t.Context(), acp.ResumeSessionRequest{}); err == nil {
		t.Fatal("empty sessionId should fail")
	}
}
