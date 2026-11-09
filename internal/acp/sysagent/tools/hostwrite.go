package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// MaxHostPlanItems caps a single HostApplyPlan call.
const MaxHostPlanItems = 100

// RegisterHostWrite adds granted-host mutation tools (NAS shares with
// readwrite grants). All are mutating and should sit behind automode confirm.
func RegisterHostWrite(r *Registry, binder *AgentBinder) {
	if r == nil || binder == nil {
		return
	}
	r.Register(Tool{
		Name: "HostRename",
		Description: "Rename or move a file/directory on a host path the assistant has readwrite grant for. " +
			"Both from and to must stay inside granted readwrite roots. Prefer HostApplyPlan for bulk renames.",
		Mutating: true,
		Parameters: objectSchema(map[string]any{
			"from": map[string]any{"type": "string", "description": "Absolute source path"},
			"to":   map[string]any{"type": "string", "description": "Absolute destination path"},
		}, "from", "to"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				From string `json:"from"`
				To   string `json:"to"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments")
			}
			return binder.hostRename(ctx, actor, in.From, in.To)
		},
	})
	r.Register(Tool{
		Name: "HostMove",
		Description: "Move a file/directory on granted host paths (rename, or copy+delete across devices). " +
			"Requires readwrite on source and destination.",
		Mutating: true,
		Parameters: objectSchema(map[string]any{
			"from": map[string]any{"type": "string", "description": "Absolute source path"},
			"to":   map[string]any{"type": "string", "description": "Absolute destination path"},
		}, "from", "to"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				From string `json:"from"`
				To   string `json:"to"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments")
			}
			return binder.hostMove(ctx, actor, in.From, in.To)
		},
	})
	r.Register(Tool{
		Name: "HostCopy",
		Description: "Copy a file or directory tree onto a readwrite-granted host path (NAS). " +
			"Source may be a granted host path, or a path under /workspace (downloaded/built files) — " +
			"workspace sources are resolved on the control plane (the Agent container cannot see /vol*). " +
			"Destination must be absolute under a readwrite grant. Dotfiles are skipped when copying trees.",
		Mutating: true,
		Parameters: objectSchema(map[string]any{
			"from": map[string]any{
				"type":        "string",
				"description": "Source: granted host abs path, or /workspace/... (or workspace-relative)",
			},
			"to": map[string]any{"type": "string", "description": "Absolute destination under a readwrite grant"},
		}, "from", "to"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				From string `json:"from"`
				To   string `json:"to"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments")
			}
			return binder.hostCopy(ctx, actor, in.From, in.To)
		},
	})
	r.Register(Tool{
		Name:        "HostMkdir",
		Description: "Create a directory (and parents) on a readwrite-granted host path.",
		Mutating:    true,
		Parameters: objectSchema(map[string]any{
			"path": map[string]any{"type": "string", "description": "Absolute directory path to create"},
		}, "path"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				Path string `json:"path"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments")
			}
			return binder.hostMkdir(ctx, actor, in.Path)
		},
	})
	r.Register(Tool{
		Name: "HostDelete",
		Description: "Delete a file or directory on a readwrite-granted host path. " +
			"Non-empty directories require recursive=true. Prefer HostApplyPlan when deleting many paths; always confirm with the user first.",
		Mutating: true,
		Parameters: objectSchema(map[string]any{
			"path":      map[string]any{"type": "string", "description": "Absolute path to delete"},
			"recursive": map[string]any{"type": "boolean", "description": "Required true to delete a non-empty directory"},
		}, "path"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				Path      string `json:"path"`
				Recursive bool   `json:"recursive"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments")
			}
			return binder.hostDelete(ctx, actor, in.Path, in.Recursive)
		},
	})
	r.Register(Tool{
		Name: "HostApplyPlan",
		Description: "Apply a batch of host file operations (rename/move/copy/mkdir/delete) under " +
			"directory grants. copy may use a /workspace source (same as HostCopy). " +
			"Show the plan to the user and get approval before calling. " +
			"At most 100 items per call. Each item needs op and the paths that op uses.",
		Mutating: true,
		Parameters: objectSchema(map[string]any{
			"items": map[string]any{
				"type": "array",
				"description": "Ordered operations. op=rename|move|copy|mkdir|delete. " +
					"rename/move/copy use from+to; mkdir uses to (or path); delete uses from (or path) and optional recursive.",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"op":        map[string]any{"type": "string"},
						"from":      map[string]any{"type": "string"},
						"to":        map[string]any{"type": "string"},
						"path":      map[string]any{"type": "string"},
						"recursive": map[string]any{"type": "boolean"},
					},
				},
			},
		}, "items"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				Items []hostPlanItem `json:"items"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("invalid arguments")
			}
			return binder.hostApplyPlan(ctx, actor, in.Items)
		},
	})
}

