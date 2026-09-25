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
	script := InstallScript([]Cred{{
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
	script := GuestClearGitScript()
	for _, want := range []string{"/workspace/.roundpen/git", "/home/roundpen/.roundpen/git", "/home/roundpen/.gitconfig"} {
		if !strings.Contains(script, want) {
			t.Fatalf("clear script missing %q:\n%s", want, script)
		}
	}
}

func TestTokenCredUsesProviderDefaults(t *testing.T) {
	gitea := TokenCred("gitea", "Git.Eaxi.com/", "tok")
	if gitea.Provider != ProviderGitea || gitea.Host != "git.eaxi.com" || gitea.Token != "tok" {
		t.Errorf("token cred = %+v", gitea)
	}
	if got := credentialURL(gitea); !strings.Contains(got, "git:tok@git.eaxi.com") {
		t.Errorf("gitea credential url = %q", got)
	}
	if env := cliEnv(gitea); env["GITEA_TOKEN"] != "tok" || env["TEA_TOKEN"] != "tok" {
		t.Errorf("gitea env = %v", env)
	}

	gh := TokenCred("gh", "github.com", "tok")
	if gh.Provider != ProviderGitHub {
		t.Errorf("github provider = %q", gh.Provider)
	}
	if got := credentialURL(gh); !strings.Contains(got, "x-access-token:tok@github.com") {
		t.Errorf("github credential url = %q", got)
	}
}

func TestMergeCredsPrimaryWinsPerHost(t *testing.T) {
	pat := Cred{Provider: ProviderGitea, Host: "git.eaxi.com", Token: "pat"}
	oauthSameHost := Cred{Provider: ProviderGitea, Host: "Git.Eaxi.COM", Token: "oauth"}
	oauthOther := Cred{Provider: ProviderGitHub, Host: "github.com", Token: "oauth-gh"}
	empty := Cred{Provider: ProviderGitea, Host: "empty.test"}

	got := MergeCreds([]Cred{pat}, []Cred{oauthSameHost, oauthOther, empty})
	if len(got) != 2 {
		t.Fatalf("merged = %+v", got)
	}
	byHost := map[string]string{}
	for _, c := range got {
		byHost[c.Host] = c.Token
	}
	if byHost["git.eaxi.com"] != "pat" {
		t.Errorf("the manual PAT should win: %+v", got)
	}
	if byHost["github.com"] != "oauth-gh" {
		t.Errorf("fallback for another host should survive: %+v", got)
	}
	if got[0].Host > got[1].Host {
		t.Errorf("output should be sorted by host: %+v", got)
	}
}
