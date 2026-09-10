// Package providers catalogs ACP agent launch configurations.
package providers

// Provider describes how to start an ACP agent.
type Provider struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Enabled     bool     `json:"enabled"`
	Mode        string   `json:"mode"` // "sysadmin" | "stdio"
	Command     string   `json:"command,omitempty"`
	Args        []string `json:"args,omitempty"`
	TemplateID  string   `json:"templateId,omitempty"`
}

// Default returns built-in providers.
func Default() []Provider {
	return []Provider{
		{
			ID:          "sysadmin",
			Name:        "System Agent",
			Description: "In-process control-plane agent: manage sandboxes/settings via Roundpen API + browser tools (user permissions).",
			Enabled:     true,
			Mode:        "sysadmin",
		},
		{
			ID:          "claude",
			Name:        "Claude Code",
			Description: "Claude Code ACP in the agent-claude QEMU VM (LLM via llmgw).",
			Enabled:     true,
			Mode:        "stdio",
			Command:     "claude-agent-acp",
			TemplateID:  "agent-claude",
		},
		{
			ID:          "stdio",
			Name:        "Custom stdio ACP",
			Description: "Run a sandbox command with --acp over AttachExec.",
			Enabled:     true,
			Mode:        "stdio",
			Command:     "sh",
			Args:        []string{"-c", "echo 'configure ROUNDPEN_ACP_CMD'; exit 1"},
			TemplateID:  "agent-claude",
		},
	}
}

// NeedsSandbox reports whether the provider requires a provisioned sandbox.
func NeedsSandbox(p Provider) bool {
	switch p.Mode {
	case "sysadmin", "mock": // mock kept for any leftover rows
		return false
	default:
		return true
	}
}

// ByID finds a provider.
func ByID(list []Provider, id string) (Provider, bool) {
	for _, p := range list {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}
