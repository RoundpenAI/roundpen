package workspace

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSeedLayoutCreatesDirsAndAgentsMD(t *testing.T) {
	root := t.TempDir()
	if err := SeedLayout(root); err != nil {
		t.Fatal(err)
	}
	for _, d := range CanonicalDirs {
		fi, err := os.Stat(filepath.Join(root, d))
		if err != nil || !fi.IsDir() {
			t.Fatalf("%s: err=%v isDir=%v", d, err, fi != nil && fi.IsDir())
		}
	}
	body, err := os.ReadFile(filepath.Join(root, AgentsMDName))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"uploads/", "projects/<name>/", "outputs/", "$HOME/.roundpen"} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("AGENTS.md missing %q", want)
		}
	}
}

func TestSeedLayoutDoesNotOverwriteAgentsMD(t *testing.T) {
	root := t.TempDir()
	custom := "# custom\n"
	if err := os.WriteFile(filepath.Join(root, AgentsMDName), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SeedLayout(root); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(root, AgentsMDName))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != custom {
		t.Fatalf("overwrote custom AGENTS.md: %q", body)
	}
	// Dirs are still created.
	if _, err := os.Stat(filepath.Join(root, DirProjects)); err != nil {
		t.Fatal(err)
	}
}

func TestSeedLayoutRejectsEmptyPath(t *testing.T) {
	if err := SeedLayout(""); err == nil {
		t.Fatal("expected error")
	}
}
