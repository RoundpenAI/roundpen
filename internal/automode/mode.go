package automode

import "strings"

// Permission modes the chat picker offers. Decide is the classifier (the
// former Auto toggle). Edit auto-approves workspace file writes and still
// asks about everything else. All auto-approves every tool action.
const (
	ModeEdit   = "edit"
	ModeDecide = "decide"
	ModeAll    = "all"
)

// NormalizeMode maps a client value onto one of the three modes. Empty and
// unknown values stay on the classifier.
func NormalizeMode(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case ModeEdit:
		return ModeEdit
	case ModeAll:
		return ModeAll
	default:
		return ModeDecide
	}
}

// IsWorkspaceEdit reports a file write or in-place edit. Shell, browser, and
// other side effects are not edits.
func IsWorkspaceEdit(name, kind string) bool {
	if strings.EqualFold(strings.TrimSpace(kind), "edit") {
		return true
	}
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "write", "edit", "notebookedit":
		return true
	default:
		return false
	}
}
