package sandbox

import (
	"testing"
)

func TestSandboxEnvSetsGuestHome(t *testing.T) {
	env := sandboxEnv(map[string]string{"FOO": "bar"}, "agent", true)
	if env["HOME"] != GuestHomeDir {
		t.Fatalf("HOME = %q", env["HOME"])
	}
	if env["GIT_CONFIG_GLOBAL"] != GuestGitConfigGlobal {
		t.Fatalf("GIT_CONFIG_GLOBAL = %q", env["GIT_CONFIG_GLOBAL"])
	}
	if env["ROUNDPEN_SLOT"] != "agent" || env["FOO"] != "bar" {
		t.Fatalf("env = %#v", env)
	}
}

func TestSandboxEnvSkipsGuestHomeWithoutMount(t *testing.T) {
	env := sandboxEnv(nil, "", false)
	if _, ok := env["HOME"]; ok {
		t.Fatalf("unexpected HOME: %#v", env)
	}
}

func TestWithGuestHomeEnvCallerWins(t *testing.T) {
	got := withGuestHomeEnv(map[string]string{"HOME": "/custom"}, true)
	if got["HOME"] != "/custom" {
		t.Fatalf("HOME = %q", got["HOME"])
	}
	if got["GIT_CONFIG_GLOBAL"] != GuestGitConfigGlobal {
		t.Fatalf("GIT_CONFIG_GLOBAL = %q", got["GIT_CONFIG_GLOBAL"])
	}
}

func TestWithGuestHomeEnvSlice(t *testing.T) {
	got := withGuestHomeEnvSlice([]string{"HOME=/custom", "A=1"}, true)
	if len(got) < 3 || got[0] != "HOME="+GuestHomeDir {
		t.Fatalf("got %#v", got)
	}
	// Later HOME=/custom wins for Docker (last assignment typically wins in shells;
	// Docker ExecConfig replaces by key when merging — we still prepend defaults).
	found := false
	for _, e := range got {
		if e == "HOME=/custom" {
			found = true
		}
	}
	if !found {
		t.Fatalf("caller HOME missing: %#v", got)
	}
}
