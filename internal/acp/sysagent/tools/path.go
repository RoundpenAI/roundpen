package tools

import (
	"fmt"
	"path"
	"strings"
)

// WorkspaceRoot is the agent-visible working directory root.
const WorkspaceRoot = "/workspace"

// ResolveWorkspacePath maps an agent path to a workspace-relative path.
// Accepts absolute paths under /workspace or relative paths (relative to /workspace).
func ResolveWorkspacePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("path is required")
	}
	var abs string
	if strings.HasPrefix(p, "/") {
		abs = path.Clean(p)
	} else {
		abs = path.Clean(path.Join(WorkspaceRoot, p))
	}
	if abs != WorkspaceRoot && !strings.HasPrefix(abs, WorkspaceRoot+"/") {
		return "", fmt.Errorf("path escapes workspace")
	}
	rel := strings.TrimPrefix(abs, WorkspaceRoot)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return ".", nil
	}
	if rel == ".." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") {
		return "", fmt.Errorf("path escapes workspace")
	}
	return rel, nil
}
