package plugins

import "testing"

func TestSanitizeDropsUnknownAndDupes(t *testing.T) {
	got := Sanitize([]string{" ", SelfHostedGit, "SELF-HOSTED-GIT", "nope"})
	if len(got) != 1 || got[0] != SelfHostedGit {
		t.Fatalf("Sanitize = %v", got)
	}
}

func TestLookup(t *testing.T) {
	p, ok := Lookup("self-hosted-git")
	if !ok || p.ID != SelfHostedGit {
		t.Fatalf("lookup = %+v ok=%v", p, ok)
	}
	if _, ok := Lookup("gitlab"); ok {
		t.Fatal("gitlab is not a plugin id")
	}
}

func TestEnabled(t *testing.T) {
	if !Enabled([]string{SelfHostedGit}, "self-hosted-git") {
		t.Fatal("expected enabled")
	}
	if Enabled(nil, SelfHostedGit) {
		t.Fatal("empty set should be off")
	}
}
