package browser

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/config"
)

type recDial struct {
	mu sync.Mutex
	n  int
	id string
}

func (r *recDial) Dial(_ context.Context, sandboxID string, destPort int) (net.Conn, error) {
	r.mu.Lock()
	r.n++
	r.id = sandboxID
	r.mu.Unlock()
	_ = destPort
	return nil, fmt.Errorf("dialed")
}

// snapshot reads the recorder under its lock; the proxy dials from its own
// goroutines while the test inspects them.
func (r *recDial) snapshot() (int, string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n, r.id
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
	t.Cleanup(h.Close)
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
	n, id := d.snapshot()
	if id != "sb-browser" {
		t.Fatalf("dialed sandbox %q (n=%d) err=%v", id, n, err)
	}
}

func TestHubBrowserTokenUsesLookup(t *testing.T) {
	h := NewHub(t.TempDir(), nil)
	h.SetTokenLookup(func(id string) string {
		if id != "sb-1" {
			t.Fatalf("lookup id = %q", id)
		}
		return "tok-1"
	})
	if got := h.browserToken("sb-1"); got != "tok-1" {
		t.Fatalf("browserToken = %q, want tok-1", got)
	}
	h2 := NewHub(t.TempDir(), nil)
	if got := h2.browserToken("sb-1"); got != "" {
		t.Fatalf("empty lookup should yield empty token, got %q", got)
	}
}

// droppableEngine is a fakeEngine whose upstream link can be severed, the way
// a remote browserless reaps an idle browser.
type droppableEngine struct {
	fakeEngine
	connected atomic.Bool
	closed    atomic.Int32
}

func newDroppableEngine() *droppableEngine {
	e := &droppableEngine{}
	e.connected.Store(true)
	return e
}

func (e *droppableEngine) IsConnected() bool { return e.connected.Load() }

func (e *droppableEngine) Close() error {
	e.closed.Add(1)
	e.connected.Store(false)
	return nil
}

func TestHubEnsureReattachesDroppedBrowser(t *testing.T) {
	h := NewHub(t.TempDir(), nil)
	t.Cleanup(h.Close)
	var engines []*droppableEngine
	h.newEngine = func(string, int, int) (Engine, error) {
		eng := newDroppableEngine()
		engines = append(engines, eng)
		return eng, nil
	}
	id := AgentBrowserID("s1")
	first, err := h.Ensure(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(engines) != 1 {
		t.Fatalf("attaches = %d, want 1", len(engines))
	}

	// The upstream browser is gone: the cached engine must not be handed out
	// again, and status must stop advertising the dead page.
	engines[0].connected.Store(false)
	if st := h.StatusEx(id); st.Attached {
		t.Fatalf("status %#v, want detached", st)
	}
	second, err := h.Ensure(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if second.Engine == first.Engine {
		t.Fatal("Ensure handed out the dead engine")
	}
	if len(engines) != 2 {
		t.Fatalf("attaches = %d, want 2 (one re-attach)", len(engines))
	}
	if got := engines[0].closed.Load(); got != 1 {
		t.Fatalf("dead engine closed %d times, want 1", got)
	}
	if st := h.StatusEx(id); !st.Attached {
		t.Fatalf("status %#v, want attached after re-attach", st)
	}
}

func TestCDPRetryable(t *testing.T) {
	// The docker provider wraps attach failures around the playwright error;
	// the guest browser is not serving CDP yet while browserless boots.
	if !cdpRetryable(fmt.Errorf("env cdp (Browser env :3000 not serving CDP): cdp attach: connect ECONNREFUSED 127.0.0.1:39999")) {
		t.Fatal("docker attach refused should retry")
	}
	if !cdpRetryable(fmt.Errorf("env cdp (Browser env :3000 not serving CDP): cdp attach: WebSocket was closed before the connection was established")) {
		t.Fatal("browserless not ready should retry")
	}
	if cdpRetryable(fmt.Errorf("docker cdp requires a sandbox dialer")) {
		t.Fatal("missing dialer should not retry")
	}
}