type hostPlanItem struct {
	Op        string `json:"op"`
	From      string `json:"from"`
	To        string `json:"to"`
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

func (b *AgentBinder) requireHost() (HostFiles, error) {
	if b == nil || b.Host == nil {
		return nil, fmt.Errorf("host directories are not available in this session")
	}
	return b.Host, nil
}

func (b *AgentBinder) hostRename(ctx context.Context, actor Actor, from, to string) (string, error) {
	host, err := b.requireHost()
	if err != nil {
		return "", err
	}
	fromAbs, toAbs, err := hostPair(from, to)
	if err != nil {
		return "", err
	}
	if err := host.RenameHost(ctx, actor, fromAbs, toAbs); err != nil {
		return "", err
	}
	return fmt.Sprintf("Renamed %s → %s", fromAbs, toAbs), nil
}

func (b *AgentBinder) hostMove(ctx context.Context, actor Actor, from, to string) (string, error) {
	host, err := b.requireHost()
	if err != nil {
		return "", err
	}
	fromAbs, toAbs, err := hostPair(from, to)
	if err != nil {
		return "", err
	}
	if err := host.MoveHost(ctx, actor, fromAbs, toAbs); err != nil {
		return "", err
	}
	return fmt.Sprintf("Moved %s → %s", fromAbs, toAbs), nil
}

func (b *AgentBinder) hostCopy(ctx context.Context, actor Actor, from, to string) (string, error) {
	host, err := b.requireHost()
	if err != nil {
		return "", err
	}
	toAbs, err := requireHostAbs(to)
	if err != nil {
		return "", fmt.Errorf("to: %w", err)
	}
	fromHost, fromLabel, fromWorkspace, err := b.resolveCopySource(ctx, actor, from)
	if err != nil {
		return "", err
	}
	if fromWorkspace {
		if err := host.PublishHost(ctx, actor, fromHost, toAbs); err != nil {
			return "", err
		}
	} else {
		if err := host.CopyHost(ctx, actor, fromHost, toAbs); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("Copied %s → %s", fromLabel, toAbs), nil
}

// resolveCopySource maps HostCopy "from" to a control-plane absolute path.
// Workspace paths (/workspace/... or relative) use the Agent sandbox host bind.
func (b *AgentBinder) resolveCopySource(ctx context.Context, actor Actor, from string) (hostAbs, label string, workspace bool, err error) {
	from = strings.TrimSpace(from)
	if from == "" {
		return "", "", false, fmt.Errorf("from is required")
	}
	if abs, isHost, herr := hostAbsPath(from); herr != nil {
		return "", "", false, fmt.Errorf("from: %w", herr)
	} else if isHost {
		return abs, abs, false, nil
	}
	if !isWorkspaceToolPath(from) {
		return "", "", false, fmt.Errorf("from: %q is not a host path (use absolute paths outside /workspace, or a /workspace path to publish)", from)
	}
	rel, err := b.resolveRel(from)
	if err != nil {
		return "", "", false, fmt.Errorf("from: %w", err)
	}
	id, err := b.ensureID(ctx, actor)
	if err != nil {
		return "", "", false, err
	}
	if b.Exec == nil {
		return "", "", false, fmt.Errorf("workspace host path is not available")
	}
	root, err := b.Exec.WorkspaceHostPath(ctx, id)
	if err != nil || strings.TrimSpace(root) == "" {
		if err == nil {
			err = fmt.Errorf("empty path")
		}
		return "", "", false, fmt.Errorf("workspace host path is not available: %w", err)
	}
	root = filepath.Clean(root)
	joined := root
	if rel != "." {
		joined = filepath.Join(root, filepath.FromSlash(rel))
	}
	joined = filepath.Clean(joined)
	sep := string(os.PathSeparator)
	if joined != root && !strings.HasPrefix(joined, root+sep) {
		return "", "", false, fmt.Errorf("from escapes workspace")
	}
	return joined, GuestAbs(rel), true, nil
}

func isWorkspaceToolPath(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" {
		return false
	}
	if !strings.HasPrefix(p, "/") {
		return true
	}
	clean := path.Clean(p)
	return clean == WorkspaceRoot || strings.HasPrefix(clean, WorkspaceRoot+"/")
}

func (b *AgentBinder) hostMkdir(ctx context.Context, actor Actor, p string) (string, error) {
	host, err := b.requireHost()
	if err != nil {
		return "", err
	}
	abs, err := requireHostAbs(p)
	if err != nil {
		return "", err
	}
	if err := host.MkdirHost(ctx, actor, abs); err != nil {
		return "", err
	}
	return fmt.Sprintf("Created directory %s", abs), nil
}

func (b *AgentBinder) hostDelete(ctx context.Context, actor Actor, p string, recursive bool) (string, error) {
	host, err := b.requireHost()
	if err != nil {
		return "", err
	}
	abs, err := requireHostAbs(p)
	if err != nil {
		return "", err
	}
	if err := host.DeleteHost(ctx, actor, abs, recursive); err != nil {
		return "", err
	}
	return fmt.Sprintf("Deleted %s", abs), nil
}

func (b *AgentBinder) hostApplyPlan(ctx context.Context, actor Actor, items []hostPlanItem) (string, error) {
	host, err := b.requireHost()
	if err != nil {
		return "", err
	}
	if len(items) == 0 {
		return "", fmt.Errorf("items must be a non-empty array")
	}
	if len(items) > MaxHostPlanItems {
		return "", fmt.Errorf("at most %d items per HostApplyPlan (got %d)", MaxHostPlanItems, len(items))
	}
	var lines []string
	for i, it := range items {
		op := strings.ToLower(strings.TrimSpace(it.Op))
		summary, err := b.applyOneHostOp(ctx, host, actor, it, op)
		if err != nil {
			return "", fmt.Errorf("item %d (%s): %w", i, op, err)
		}
		lines = append(lines, summary)
	}
	return fmt.Sprintf("Applied %d host operations:\n%s", len(lines), strings.Join(lines, "\n")), nil
}

func (b *AgentBinder) applyOneHostOp(ctx context.Context, host HostFiles, actor Actor, it hostPlanItem, op string) (string, error) {
	switch op {
	case "rename":
		from, to, err := hostPair(firstPath(it.From, it.Path), firstPath(it.To, ""))
		if err != nil {
			return "", err
		}
		if err := host.RenameHost(ctx, actor, from, to); err != nil {
			return "", err
		}
		return fmt.Sprintf("rename %s → %s", from, to), nil
	case "move":
		from, to, err := hostPair(firstPath(it.From, it.Path), firstPath(it.To, ""))
		if err != nil {
			return "", err
		}
		if err := host.MoveHost(ctx, actor, from, to); err != nil {
			return "", err
		}
		return fmt.Sprintf("move %s → %s", from, to), nil
	case "copy":
		msg, err := b.hostCopy(ctx, actor, firstPath(it.From, it.Path), firstPath(it.To, ""))
		if err != nil {
			return "", err
		}
		return strings.Replace(msg, "Copied ", "copy ", 1), nil
	case "mkdir":
		abs, err := requireHostAbs(firstPath(it.To, it.Path))
		if err != nil {
			return "", err
		}
		if err := host.MkdirHost(ctx, actor, abs); err != nil {
			return "", err
		}
		return fmt.Sprintf("mkdir %s", abs), nil
	case "delete":
		abs, err := requireHostAbs(firstPath(it.From, it.Path))
		if err != nil {
			return "", err
		}
		if err := host.DeleteHost(ctx, actor, abs, it.Recursive); err != nil {
			return "", err
		}
		return fmt.Sprintf("delete %s", abs), nil
	default:
		return "", fmt.Errorf("unknown op %q (want rename|move|copy|mkdir|delete)", it.Op)
	}
}

func firstPath(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

func hostPair(from, to string) (string, string, error) {
	fromAbs, err := requireHostAbs(from)
	if err != nil {
		return "", "", fmt.Errorf("from: %w", err)
	}
	toAbs, err := requireHostAbs(to)
	if err != nil {
		return "", "", fmt.Errorf("to: %w", err)
	}
	return fromAbs, toAbs, nil
}

func requireHostAbs(p string) (string, error) {
	abs, isHost, err := hostAbsPath(p)
	if err != nil {
		return "", err
	}
	if !isHost {
		return "", fmt.Errorf("%q is not a host path (use absolute paths outside /workspace)", p)
	}
	return abs, nil
}
