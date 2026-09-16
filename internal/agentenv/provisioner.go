// Package agentenv provisions sandboxes with platform credentials pre-injected.
package agentenv

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
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
	ModelSource  string // storage.ModelSource*; "own" withholds the llmgw env
	ProxyURL     string // optional egress proxy for in-sandbox tools
}

// ProxyEnv returns proxy env vars (upper and lower case) for tools running in
// a sandbox. noProxyHosts are appended to the loopback defaults, so the
// control-plane host must be passed to keep it off the proxy. Nil when no
// proxy is configured.
func ProxyEnv(proxyURL string, noProxyHosts ...string) map[string]string {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return nil
	}
	entries := append([]string{"localhost", "127.0.0.1", "::1"}, noProxyHosts...)
	noProxy := strings.Join(entries, ",")
	return map[string]string{
		"HTTP_PROXY": proxyURL, "HTTPS_PROXY": proxyURL, "ALL_PROXY": proxyURL, "NO_PROXY": noProxy,
		"http_proxy": proxyURL, "https_proxy": proxyURL, "all_proxy": proxyURL, "no_proxy": noProxy,
	}
}

// HostOf returns the hostname (no port) of an absolute URL, or "".
func HostOf(rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// UsesOwnModels reports whether the model source means the sandbox keeps the
// user's own vendor login instead of the platform gateway env.
func UsesOwnModels(modelSource string) bool {
	return strings.EqualFold(strings.TrimSpace(modelSource), storage.ModelSourceOwn)
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
	// then we patch env via a second exec writing ~/.roundpen/env after create.
	env := map[string]string{
		"ROUNDPEN_URL":        base,
		"ROUNDPEN_API_KEY":    p.Config.APIKey,
		"ROUNDPEN_AGENT_ID":   agentID,
		"ROUNDPEN_SESSION_ID": sessionID,
		"ROUNDPEN_MEMORY_URL": base + "/v1",
		"ROUNDPEN_USER":       userName,
	}
	// ANTHROPIC_API_KEY in particular must stay unset in "own" mode: its
	// presence makes Claude Code bill the API instead of the user's plan.
	if !UsesOwnModels(p.Config.ModelSource) {
		env["OPENAI_BASE_URL"] = base + "/llmgw/openai"
		env["ANTHROPIC_BASE_URL"] = base + "/llmgw/anthropic"
		env["OPENAI_API_KEY"] = p.Config.VirtualKey
		env["ANTHROPIC_API_KEY"] = p.Config.VirtualKey
		env["ANTHROPIC_AUTH_TOKEN"] = p.Config.VirtualKey
		ApplyDefaultModel(env, p.Config.DefaultModel)
	}
	for k, v := range ProxyEnv(p.Config.ProxyURL, HostOf(base)) {
		env[k] = v
	}

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

	// Persist the env file for agents that source it. It lives in the guest's
	// home (~/.roundpen/env), NOT under /workspace: the agent's project tree
	// must never contain its own credentials.
	var b strings.Builder
	for k, v := range env {
		fmt.Fprintf(&b, "export %s=%q\n", k, v)
	}
	payload := base64.StdEncoding.EncodeToString([]byte(b.String()))
	const writeEnvScript = `D="$HOME/.roundpen"; mkdir -p "$D"; printf %s "$1" | base64 -d > "$D/env"; chmod 600 "$D/env"`
	_, err = p.Sandboxes.Exec(ctx, sb.ID, sandbox.ExecRequest{
		Cmd: []string{"/bin/sh", "-c", writeEnvScript, "roundpen-write-env", payload},
	})
	if err != nil {
		// Non-fatal: env is also injected via CreateRequest.Env.
		return &Result{Sandbox: sb, Env: env}, nil
	}

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
