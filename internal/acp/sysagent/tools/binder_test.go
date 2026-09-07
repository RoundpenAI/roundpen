package tools

import (
	"context"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/browser"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

type stubSlots struct {
	id    string
	user  string
	calls int
	fail  error
}

func (s *stubSlots) EnsureBrowser(_ context.Context, userID string) (*sandbox.Sandbox, error) {
	s.calls++
	s.user = userID
	if s.fail != nil {
		return nil, s.fail
	}
	return &sandbox.Sandbox{ID: s.id, Name: "browser-alice"}, nil
}

func TestBrowserBinderUsesSlotSandbox(t *testing.T) {
	hub := browser.NewHub(t.TempDir(), nil)
	// newEngine is unexported; Ensure with Slots still needs a working hub.
	// Use Status after Ensure via a fake by attaching through public API only
	// if Chrome exists — instead check resolveID.
	slots := &stubSlots{id: "sb-browser"}
	b := &BrowserBinder{Hub: hub, Slots: slots, SessionID: "sess-1"}
	id, err := b.resolveID(t.Context(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if id != "sb-browser" {
		t.Fatalf("hub id=%q", id)
	}
	if slots.user != "alice" || slots.calls != 1 {
		t.Fatalf("slots user=%q calls=%d", slots.user, slots.calls)
	}
	fallback, err := (&BrowserBinder{SessionID: "sess-1"}).resolveID(t.Context(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if fallback != browser.AgentBrowserID("sess-1") {
		t.Fatalf("fallback=%q", fallback)
	}
}
