package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"strings"
)

// RegisterSearch adds Glob and Grep tools (guest exec with implicit ensure).
func RegisterSearch(r *Registry, binder *AgentBinder) {
	if r == nil || binder == nil {
		return
	}
	r.Register(Tool{
		Name:        "Glob",
		Description: "Find files by glob pattern in the Agent workspace (default root /workspace).",
		Parameters: objectSchema(map[string]any{
			"pattern": map[string]any{"type": "string", "description": "Glob pattern (e.g. **/*.ts)"},
			"path":    map[string]any{"type": "string", "description": "Search root (default /workspace)"},
		}, "pattern"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				Pattern string `json:"pattern"`
				Path    string `json:"path"`
			}
			if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Pattern) == "" {
				return "", fmt.Errorf("pattern is required")
			}
			return binder.glob(ctx, actor, in.Pattern, in.Path)
		},
	})
	r.Register(Tool{
		Name:        "Grep",
		Description: "Search file contents with ripgrep in the Agent workspace.",
		Parameters: objectSchema(map[string]any{
			"pattern":          map[string]any{"type": "string", "description": "Regex pattern to search for"},
			"path":             map[string]any{"type": "string", "description": "Search root (default /workspace)"},
			"glob":             map[string]any{"type": "string", "description": "Optional file glob filter"},
			"case_insensitive": map[string]any{"type": "boolean", "description": "Case-insensitive search"},
			"output_mode":      map[string]any{"type": "string", "description": "content | files_with_matches | count (default content)"},
		}, "pattern"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				Pattern         string `json:"pattern"`
				Path            string `json:"path"`
				Glob            string `json:"glob"`
				CaseInsensitive bool   `json:"case_insensitive"`
				OutputMode      string `json:"output_mode"`
			}
			if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Pattern) == "" {
				return "", fmt.Errorf("pattern is required")
			}
			return binder.grep(ctx, actor, in.Pattern, in.Path, in.Glob, in.CaseInsensitive, in.OutputMode)
		},
	})
}

func resolveGuestAbs(p string) (string, error) {
	if strings.TrimSpace(p) == "" {
		return WorkspaceRoot, nil
	}
	rel, err := ResolveWorkspacePath(p)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return WorkspaceRoot, nil
	}
	return path.Join(WorkspaceRoot, rel), nil
}

func (b *AgentBinder) glob(ctx context.Context, actor Actor, pattern, root string) (string, error) {
	abs, err := resolveGuestAbs(root)
	if err != nil {
		return "", err
	}
	id, err := b.ensureID(ctx, actor)
	if err != nil {
		return "", err
	}
	script := `PATTERN="$1"; ROOT="$2"
if command -v rg >/dev/null 2>&1; then
  rg --files -g "$PATTERN" -- "$ROOT" 2>/dev/null
else
  find "$ROOT" -type f 2>/dev/null
fi`
	res, err := b.execResult(ctx, id, []string{"/bin/sh", "-c", script, "glob", pattern, abs}, WorkspaceRoot, defaultExecTimeout)
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(string(res.Stdout))
	if text == "" {
		return "No files matched.", nil
	}
	return truncateRunes(text, maxExecOutput), nil
}

func (b *AgentBinder) grep(ctx context.Context, actor Actor, pattern, root, fileGlob string, caseInsensitive bool, outputMode string) (string, error) {
	abs, err := resolveGuestAbs(root)
	if err != nil {
		return "", err
	}
	id, err := b.ensureID(ctx, actor)
	if err != nil {
		return "", err
	}
	mode := strings.TrimSpace(outputMode)
	if mode == "" {
		mode = "content"
	}
	script := `PATTERN="$1"; ROOT="$2"; GLOB="$3"; MODE="$4"; CI="$5"
if ! command -v rg >/dev/null 2>&1; then
  echo "rg is not available in the Agent workspace" >&2
  exit 127
fi
args=()
case "$MODE" in
  files_with_matches) args+=(-l) ;;
  count) args+=(-c) ;;
  *) args+=(-n) ;;
esac
if [ "$CI" = "1" ]; then args+=(-i); fi
if [ -n "$GLOB" ]; then args+=(-g "$GLOB"); fi
rg "${args[@]}" -- "$PATTERN" "$ROOT"
`
	ci := "0"
	if caseInsensitive {
		ci = "1"
	}
	res, err := b.execResult(ctx, id, []string{"/bin/sh", "-c", script, "grep", pattern, abs, fileGlob, mode, ci}, WorkspaceRoot, defaultExecTimeout)
	if err != nil {
		return "", err
	}
	stdout := truncateRunes(string(res.Stdout), maxExecOutput)
	stderr := truncateRunes(string(res.Stderr), maxExecOutput)
	switch res.ExitCode {
	case 0:
		if strings.TrimSpace(stdout) == "" {
			return "No matches found.", nil
		}
		return stdout, nil
	case 1:
		return "No matches found.", nil
	case 127:
		return "", fmt.Errorf("rg is not available in the Agent workspace")
	default:
		if strings.Contains(stderr, "rg is not available") {
			return "", fmt.Errorf("rg is not available in the Agent workspace")
		}
		if stderr != "" {
			return "", fmt.Errorf("grep failed (exit %d): %s", res.ExitCode, stderr)
		}
		return "", fmt.Errorf("grep failed (exit %d)", res.ExitCode)
	}
}
