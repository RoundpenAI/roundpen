package sandbox

import (
	"strings"
)

// Guest home paths match the code-agent image layout and gitcred injection.
// Persistent sandboxes often run as the host workspace owner UID (e.g. fnOS
// package uid 980), which has no /etc/passwd entry in the image — without an
// explicit HOME, git never loads /home/roundpen/.gitconfig.
const (
	GuestHomeDir         = "/home/roundpen"
	GuestGitConfigGlobal = "/home/roundpen/.gitconfig"
)

func hasHomeMount(meta map[string]string) bool {
	if meta == nil {
		return false
	}
	return strings.TrimSpace(meta["homeHost"]) != ""
}

// withGuestHomeEnv copies env and, when apply is true, ensures HOME and
// GIT_CONFIG_GLOBAL point at the guest home mount. Caller keys win.
func withGuestHomeEnv(env map[string]string, apply bool) map[string]string {
	if !apply && env == nil {
		return nil
	}
	out := make(map[string]string, len(env)+2)
	if apply {
		out["HOME"] = GuestHomeDir
		out["GIT_CONFIG_GLOBAL"] = GuestGitConfigGlobal
	}
	for k, v := range env {
		out[k] = v
	}
	return out
}

// withGuestHomeEnvSlice merges guest-home defaults into a KEY=VAL slice.
// Existing entries for the same key win (appear after defaults).
func withGuestHomeEnvSlice(env []string, apply bool) []string {
	if !apply {
		return env
	}
	out := make([]string, 0, len(env)+2)
	out = append(out, "HOME="+GuestHomeDir, "GIT_CONFIG_GLOBAL="+GuestGitConfigGlobal)
	return append(out, env...)
}
