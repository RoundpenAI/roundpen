package gitcred

import (
	"os"
	"path/filepath"
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
}

func TestCredentialStoreHasHTTPS(t *testing.T) {
	store := credentialStore([]Cred{{
		Provider: ProviderGitHub, Host: "github.com", Token: "ghp_x",
	}})
	if !strings.Contains(store, "https://x-access-token:ghp_x@github.com") {
		t.Fatalf("store=%q", store)
	}
}

func TestInjectWritesWorkspaceNotHostSSH(t *testing.T) {
	ws := t.TempDir()
	legacy := filepath.Join(ws, guestLegacySSH, "id")
	if err := os.MkdirAll(filepath.Dir(legacy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(legacy, []byte("host-private-key"), 0o600); err != nil {
		t.Fatal(err)
	}
	var s *Store
	env, err := s.Inject(t.Context(), "alice", ws)
	if err != nil {
		t.Fatal(err)
	}
	if env != nil {
		t.Fatalf("empty store env=%v", env)
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatal("legacy host ssh key should be removed")
	}
}

func TestExecEnvReadsGitDir(t *testing.T) {
	ws := t.TempDir()
	if env := ExecEnv(ws); env != nil {
		t.Fatalf("empty: %v", env)
	}
	if err := os.MkdirAll(filepath.Join(ws, guestGitDir), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, guestGitConfig), []byte("[credential]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, guestGitEnv), []byte("export GITEA_TOKEN=\"abc\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	env := ExecEnv(ws)
	if env["GIT_CONFIG_GLOBAL"] != GuestGitConfig || env["GITEA_TOKEN"] != "abc" {
		t.Fatalf("%v", env)
	}
	if env["GIT_TERMINAL_PROMPT"] != "0" {
		t.Fatalf("prompt: %v", env)
	}
}

func TestGuestInstallScriptHasNoRawTokenAndRewritesSSH(t *testing.T) {
	script := guestInstallScript([]Cred{{
		Provider: ProviderGitea, Host: "git.eaxi.com", Token: "rp-secret-pat",
	}})
	if strings.Contains(script, "rp-secret-pat") {
		t.Fatal("raw token must not appear in guest script")
	}
	if !strings.Contains(script, "base64 -d") || !strings.Contains(script, "/home/roundpen/.gitconfig") {
		t.Fatalf("script:\n%s", script)
	}
}

func TestChmodGuestReadable(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, guestGitDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(filepath.Join(ws, ".roundpen"), 0o700)
	if err := os.WriteFile(filepath.Join(ws, guestGitConfig), []byte("[credential]\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := chmodGuestReadable(ws); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(ws, ".roundpen"))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o755 {
		t.Fatalf("dir mode %o", st.Mode().Perm())
	}
	st, err = os.Stat(filepath.Join(ws, guestGitConfig))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o644 {
		t.Fatalf("file mode %o", st.Mode().Perm())
	}
}
