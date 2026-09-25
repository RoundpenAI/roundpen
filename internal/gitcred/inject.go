package gitcred

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
)

const (
	guestHomeGitDir     = "/home/roundpen/.roundpen/git"
	guestHomeGitConfig  = "/home/roundpen/.roundpen/git/config"
	guestHomeGitStore   = "/home/roundpen/.roundpen/git/credentials"
	guestHomeGitEnvFile = "/home/roundpen/.roundpen/git/env"

	// GuestInstallScript writes PAT files inside the guest (via sandbox Exec).
	// Files live in the guest's home, never under /workspace: the project tree
	// must not contain credentials.
	GuestHomeGitConfig = guestHomeGitConfig
)

// GuestInstallScript lists the user's stored PATs and renders the install
// script. Callers that also hold tokens from other sources (OAuth identities)
// should list both and use InstallScript directly.
func (s *Store) GuestInstallScript(ctx context.Context, userID string) (string, error) {
	if s == nil {
		return GuestClearGitScript(), nil
	}
	creds, err := s.List(ctx, userID)
	if err != nil {
		return "", err
	}
	return InstallScript(creds), nil
}

// GuestClearGitScript removes every credential file from a guest.
func GuestClearGitScript() string {
	return `set -e
rm -rf /workspace/.roundpen/git /home/roundpen/.roundpen/git 2>/dev/null || true
rm -f /home/roundpen/.gitconfig 2>/dev/null || true
exit 0
`
}

// InstallScript renders the guest-side script that (re)writes every git
// credential file. Pure so callers can merge sources before rendering.
func InstallScript(creds []Cred) string {
	if len(creds) == 0 {
		return GuestClearGitScript()
	}
	cfg := base64.StdEncoding.EncodeToString([]byte(gitConfig(creds)))
	store := base64.StdEncoding.EncodeToString([]byte(credentialStore(creds)))
	envb := base64.StdEncoding.EncodeToString([]byte(envFile(mergeEnv(creds))))
	include := base64.StdEncoding.EncodeToString([]byte("[include]\n\tpath = " + guestHomeGitConfig + "\n"))
	var b strings.Builder
	b.WriteString("set -e\n")
	b.WriteString("mkdir -p " + guestHomeGitDir + "\n")
	fmt.Fprintf(&b, "echo %s | base64 -d > %s\n", cfg, guestHomeGitDir+"/config")
	fmt.Fprintf(&b, "echo %s | base64 -d > %s\n", store, guestHomeGitDir+"/credentials")
	fmt.Fprintf(&b, "echo %s | base64 -d > %s\n", envb, guestHomeGitDir+"/env")
	fmt.Fprintf(&b, "echo %s | base64 -d > /home/roundpen/.gitconfig\n", include)
	fmt.Fprintf(&b, "chmod 700 %s\n", guestHomeGitDir)
	fmt.Fprintf(&b, "chmod 600 %s/config %s/credentials %s/env /home/roundpen/.gitconfig\n", guestHomeGitDir, guestHomeGitDir, guestHomeGitDir)
	fmt.Fprintf(&b, "rm -rf /workspace/.roundpen/git /workspace/.roundpen/ssh 2>/dev/null || true\n")
	return b.String()
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
	b.WriteString("[credential]\n\thelper = store --file /home/roundpen/.roundpen/git/credentials\n")
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
