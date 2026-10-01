package workspace

import (
	"fmt"
	"os"
	"path/filepath"
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
