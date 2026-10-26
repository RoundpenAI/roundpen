package expertrun

import (
	"fmt"
	"strings"
)

// BuildBrief assembles the one-shot Prompt for a coding specialist.
// No secrets, no full transcript — goal, paths, constraints, return contract only.
func BuildBrief(req Request) string {
	var b strings.Builder
	b.WriteString("You are a one-shot coding specialist inside Roundpen's Docker agent.\n")
	b.WriteString("Work under /workspace. Edit files as needed. Do NOT push, force-push, or merge pull requests.\n")
	b.WriteString("Do NOT print API keys, tokens, or credentials.\n")
	b.WriteString("When finished, Roundpen's System Agent will commit and open a GitHub PR after the user confirms.\n\n")

	b.WriteString("Goal:\n")
	b.WriteString(strings.TrimSpace(req.Goal))
	b.WriteString("\n\n")

	if rp := strings.TrimSpace(req.RepoPath); rp != "" {
		b.WriteString("Repo path (under /workspace): ")
		b.WriteString(rp)
		b.WriteString("\n")
	}
	if br := strings.TrimSpace(req.Branch); br != "" {
		b.WriteString("Preferred branch: ")
		b.WriteString(br)
		b.WriteString("\n")
	}
	if len(req.Paths) > 0 {
		b.WriteString("Focus paths:\n")
		for _, p := range req.Paths {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			b.WriteString("- ")
			b.WriteString(p)
			b.WriteString("\n")
		}
	}
	if s := strings.TrimSpace(req.ChangeSetSummary); s != "" {
		b.WriteString("\nPrior ChangeSet summary:\n")
		b.WriteString(s)
		b.WriteString("\n")
	}
	if u := strings.TrimSpace(req.IssueURL); u != "" {
		b.WriteString("Issue: ")
		b.WriteString(u)
		b.WriteString("\n")
	}
	if u := strings.TrimSpace(req.PRURL); u != "" {
		b.WriteString("Existing PR: ")
		b.WriteString(u)
		b.WriteString("\n")
	}
	if c := strings.TrimSpace(req.Constraints); c != "" {
		b.WriteString("\nConstraints:\n")
		b.WriteString(c)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(returnContractInstructions())
	return b.String()
}

func returnContractInstructions() string {
	return strings.TrimSpace(fmt.Sprintf(`
Return contract — end your FINAL message with a fenced JSON block (and nothing after it):
`+"```json"+`
{"status":"succeeded"|"failed"|"needs_user","summary":"one short paragraph","changedPaths":["relative/path"],"prIntent":{"action":"none"|"open_pr"|"push_branch","branch":"optional","title":"optional"},"notesForUser":"optional blockers"}
`+"```"+`
status=succeeded when the code change is done on disk. Prefer prIntent.action=open_pr for delivery (Roundpen always opens a PR; user confirms). Use needs_user if you need login or a human decision.
`))
}
