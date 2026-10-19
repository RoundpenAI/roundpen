package automode

import "testing"

func TestNormalizeMode(t *testing.T) {
	if got := NormalizeMode(""); got != ModeDecide {
		t.Fatalf("empty = %q", got)
	}
	if got := NormalizeMode("EDIT"); got != ModeEdit {
		t.Fatalf("edit = %q", got)
	}
	if got := NormalizeMode("all"); got != ModeAll {
		t.Fatalf("all = %q", got)
	}
	if got := NormalizeMode("yolo"); got != ModeDecide {
		t.Fatalf("unknown = %q", got)
	}
}

func TestIsWorkspaceEdit(t *testing.T) {
	if !IsWorkspaceEdit("Write", "") || !IsWorkspaceEdit("edit", "") {
		t.Fatal("file tools should count as edits")
	}
	if !IsWorkspaceEdit("Bash", "edit") {
		t.Fatal("kind edit should count")
	}
	if IsWorkspaceEdit("Bash", "") || IsWorkspaceEdit("Skill", "execute") {
		t.Fatal("shell and skill are not edits")
	}
}
