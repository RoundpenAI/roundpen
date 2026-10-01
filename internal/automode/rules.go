// Package automode implements the policy classifier behind the chat Auto
// toggle: safe tool calls run automatically, destructive, irreversible or
// exfiltrating ones are blocked before execution.
package automode

import "strings"

// Rules is the admin-configured rule set, modeled on Claude Code's autoMode
// block. Entries are prose read by the classifier, not patterns.
type Rules struct {
	Environment []string
	Allow       []string
	SoftDeny    []string
	HardDeny    []string
}

// DefaultsToken splices the built-in rules into a list at its position.
// A non-empty list without the token replaces the built-in list entirely.
const DefaultsToken = "$defaults"

// Defaults returns the built-in rules shipped with Roundpen.
func Defaults() Rules {
	return Rules{
		Environment: []string{
			"Trusted environment: the agent's sandbox workspace at /workspace and everything inside it. File edits, dependency installs, builds, tests and routine development commands there are normal work.",
			"Outside the environment: the host machine running Roundpen, the Roundpen control plane and its API, other users' sandboxes and the local network. Nothing the agent runs may target them.",
			"The agent's browser operates the user's own browsing session on the host; treat browsing as acting on the user's machine.",
		},
		Allow: []string{
			"Creating, editing and deleting files inside the workspace /workspace.",
			"Routine development commands in the sandbox: dependency installs, builds, tests, linters, formatters, and git commands that only inspect or commit to the current branch.",
			"Recording issues, plan documents and tasks in the user's own issue tracker (CreateIssue, UpdateIssue, WriteIssueDoc, CreateTask, UpdateTask). This writes only the user's own tracker rows and grants no new capability.",
			"Creating and updating the user's own standing routines (CreateRoutine, UpdateRoutine, UpdateRoutineState). This writes only the user's routine rows and grants no new capability.",
			"Fetching public documentation, package registries and ordinary public websites over HTTPS.",
		},
		SoftDeny: []string{
			"Deleting or overwriting files outside /workspace, or recursive deletes (rm -rf) whose target is not clearly inside the workspace.",
			"Force-pushing, deleting remote branches or tags, or rewriting remote git history.",
			"Sending workspace code, file contents or user data to unfamiliar endpoints, paste sites or third-party services.",
			"System-level changes: installing OS packages or services, modifying system configuration, or changing user accounts and permissions.",
			"Changing credentials, API keys or secrets management.",
		},
		HardDeny: []string{
			"Exfiltrating secrets: reading credentials (.env, SSH keys, tokens, cloud credentials) and sending them anywhere outside the workspace.",
			"Reaching cloud metadata endpoints (169.254.169.254, metadata.google.internal) or loopback, link-local, private-network or control-plane addresses, including via the browser.",
			"Piping remote content directly into a shell or interpreter (curl ... | bash and equivalents).",
			"Disabling, bypassing or modifying Roundpen's permission handling, auto mode, auditing or sandbox guardrails.",
			"Destructive infrastructure actions: deploying to production, dropping or deleting databases, revoking credentials, or deleting cloud resources.",
		},
	}
}

// Expand replaces each DefaultsToken entry with the built-in rules for that
// list. An empty list falls back to the built-in rules; a non-empty list
// without the token replaces them.
func Expand(cfg Rules) Rules {
	def := Defaults()
	return Rules{
		Environment: expandList(cfg.Environment, def.Environment),
		Allow:       expandList(cfg.Allow, def.Allow),
		SoftDeny:    expandList(cfg.SoftDeny, def.SoftDeny),
		HardDeny:    expandList(cfg.HardDeny, def.HardDeny),
	}
}

func expandList(list, def []string) []string {
	if len(list) == 0 {
		return def
	}
	out := make([]string, 0, len(list)+len(def))
	for _, e := range list {
		if strings.TrimSpace(e) == DefaultsToken {
			out = append(out, def...)
			continue
		}
		out = append(out, e)
	}
	return out
}
