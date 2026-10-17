package docker

import (
	"reflect"
	"testing"
)

func TestPullCandidatesGHCR(t *testing.T) {
	clearPreferredForTest()
	ref := "ghcr.io/browserless/chrome:v2.56.7"
	got := pullCandidates(ref, nil, false)
	want := []string{
		ref,
		"m.daocloud.io/ghcr.io/browserless/chrome:v2.56.7",
		"ghcr.m.daocloud.io/browserless/chrome:v2.56.7",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestPullCandidatesCustomAndPreferred(t *testing.T) {
	clearPreferredForTest()
	ref := "ghcr.io/roundpenai/code-agent:0.1.0"
	rememberPreferredPull(ref, "ghcr.m.daocloud.io/roundpenai/code-agent:0.1.0")
	got := pullCandidates(ref, []string{"mirror.example/{image}"}, false)
	if got[0] != "ghcr.m.daocloud.io/roundpenai/code-agent:0.1.0" {
		t.Fatalf("preferred first: %#v", got)
	}
	if got[1] != ref {
		t.Fatalf("original second: %#v", got)
	}
	if got[2] != "mirror.example/ghcr.io/roundpenai/code-agent:0.1.0" {
		t.Fatalf("custom third: %#v", got)
	}
}

func TestPullCandidatesDisableBuiltin(t *testing.T) {
	clearPreferredForTest()
	ref := "ghcr.io/browserless/chrome:v2.56.7"
	got := pullCandidates(ref, nil, true)
	if !reflect.DeepEqual(got, []string{ref}) {
		t.Fatalf("got %#v", got)
	}
}

func TestParseImageMirrors(t *testing.T) {
	custom, off := ParseImageMirrors("none")
	if !off || custom != nil {
		t.Fatalf("none: custom=%v off=%v", custom, off)
	}
	custom, off = ParseImageMirrors(" m.daocloud.io/{image} , ghcr.nju.edu.cn/{path} ")
	if off || !reflect.DeepEqual(custom, []string{"m.daocloud.io/{image}", "ghcr.nju.edu.cn/{path}"}) {
		t.Fatalf("custom=%v off=%v", custom, off)
	}
}

func TestExpandMirrorBareHost(t *testing.T) {
	got := expandMirror("ghcr.m.daocloud.io", "ghcr.io/browserless/chrome:v2.56.7")
	if got != "ghcr.m.daocloud.io/browserless/chrome:v2.56.7" {
		t.Fatalf("got %q", got)
	}
	got = expandMirror("m.daocloud.io", "ghcr.io/browserless/chrome:v2.56.7")
	if got != "m.daocloud.io/ghcr.io/browserless/chrome:v2.56.7" {
		t.Fatalf("got %q", got)
	}
}

func clearPreferredForTest() {
	preferredMu.Lock()
	preferredPull = map[string]string{}
	preferredMu.Unlock()
}
