package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHasGitRepo(t *testing.T) {
	root := t.TempDir()
	if HasGitRepo(root, "demo") {
		t.Fatal("expected false")
	}
	dir := filepath.Join(root, "projects", "demo", ".git")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if !HasGitRepo(root, "demo") {
		t.Fatal("expected true")
	}
}

func TestCloneScriptMentionsPreserve(t *testing.T) {
	s := CloneScript()
	for _, needle := range []string{"git clone", ".roundpen", "already_cloned"} {
		if !strings.Contains(s, needle) {
			t.Fatalf("missing %q", needle)
		}
	}
}
