package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ClientActionKey marks a tool result the console should fulfill via the
// fnOS Trim JS SDK (openFile / showFileDetails / openFileManager).
const ClientActionKey = "_clientAction"

// FnosPaths resolves display labels for host paths (optional).
type FnosPaths interface {
	DisplayPath(ctx context.Context, path, language string) (string, error)
}

// RegisterFnosOpen adds OpenHostPath for revealing/opening files on fnOS.
func RegisterFnosOpen(r *Registry, binder *AgentBinder) {
	if r == nil || binder == nil {
		return
	}
	r.Register(Tool{
		Name: "OpenHostPath",
		Description: "Ask the Roundpen console (on fnOS) to open a granted host path with the system " +
			"file manager / default app / details page. action=open|details|manager (default open). " +
			"Off fnOS the tool still returns the path for the user to open manually.",
		Parameters: objectSchema(map[string]any{
			"path":     map[string]any{"type": "string", "description": "Absolute host path under a directory grant"},
			"action":   map[string]any{"type": "string", "description": "open (default) | details | manager"},
			"language": map[string]any{"type": "string", "description": "Optional locale for display path (e.g. zh-CN)"},
		}, "path"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				Path     string `json:"path"`
				Action   string `json:"action"`
				Language string `json:"language"`
			}
			if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.Path) == "" {
				return "", fmt.Errorf("path is required")
			}
			return binder.openHostPath(ctx, actor, in.Path, in.Action, in.Language)
		},
	})
}

func (b *AgentBinder) openHostPath(ctx context.Context, actor Actor, rawPath, action, language string) (string, error) {
	host, err := b.requireHost()
	if err != nil {
		return "", err
	}
	abs, err := requireHostAbs(rawPath)
	if err != nil {
		return "", err
	}
	rc, _, err := host.OpenHost(ctx, actor, abs)
	if err != nil {
		return "", err
	}
	_ = rc.Close()

	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "", "open", "file":
		action = "open"
	case "details", "detail", "info":
		action = "details"
	case "manager", "folder", "reveal":
		action = "manager"
	default:
		return "", fmt.Errorf("action must be open, details, or manager")
	}

	display := ""
	if b.Fnos != nil {
		if language == "" {
			language = "zh-CN"
		}
		if d, derr := b.Fnos.DisplayPath(ctx, abs, language); derr == nil {
			display = d
		}
	}

	payload := map[string]any{
		"ok":     true,
		"path":   abs,
		"action": action,
		"note":   "On fnOS desktop the console opens this via the system SDK; elsewhere show the path to the user.",
		ClientActionKey: map[string]any{
			"kind":   "fnos.open",
			"action": action,
			"path":   abs,
		},
	}
	if display != "" {
		payload["displayPath"] = display
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
