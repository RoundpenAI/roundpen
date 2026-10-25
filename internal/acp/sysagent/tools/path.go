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
	return ResolveWorkspacePathFrom(WorkspaceRoot, p)
}

// ResolveWorkspacePathFrom maps an agent path to a workspace-relative path.
// Relative paths are resolved against base (guest absolute, default /workspace).
// Absolute paths must still stay under /workspace.
func ResolveWorkspacePathFrom(base, p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("path is required")
	}
	base = strings.TrimSpace(base)
	if base == "" {
		base = WorkspaceRoot
	}
	base = path.Clean(base)
	if base != WorkspaceRoot && !strings.HasPrefix(base, WorkspaceRoot+"/") {
		return "", fmt.Errorf("base escapes workspace")
	}
	var abs string
	if strings.HasPrefix(p, "/") {
		abs = path.Clean(p)
	} else {
		abs = path.Clean(path.Join(base, p))
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

// GuestAbs joins a workspace-relative path onto /workspace.
func GuestAbs(rel string) string {
	rel = strings.TrimSpace(rel)
	if rel == "" || rel == "." {
		return WorkspaceRoot
	}
	if strings.HasPrefix(rel, "/") {
		return path.Clean(rel)
	}
	return path.Join(WorkspaceRoot, rel)
}
