package browser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/config"
)

func TestResolveRuntime(t *testing.T) {
	src := Source{Provider: config.CDPProviderDocker, Port: 3000}
	rt := ResolveRuntime("sb-1", src)
	if rt.Kind != RuntimeDocker || rt.SandboxID != "sb-1" || rt.ID != "sb-1" {
		t.Fatalf("docker runtime = %+v", rt)
	}

	remote := Source{Provider: config.CDPProviderRemote, Endpoint: "wss://x"}
	rt = ResolveRuntime("browser-alice", remote)
	if rt.Kind != RuntimeRemote || rt.SandboxID != "" {
		t.Fatalf("remote runtime = %+v", rt)
	}
}

func TestEnsureProfile(t *testing.T) {
	root := t.TempDir()
	p, err := EnsureProfile(root, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "user:alice" {
		t.Fatalf("id = %q", p.ID)
	}
	if filepath.Base(filepath.Dir(p.StatePath)) != "user_alice" {
		t.Fatalf("dir = %q", p.StatePath)
	}
	if _, err := os.Stat(filepath.Dir(p.StatePath)); err != nil {
		t.Fatal(err)
	}
}

func TestHubBindLayers(t *testing.T) {
	h := NewHub(t.TempDir(), nil)
	t.Cleanup(h.Close)
	eng := &fakeEngine{}
	h.newEngine = func(string, int, int) (Engine, error) { return eng, nil }

	src := Source{Provider: config.CDPProviderHost}
	profile, err := EnsureProfile(h.ProfileRoot(), "bob")
	if err != nil {
		t.Fatal(err)
	}
	h.Bind("browser-bob", src, profile)

	sess, err := h.Ensure(t.Context(), "browser-bob")
	if err != nil {
		t.Fatal(err)
	}
	if sess.Runtime.Kind != RuntimeHost {
		t.Fatalf("runtime = %q", sess.Runtime.Kind)
	}
	if sess.Profile.ID != "user:bob" {
		t.Fatalf("profile = %q", sess.Profile.ID)
	}
	if sess.ActiveTab != DefaultTabID || len(sess.Tabs) != 1 {
		t.Fatalf("tabs = %+v active=%q", sess.Tabs, sess.ActiveTab)
	}
	st := h.StatusEx("browser-bob")
	if st.ProfileID != "user:bob" || st.Runtime != RuntimeHost || st.ActiveTab != DefaultTabID {
		t.Fatalf("status = %#v", st)
	}
}
