package workspace

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Canonical top-level directories under the account workspace (/workspace).
// Agents and the Files UI share this layout; do not invent new root folders
// for routine work.
const (
	DirUploads  = "uploads"
	DirProjects = "projects"
	DirNotes    = "notes"
	DirOutputs  = "outputs"
	DirTmp      = "tmp"
)

// AgentsMDName is the layout contract file seeded at the workspace root.
// Coding agents (Claude Code, Codex, …) and System Agent are expected to
// read and follow it.
const AgentsMDName = "AGENTS.md"

// CanonicalDirs is the ordered set of directories SeedLayout creates.
var CanonicalDirs = []string{DirUploads, DirProjects, DirNotes, DirOutputs, DirTmp}

// AgentsMD is the English layout contract written to /workspace/AGENTS.md.
// Keep it short: models load this from disk; docs/workspace-layout.md is the
// longer human-facing reference.
const AgentsMD = `# Workspace layout

This account workspace is shared by every assistant. Keep the root tidy.

| Path | Use for |
|------|---------|
| uploads/ | User-provided materials (uploads, dumps, source PDFs). Prefer reading here before copying. |
| projects/<name>/ | One directory per project or git repo. Clone and create apps here — never at the workspace root. |
| notes/ | Durable notes the user cares about. |
| outputs/ | Deliverables you produce: reports, plans, exports, screenshots. |
| tmp/ | Scratch and ephemeral files. Safe to delete when done. |

## Rules

1. Do **not** create loose files at /workspace (no report.md, ip.txt, or ISS-*.md at the root).
2. Do **not** git clone or scaffold a project at the workspace root — use projects/<name>/.
3. Do **not** put skills, git credentials, or agent config under /workspace — those live in $HOME/.roundpen/.
4. Name project dirs with ^[a-z0-9][a-z0-9_-]*$ (e.g. projects/pokemon).
5. Before creating a new top-level folder, prefer the table above. Ask the user if something truly does not fit.

When unsure where a file belongs, put it in tmp/ and move it once the destination is clear.
`

// ProjectSlugPattern is the allowed name for projects/<slug>/.
var ProjectSlugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// ValidProjectSlug reports whether slug may be used under projects/.
func ValidProjectSlug(slug string) bool {
	return ProjectSlugPattern.MatchString(slug)
}

// ProjectRelPath is the guest-relative path for a project tree (projects/<slug>).
func ProjectRelPath(slug string) string {
	return path.Join(DirProjects, slug)
}

// ProjectContextRelPath is the Shared Context directory for a product Project.
// It lives beside the repo tree and is not meant to be committed.
func ProjectContextRelPath(slug string) string {
	return path.Join(DirProjects, slug, ".roundpen", "context")
}

// SeedProject creates projects/<slug>/ and its Shared Context directory under
// the host workspace root. Existing files are left untouched.
func SeedProject(hostPath, slug string) error {
	if !ValidProjectSlug(slug) {
		return fmt.Errorf("invalid project slug %q", slug)
	}
	hostPath = filepath.Clean(hostPath)
	if hostPath == "" || hostPath == "." {
		return fmt.Errorf("workspace host path is empty")
	}
	dirs := []string{
		filepath.Join(hostPath, filepath.FromSlash(ProjectRelPath(slug))),
		filepath.Join(hostPath, filepath.FromSlash(ProjectContextRelPath(slug))),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	readme := filepath.Join(dirs[1], "README.md")
	if _, err := os.Stat(readme); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	body := "# Project context\n\nShared notes for agents working on this project.\n" +
		"Prefer adding playbooks and learnings here so future agents can reuse them.\n"
	return os.WriteFile(readme, []byte(body), 0o644)
}

// SlugifyProjectName turns a display name into a projects/ directory slug.
// Empty or invalid results yield "".
func SlugifyProjectName(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			return r
		case r == ' ' || r == '-':
			return '-'
		default:
			return -1
		}
	}, s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if !ValidProjectSlug(s) {
		return ""
	}
	return s
}

// SeedLayout creates the canonical directories under hostPath and writes
// AGENTS.md when it is missing. Existing files are left untouched so users
// can customize the contract. hostPath is the host directory mounted at
// guest /workspace.
func SeedLayout(hostPath string) error {
	hostPath = filepath.Clean(hostPath)
	if hostPath == "" || hostPath == "." {
		return fmt.Errorf("workspace host path is empty")
	}
	if err := os.MkdirAll(hostPath, 0o755); err != nil {
		return err
	}
	for _, d := range CanonicalDirs {
		if err := os.MkdirAll(filepath.Join(hostPath, d), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	agentsPath := filepath.Join(hostPath, AgentsMDName)
	if _, err := os.Stat(agentsPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(agentsPath, []byte(AgentsMD), 0o644)
}
