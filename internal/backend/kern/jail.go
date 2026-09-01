package kern

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Guest paths inside the kern jail (bubblewrap mount namespace).
const (
	guestWorkspace = "/workspace"
	guestHome      = "/home"
)

func homeBesideWorkspace(workspaceMount string) string {
	if filepath.Base(workspaceMount) == "workspace" {
		return filepath.Join(filepath.Dir(workspaceMount), "home")
	}
	// Legacy workspaces/{id}
	return workspaceMount + ".home"
}

func ensureSandboxDirs(workspaceMount string) (home string, err error) {
	home = homeBesideWorkspace(workspaceMount)
	if err = os.MkdirAll(workspaceMount, 0o755); err != nil {
		return "", err
	}
	if err = os.MkdirAll(home, 0o755); err != nil {
		return "", err
	}
	return home, nil
}

// guestWorkDir maps a host workdir under workspaceMount to a guest path.
func guestWorkDir(workspaceMount, hostWorkDir string) (string, error) {
	if hostWorkDir == "" || hostWorkDir == workspaceMount {
		return guestWorkspace, nil
	}
	rel, err := filepath.Rel(workspaceMount, hostWorkDir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("workdir escapes workspace")
	}
	if rel == "." {
		return guestWorkspace, nil
	}
	return guestWorkspace + "/" + filepath.ToSlash(rel), nil
}

func lookBwrap() (string, error) {
	return exec.LookPath("bwrap")
}

func buildJailEnv(base, extra map[string]string, guestPWD, hostname string) []string {
	if hostname == "" {
		hostname = "roundpen"
	}
	merged := map[string]string{
		"PATH":     "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME":     guestHome,
		"PWD":      guestPWD,
		"SHELL":    "/bin/bash",
		"TERM":     "xterm-256color",
		"HOSTNAME": hostname,
		// Explicit prompt so the shell shows the jail name even if UTS hostname
		// cannot be changed in restricted environments.
		"PS1": hostname + `:\w\$ `,
	}
	if _, err := os.Stat("/bin/bash"); err != nil {
		merged["SHELL"] = "/bin/sh"
	}
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range extra {
		merged[k] = v
	}
	merged["HOME"] = guestHome
	merged["PWD"] = guestPWD
	merged["HOSTNAME"] = hostname
	merged["PS1"] = hostname + `:\w\$ `
	out := make([]string, 0, len(merged))
	for k, v := range merged {
		out = append(out, k+"="+v)
	}
	return out
}

func guestHostname(sandboxID, name string) string {
	if slug := hostnameSlug(name); slug != "" {
		return slug
	}
	id := strings.ReplaceAll(sandboxID, "-", "")
	if len(id) > 8 {
		id = id[:8]
	}
	if id == "" {
		return "roundpen"
	}
	return "rp-" + id
}

func hostnameSlug(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ':
			if b.Len() > 0 && !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
		if b.Len() >= 24 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" || out == "localhost" {
		return ""
	}
	return out
}

// wrapBwrap rewrites cmd to run under bubblewrap with /workspace and /home
// bind-mounted from the host sandbox dirs. Host `/` is not visible as-is:
// only explicitly bound paths appear under guest `/`.
func wrapBwrap(hostname, workspaceMount, homeMount, guestPWD string, cmd []string, env []string) (path string, args []string, err error) {
	bwrap, err := lookBwrap()
	if err != nil {
		return "", nil, fmt.Errorf("kern jail requires bubblewrap (bwrap): %w", err)
	}
	if len(cmd) == 0 {
		return "", nil, fmt.Errorf("empty command")
	}
	if guestPWD == "" {
		guestPWD = guestWorkspace
	}
	if hostname == "" {
		hostname = "roundpen"
	}

	base := []string{
		"--die-with-parent",
		"--dir", "/",
		"--dir", "/tmp",
		"--proc", "/proc",
		"--dev", "/dev",
		"--bind", workspaceMount, guestWorkspace,
		"--bind", homeMount, guestHome,
	}
	for _, p := range []string{"/usr", "/bin", "/sbin", "/lib", "/lib64", "/lib32", "/etc"} {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			base = append(base, "--ro-bind", p, p)
		}
	}
	if fi, err := os.Stat("/run"); err == nil && fi.IsDir() {
		base = append(base, "--ro-bind", "/run", "/run")
	}

	finish := func(prefix []string) []string {
		out := append([]string{}, prefix...)
		out = append(out, "--chdir", guestPWD, "--clearenv")
		for _, e := range env {
			k, v, ok := strings.Cut(e, "=")
			if !ok || k == "" {
				continue
			}
			out = append(out, "--setenv", k, v)
		}
		out = append(out, "--")
		out = append(out, cmd...)
		return out
	}

	withHost := append([]string{"--die-with-parent", "--unshare-uts", "--hostname", hostname}, base[1:]...)
	if bwrapProbe(bwrap, withHost) {
		return bwrap, finish(withHost), nil
	}
	// Restricted hosts may forbid UTS namespaces; PS1/HOSTNAME still show the jail name.
	return bwrap, finish(base), nil
}

func bwrapProbe(bwrap string, mid []string) bool {
	args := append([]string{}, mid...)
	args = append(args, "--", "true")
	return exec.Command(bwrap, args...).Run() == nil
}
