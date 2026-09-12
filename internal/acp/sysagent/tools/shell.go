package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RoundpenAI/roundpen/internal/gitcred"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

const (
	defaultExecTimeout = 3 * time.Minute
	maxExecOutput      = 32 << 10
)

// AgentSlot starts the user's Cloud Agent environment.
type AgentSlot interface {
	EnsureAgent(ctx context.Context, userID string) (*sandbox.Sandbox, error)
}

// SandboxExec runs a command inside the agent workspace environment.
type SandboxExec interface {
	Exec(ctx context.Context, id string, req sandbox.ExecRequest) (*sandbox.ExecResult, error)
	WorkspaceHostPath(ctx context.Context, id string) (string, error)
}

// WorkspaceFiles reads and writes files in the agent workspace.
type WorkspaceFiles interface {
	ReadFile(ctx context.Context, id, relPath string) (io.ReadCloser, error)
	WriteFile(ctx context.Context, id, relPath string, r io.Reader) error
}

// AgentBinder runs shell and file tools in the user's Agent workspace.
type AgentBinder struct {
	Slots AgentSlot
	Exec  SandboxExec
	Files WorkspaceFiles
}

func (b *AgentBinder) ensure(ctx context.Context, actor Actor) (*sandbox.Sandbox, error) {
	if b == nil || b.Slots == nil {
		return nil, fmt.Errorf("agent slot not configured")
	}
	if strings.TrimSpace(actor.Username) == "" {
		return nil, fmt.Errorf("username is required")
	}
	sb, err := b.Slots.EnsureAgent(ctx, actor.Username)
	if err != nil {
		return nil, fmt.Errorf("agent workspace is not available: %w", err)
	}
	if sb == nil || sb.ID == "" {
		return nil, fmt.Errorf("agent workspace is not available")
	}
	return sb, nil
}

func (b *AgentBinder) ensureID(ctx context.Context, actor Actor) (string, error) {
	sb, err := b.ensure(ctx, actor)
	if err != nil {
		return "", err
	}
	return sb.ID, nil
}

func (b *AgentBinder) exec(ctx context.Context, sbID string, cmd []string, workDir string, timeout time.Duration) (string, error) {
	res, err := b.execResult(ctx, sbID, cmd, workDir, timeout)
	if err != nil {
		return "", err
	}
	out := formatExecResult(res)
	if res.ExitCode != 0 {
		return out, fmt.Errorf("exit %d", res.ExitCode)
	}
	return out, nil
}

func (b *AgentBinder) execResult(ctx context.Context, sbID string, cmd []string, workDir string, timeout time.Duration) (*sandbox.ExecResult, error) {
	if b == nil || b.Exec == nil {
		return nil, fmt.Errorf("workspace exec not configured")
	}
	if timeout <= 0 {
		timeout = defaultExecTimeout
	}
	if workDir == "" {
		workDir = WorkspaceRoot
	}
	env := map[string]string{
		"GIT_TERMINAL_PROMPT": "0",
	}
	if root, err := b.Exec.WorkspaceHostPath(ctx, sbID); err == nil {
		for k, v := range gitcred.ExecEnv(root) {
			env[k] = v
		}
	}
	return b.Exec.Exec(ctx, sbID, sandbox.ExecRequest{
		Cmd:     cmd,
		WorkDir: workDir,
		Env:     env,
		Timeout: timeout,
	})
}

func formatExecResult(res *sandbox.ExecResult) string {
	if res == nil {
		return `{"exitCode":-1}`
	}
	payload := map[string]any{
		"exitCode": res.ExitCode,
		"stdout":   truncateRunes(string(res.Stdout), maxExecOutput),
		"stderr":   truncateRunes(string(res.Stderr), maxExecOutput),
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`{"exitCode":%d}`, res.ExitCode)
	}
	return string(raw)
}

func truncateRunes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	for max > 0 && !utf8.ValidString(s[:max]) {
		max--
	}
	return s[:max] + "…"
}

// RegisterShell adds the Bash tool (implicit agent-workspace ensure).
func RegisterShell(r *Registry, binder *AgentBinder) {
	if r == nil || binder == nil {
		return
	}
	r.Register(Tool{
		Name: "Bash",
		Description: "Run a shell command in the Agent workspace (default cwd /workspace). " +
			"Use for git, builds, tests, and other command-line work. " +
			"The Browser cannot run shell commands.",
		Mutating: true,
		Parameters: objectSchema(map[string]any{
			"command": map[string]any{"type": "string", "description": "Shell command (passed to /bin/sh -c)"},
			"workdir": map[string]any{"type": "string", "description": "Working directory (default /workspace)"},
			"timeout": map[string]any{"type": "integer", "description": "Timeout in seconds (default 180)"},
		}, "command"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				Command string `json:"command"`
				WorkDir string `json:"workdir"`
				Timeout int    `json:"timeout"`
			}
			if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Command) == "" {
				return "", fmt.Errorf("command is required")
			}
			id, err := binder.ensureID(ctx, actor)
			if err != nil {
				return "", err
			}
			timeout := defaultExecTimeout
			if in.Timeout > 0 {
				timeout = time.Duration(in.Timeout) * time.Second
			}
			out, err := binder.exec(ctx, id, []string{"/bin/sh", "-c", in.Command}, strings.TrimSpace(in.WorkDir), timeout)
			if err != nil {
				if out != "" {
					return out, err
				}
				return "", err
			}
			return out, nil
		},
	})
}
