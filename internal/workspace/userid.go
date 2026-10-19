package workspace

import "strings"

// UserWorkspaceID is the stable workspace id for a login.
// Host path is {dataRoot}/sandboxes/user-{name}/workspace/.
func UserWorkspaceID(username string) string {
	return "user-" + sanitizeWorkspaceUser(username)
}

// AgentSandboxID is the stable sandbox id for the user's interactive Agent slot.
// Docker container name is "roundpen-" + AgentSandboxID (see docker.containerName).
func AgentSandboxID(username string) string {
	return sanitizeWorkspaceUser(username)
}

// AgentJobSandboxID is the stable sandbox id for long-running job work
// (routines / project workers). Shares the user's workspace mount with the
// interactive agent slot but uses a separate container so chat is not blocked.
func AgentJobSandboxID(username string) string {
	return sanitizeWorkspaceUser(username) + "-job"
}

func sanitizeWorkspaceUser(u string) string {
	u = strings.ToLower(strings.TrimSpace(u))
	var b strings.Builder
	for _, r := range u {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	if len(out) > 40 {
		out = out[:40]
	}
	if out == "" {
		return "user"
	}
	return out
}
