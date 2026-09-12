// Package agentenv provisions sandboxes with platform credentials pre-injected.
package agentenv

import (
	"context"
	"fmt"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/workspace"
)

// Config controls injection values.
type Config struct {
	PublicURL    string // e.g. http://127.0.0.1:19001
	APIKey       string
	VirtualKey   string // llmgw vkey (not upstream)
	DefaultModel string // llmgw fallback model injected as ANTHROPIC_MODEL etc.
	TemplateID   string
	Category     string
	NamePrefix   string
}

// Provisioner creates sandboxes ready for agents.
type Provisioner struct {
	Sandboxes sandbox.Manager
	Config    Config
}

// Result is a provisioned sandbox plus the env map applied.
type Result struct {
	Sandbox *sandbox.Sandbox
	Env     map[string]string
}

// Provision creates a sandbox with injected env for the given agent session.
func (p *Provisioner) Provision(ctx context.Context, sessionID, agentID, userName string) (*Result, error) {
	if p.Sandboxes == nil {
		return nil, fmt.Errorf("sandboxes manager required")
	}
	base := strings.TrimRight(p.Config.PublicURL, "/")
	if base == "" {
		base = "http://127.0.0.1:19001"
	}
	tpl := p.Config.TemplateID
	if tpl == "" {
		tpl = "code-agent"
	}
	cat := p.Config.Category
	if cat == "" {
		cat = "Agent"
	}
	name := p.Config.NamePrefix
	if name == "" {
		name = "agent"
	}
	name = fmt.Sprintf("%s-%s", name, shortID(sessionID))

	// Sandbox id is assigned on create; first pass without id-dependent URLs,
	// then we patch env via a second write of .roundpen/env after create.
	env := map[string]string{
		"ROUNDPEN_URL":         base,
		"ROUNDPEN_API_KEY":     p.Config.APIKey,
		"ROUNDPEN_AGENT_ID":    agentID,
		"ROUNDPEN_SESSION_ID":  sessionID,
		"ROUNDPEN_MEMORY_URL":  base + "/v1",
		"OPENAI_BASE_URL":      base + "/llmgw/openai",
		"ANTHROPIC_BASE_URL":   base + "/llmgw/anthropic",
		"OPENAI_API_KEY":       p.Config.VirtualKey,
		"ANTHROPIC_API_KEY":    p.Config.VirtualKey,
		"ANTHROPIC_AUTH_TOKEN": p.Config.VirtualKey,
		"ROUNDPEN_USER":        userName,
	}
	ApplyDefaultModel(env, p.Config.DefaultModel)

	sb, err := p.Sandboxes.Create(ctx, sandbox.CreateRequest{
		Name:        name,
		Category:    cat,
		TemplateID:  tpl,
		WorkspaceID: workspace.UserWorkspaceID(userName),
		Env:         env,
		Metadata: map[string]string{
			"roundpen.agent_session": sessionID,
			"roundpen.agent_id":      agentID,
			"roundpen.owner":         userName,
		},
	})
	if err != nil {
		return nil, err
	}

	env["ROUNDPEN_SANDBOX_ID"] = sb.ID
	env["ROUNDPEN_BROWSER_MCP_URL"] = fmt.Sprintf("%s/v1/sandboxes/%s/browser/mcp", base, sb.ID)

	// Persist env file for agents that source it.
	var b strings.Builder
	for k, v := range env {
		fmt.Fprintf(&b, "export %s=%q\n", k, v)
	}
	_ = p.Sandboxes.WriteFile(ctx, sb.ID, ".roundpen/env", strings.NewReader(b.String()))

	return &Result{Sandbox: sb, Env: env}, nil
}

// ApplyDefaultModel pins Claude Code / Anthropic clients to the llmgw default
// so they do not pick a built-in model the upstream does not serve.
func ApplyDefaultModel(env map[string]string, model string) {
	model = strings.TrimSpace(model)
	if model == "" || env == nil {
		return
	}
	for _, k := range []string{
		"ANTHROPIC_MODEL",
		"ANTHROPIC_DEFAULT_OPUS_MODEL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL",
		"ANTHROPIC_SMALL_FAST_MODEL",
		"CLAUDE_CODE_SUBAGENT_MODEL",
	} {
		if strings.TrimSpace(env[k]) == "" {
			env[k] = model
		}
	}
}

func shortID(id string) string {
	id = strings.ReplaceAll(id, "-", "")
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
