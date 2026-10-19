// Package vcs abstracts remote git hosts for Roundpen PR create/merge.
// P1 ships a GitHub-backed implementation; Gitea/GitLab are extension points only.
package vcs

import "strings"

// Kind identifies a remote host family.
type Kind string

const (
	KindGitHub Kind = "github"
	KindGitea  Kind = "gitea"  // extension point — not wired in P1 UI/tools
	KindGitLab Kind = "gitlab" // extension point — not wired in P1 UI/tools
)

// DetectKind from a remote URL or host name. Unknown defaults to github for P1.
func DetectKind(remoteOrHost string) Kind {
	s := strings.ToLower(strings.TrimSpace(remoteOrHost))
	switch {
	case strings.Contains(s, "gitlab"):
		return KindGitLab
	case strings.Contains(s, "gitea") || strings.Contains(s, "gitea."):
		return KindGitea
	default:
		return KindGitHub
	}
}

// CreatePRInput is host-agnostic PR creation.
type CreatePRInput struct {
	Title string
	Body  string
	Base  string // default branch name; empty = host default
	Head  string // current branch; empty = current
	Draft bool
}

// MergePRInput merges an open PR.
type MergePRInput struct {
	Number int
	Method string // squash | merge | rebase — P1 defaults squash
}
