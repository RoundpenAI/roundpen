package browser

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/config"
)

type recDial struct {
	n  int
	id string
}

func (r *recDial) Dial(_ context.Context, sandboxID string, destPort int) (net.Conn, error) {
	r.n++
	r.id = sandboxID
	_ = destPort
	return nil, fmt.Errorf("dialed")
}

func TestHubAttachDockerWithoutDialer(t *testing.T) {
	h := NewHub(t.TempDir(), nil)
	h.SetConfig(&config.Config{
		Backend: "docker",
		CDP:     config.CDPConfig{Provider: config.CDPProviderDocker, Port: 9222},
	})
	_, err := h.Ensure(t.Context(), "sb-1")
	if err == nil {
		t.Fatal("expected docker cdp to fail without a dialer")
	}
}

func TestAgentBrowserID(t *testing.T) {
	if got := AgentBrowserID("abc"); got != "sysagent-abc" {
		t.Fatalf("got %q", got)
	}
	if got := AgentBrowserID("sysagent-abc"); got != "sysagent-abc" {
		t.Fatalf("got %q", got)
	}
}

func TestHubTakeover(t *testing.T) {
	h := NewHub(t.TempDir(), nil)
	eng := &fakeEngine{}
	h.newEngine = func(string, int, int) (Engine, error) { return eng, nil }
	id := AgentBrowserID("s1")
	if _, err := h.Ensure(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if h.Takeover(id) {
		t.Fatal("expected no takeover")
	}
	if err := h.SetTakeover(id, true); err != nil {
		t.Fatal(err)
	}
	st := h.StatusEx(id)
	if !st.Takeover || !st.Attached {
		t.Fatalf("status %#v", st)
	}
	if err := h.SetTakeover(id, false); err != nil {
		t.Fatal(err)
	}
	if h.Takeover(id) {
		t.Fatal("expected takeover cleared")
	}
}

func TestHubDockerDialsGivenSandbox(t *testing.T) {
	h := NewHub(t.TempDir(), nil)
	h.SetConfig(&config.Config{
		Backend: "docker",
		CDP:     config.CDPConfig{Provider: config.CDPProviderDocker, Port: 9222},
	})
	d := &recDial{}
	h.SetDialer(d)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	_, err := h.Ensure(ctx, "sb-browser")
	if err == nil {
		t.Fatal("expected CDP attach to fail without a guest")
	}
	if d.id != "sb-browser" {
		t.Fatalf("dialed sandbox %q (n=%d) err=%v", d.id, d.n, err)
	}
}

func TestCDPRetryable(t *testing.T) {
	if !cdpRetryable(fmt.Errorf("env cdp (nothing listening on guest :9222): cdp attach: connection reset by peer")) {
		t.Fatal("guest reset should retry")
	}
	if cdpRetryable(fmt.Errorf("docker cdp requires a sandbox dialer")) {
		t.Fatal("missing dialer should not retry")
	}
}
