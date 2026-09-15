package gitcred

import (
	"strings"
	"testing"
)

func TestNormalizeHost(t *testing.T) {
	cases := map[string]string{
		"https://git.eaxi.com/ttz/gem-vault.git": "git.eaxi.com",
		"git@git.eaxi.com:ttz/gem-vault.git":     "git.eaxi.com",
		"github.com":                             "github.com",
	}
	for in, want := range cases {
		if got := normalizeHost(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestGitConfigRewritesSSH(t *testing.T) {
	cfg := gitConfig([]Cred{{Provider: ProviderGitea, Host: "git.eaxi.com", Token: "rp-secret-pat"}})
	if !strings.Contains(cfg, `insteadOf = git@git.eaxi.com:`) {
		t.Fatalf("config:\n%s", cfg)
	}
	if strings.Contains(cfg, "rp-secret-pat") {
		t.Fatal("token must not appear in git config")
	}
	if !strings.Contains(cfg, "/home/roundpen/.roundpen/git/credentials") {
		t.Fatalf("credential store must point into guest home:\n%s", cfg)
	}
}

func TestCredentialStoreHasHTTPS(t *testing.T) {
	store := credentialStore([]Cred{{
		Provider: ProviderGitHub, Host: "github.com", Token: "ghp_x",
	}})
	if !strings.Contains(store, "https://x-access-token:ghp_x@github.com") {
		t.Fatalf("store=%q", store)
	}
}

func TestGuestInstallScriptHasNoRawTokenAndRewritesSSH(t *testing.T) {
	script := guestInstallScript([]Cred{{
		Provider: ProviderGitea, Host: "git.eaxi.com", Token: "rp-secret-pat",
	}})
	if strings.Contains(script, "rp-secret-pat") {
		t.Fatal("raw token must not appear in guest script")
	}
	for _, want := range []string{
		"base64 -d",
		"/home/roundpen/.gitconfig",
		"/home/roundpen/.roundpen/git/config",
		"/home/roundpen/.roundpen/git/credentials",
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("script missing %q:\n%s", want, script)
		}
	}
	if strings.Contains(script, "> /workspace/.roundpen/git/") {
		t.Fatalf("git credentials must not be written under /workspace:\n%s", script)
	}
	if !strings.Contains(script, "rm -rf /workspace/.roundpen/git") {
		t.Fatalf("script must clear legacy workspace git dir:\n%s", script)
	}
}

func TestGuestClearScriptRemovesWorkspaceLegacy(t *testing.T) {
	script := guestClearGitScript()
	for _, want := range []string{"/workspace/.roundpen/git", "/home/roundpen/.roundpen/git", "/home/roundpen/.gitconfig"} {
		if !strings.Contains(script, want) {
			t.Fatalf("clear script missing %q:\n%s", want, script)
		}
	}
}
