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
	guestHomeGNUPG      = "/home/roundpen/.roundpen/gnupg"

	// GuestHomeGitConfig is the path included from ~/.gitconfig.
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
	return InstallScript(creds, Identity{}), nil
}

// GuestClearGitScript removes every credential file from a guest.
func GuestClearGitScript() string {
	return `set -e
rm -rf /workspace/.roundpen/git /home/roundpen/.roundpen/git /home/roundpen/.roundpen/gnupg 2>/dev/null || true
rm -f /home/roundpen/.gitconfig 2>/dev/null || true
exit 0
`
}

// InstallScript renders the guest-side script that (re)writes git credential
// files, commit authorship, and optional GPG signing material.
func InstallScript(creds []Cred, id Identity) string {
	if len(creds) == 0 && id.Empty() {
		return GuestClearGitScript()
	}
	cfg := base64.StdEncoding.EncodeToString([]byte(gitConfig(creds, id)))
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
	b.WriteString(gpgInstallScript(id))
	fmt.Fprintf(&b, "rm -rf /workspace/.roundpen/git /workspace/.roundpen/ssh 2>/dev/null || true\n")
	return b.String()
}

func gpgInstallScript(id Identity) string {
	key := strings.TrimSpace(id.GPGPrivateKey)
	if key == "" || !id.SignCommits {
		return "rm -rf " + guestHomeGNUPG + " 2>/dev/null || true\n"
	}
	keyB64 := base64.StdEncoding.EncodeToString([]byte(key))
	keyID := strings.TrimSpace(id.GPGKeyID)
	var b strings.Builder
	fmt.Fprintf(&b, "rm -rf %s\n", guestHomeGNUPG)
	fmt.Fprintf(&b, "mkdir -p %s\n", guestHomeGNUPG)
	fmt.Fprintf(&b, "chmod 700 %s\n", guestHomeGNUPG)
	fmt.Fprintf(&b, "export GNUPGHOME=%s\n", guestHomeGNUPG)
	b.WriteString("printf 'allow-loopback-pinentry\\n' > \"$GNUPGHOME/gpg-agent.conf\"\n")
	b.WriteString("printf 'pinentry-mode loopback\\n' > \"$GNUPGHOME/gpg.conf\"\n")
	fmt.Fprintf(&b, "echo %s | base64 -d > \"$GNUPGHOME/key.asc\"\n", keyB64)
	b.WriteString("chmod 600 \"$GNUPGHOME/key.asc\" \"$GNUPGHOME/gpg.conf\" \"$GNUPGHOME/gpg-agent.conf\"\n")
	b.WriteString("if command -v gpg >/dev/null 2>&1; then\n")
	b.WriteString("  gpg --batch --yes --import \"$GNUPGHOME/key.asc\" >/dev/null 2>&1 || true\n")
	if keyID != "" {
		fmt.Fprintf(&b, "  KEYID=%q\n", keyID)
	} else {
		b.WriteString("  KEYID=$(gpg --batch --list-secret-keys --with-colons 2>/dev/null | awk -F: '/^fpr:/{print $10; exit}')\n")
	}
	b.WriteString("  if [ -n \"$KEYID\" ]; then\n")
	b.WriteString("    git config --file " + guestHomeGitConfig + " user.signingkey \"$KEYID\"\n")
	b.WriteString("    git config --file " + guestHomeGitConfig + " commit.gpgsign true\n")
	b.WriteString("    git config --file " + guestHomeGitConfig + " gpg.program gpg\n")
	b.WriteString("  fi\n")
	b.WriteString("fi\n")
	b.WriteString("rm -f \"$GNUPGHOME/key.asc\"\n")
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

func gitConfig(creds []Cred, id Identity) string {
	var b strings.Builder
	b.WriteString("[credential]\n\thelper = store --file /home/roundpen/.roundpen/git/credentials\n")
	if name := strings.TrimSpace(id.Name); name != "" {
		fmt.Fprintf(&b, "[user]\n\tname = %s\n", gitConfigValue(name))
		if email := strings.TrimSpace(id.Email); email != "" {
			fmt.Fprintf(&b, "\temail = %s\n", gitConfigValue(email))
		}
	} else if email := strings.TrimSpace(id.Email); email != "" {
		fmt.Fprintf(&b, "[user]\n\temail = %s\n", gitConfigValue(email))
	}
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

// gitConfigValue escapes a value for a git-config line (no newlines).
func gitConfigValue(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return s
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
