package gitcred

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	guestGitDir     = ".roundpen/git"
	guestGitConfig  = ".roundpen/git/config"
	guestGitStore   = ".roundpen/git/credentials"
	guestGitEnv     = ".roundpen/git/env"
	guestLegacySSH  = ".roundpen/ssh"
	GuestGitConfig  = "/workspace/.roundpen/git/config"
	GuestGitEnvFile = "/workspace/.roundpen/git/env"
)

// Inject writes the user's stored tokens into the Agent workspace.
// It never reads host ~/.ssh. Leftover auto-copied SSH keys are removed.
func (s *Store) Inject(ctx context.Context, userID, workspaceHostPath string) (map[string]string, error) {
	if workspaceHostPath == "" {
		return nil, nil
	}
	_ = os.RemoveAll(filepath.Join(workspaceHostPath, guestLegacySSH))

	if s == nil {
		return nil, nil
	}
	creds, err := s.List(ctx, userID)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(workspaceHostPath, guestGitDir)
	if len(creds) == 0 {
		_ = os.RemoveAll(dir)
		return nil, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(workspaceHostPath, guestGitStore), []byte(credentialStore(creds)), 0o644); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(workspaceHostPath, guestGitConfig), []byte(gitConfig(creds)), 0o644); err != nil {
		return nil, err
	}
	env := mergeEnv(creds)
	if err := os.WriteFile(filepath.Join(workspaceHostPath, guestGitEnv), []byte(envFile(env)), 0o644); err != nil {
		return nil, err
	}
	if err := chmodGuestReadable(workspaceHostPath); err != nil {
		return nil, err
	}
	env["GIT_CONFIG_GLOBAL"] = GuestGitConfig
	env["GIT_CONFIG_NOSYSTEM"] = "1"
	env["GIT_TERMINAL_PROMPT"] = "0"
	return env, nil
}

// GuestInstallScript writes PAT files inside the guest (via sandbox Exec).
// The host never opens workspace internals.
func (s *Store) GuestInstallScript(ctx context.Context, userID string) (string, error) {
	if s == nil {
		return guestClearGitScript(), nil
	}
	creds, err := s.List(ctx, userID)
	if err != nil {
		return "", err
	}
	return guestInstallScript(creds), nil
}

func guestClearGitScript() string {
	return `set -e
rm -rf /workspace/.roundpen/git /home/roundpen/.roundpen/git 2>/dev/null || true
rm -f /home/roundpen/.gitconfig 2>/dev/null || true
exit 0
`
}

func guestInstallScript(creds []Cred) string {
	if len(creds) == 0 {
		return guestClearGitScript()
	}
	cfg := base64.StdEncoding.EncodeToString([]byte(gitConfig(creds)))
	store := base64.StdEncoding.EncodeToString([]byte(credentialStore(creds)))
	envb := base64.StdEncoding.EncodeToString([]byte(envFile(mergeEnv(creds))))
	include := base64.StdEncoding.EncodeToString([]byte("[include]\n\tpath = /workspace/.roundpen/git/config\n"))
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("mkdir -p /workspace/.roundpen/git /home/roundpen/.roundpen\n")
	fmt.Fprintf(&b, "echo %s | base64 -d > /workspace/.roundpen/git/config\n", cfg)
	fmt.Fprintf(&b, "echo %s | base64 -d > /workspace/.roundpen/git/credentials\n", store)
	fmt.Fprintf(&b, "echo %s | base64 -d > /workspace/.roundpen/git/env\n", envb)
	fmt.Fprintf(&b, "echo %s | base64 -d > /home/roundpen/.gitconfig\n", include)
	b.WriteString("chmod 700 /workspace/.roundpen /workspace/.roundpen/git\n")
	b.WriteString("chmod 600 /workspace/.roundpen/git/credentials /workspace/.roundpen/git/config /workspace/.roundpen/git/env /home/roundpen/.gitconfig\n")
	b.WriteString("rm -rf /workspace/.roundpen/ssh 2>/dev/null || true\n")
	return b.String()
}

// ExecEnv is the extra environment for sandbox_exec when git config exists.
func ExecEnv(workspaceHostPath string) map[string]string {
	if workspaceHostPath == "" {
		return nil
	}
	if _, err := os.Stat(filepath.Join(workspaceHostPath, guestGitConfig)); err != nil {
		return nil
	}
	env := map[string]string{
		"GIT_CONFIG_GLOBAL":   GuestGitConfig,
		"GIT_CONFIG_NOSYSTEM": "1",
		"GIT_TERMINAL_PROMPT": "0",
	}
	raw, err := os.ReadFile(filepath.Join(workspaceHostPath, guestGitEnv))
	if err != nil {
		return env
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
		k, v, ok := strings.Cut(line, "=")
		if !ok || k == "" {
			continue
		}
		env[k] = strings.Trim(v, `"'`)
	}
	return env
}

func credentialStore(creds []Cred) string {
	var b strings.Builder
	for _, c := range creds {
		if strings.TrimSpace(c.Token) == "" {
			continue
		}
		fmt.Fprintln(&b, credentialURL(c))
	}
	return b.String()
}

func gitConfig(creds []Cred) string {
	var b strings.Builder
	b.WriteString("[credential]\n\thelper = store --file /workspace/.roundpen/git/credentials\n")
	seen := map[string]bool{}
	for _, c := range creds {
		host := c.Host
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		https := "https://" + host + "/"
		fmt.Fprintf(&b, "[url %q]\n\tinsteadOf = git@%s:\n\tinsteadOf = ssh://git@%s/\n\tinsteadOf = https://%s/\n",
			https, host, host, host)
	}
	return b.String()
}

func mergeEnv(creds []Cred) map[string]string {
	env := map[string]string{}
	for _, c := range creds {
		for k, v := range cliEnv(c) {
			env[k] = v
		}
	}
	return env
}

// chmodGuestReadable makes injected git files readable in Kata/Docker guests.
// Host writes as the control-plane uid; guest root is a different uid, so 0700
// host files are invisible and git falls back to SSH.
func chmodGuestReadable(workspaceHostPath string) error {
	for _, rel := range []string{".roundpen", guestGitDir} {
		if err := os.Chmod(filepath.Join(workspaceHostPath, rel), 0o755); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for _, rel := range []string{guestGitStore, guestGitConfig, guestGitEnv} {
		if err := os.Chmod(filepath.Join(workspaceHostPath, rel), 0o644); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func envFile(env map[string]string) string {
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "export %s=%q\n", k, env[k])
	}
	return b.String()
}
