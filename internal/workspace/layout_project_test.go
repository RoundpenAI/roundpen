package workspace

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSlugifyProjectName(t *testing.T) {
	cases := map[string]string{
		"My App":     "my-app",
		"acme_api":   "acme_api",
		"  Hello  ":  "hello",
		"@@@":        "",
		"9lives":     "9lives",
		"a":          "a",
	}
	for in, want := range cases {
		if got := SlugifyProjectName(in); got != want {
			t.Fatalf("%q: got %q want %q", in, got, want)
		}
	}
}

func TestSeedProject(t *testing.T) {
	root := t.TempDir()
	if err := SeedProject(root, "demo"); err != nil {
		t.Fatal(err)
	}
	readme := filepath.Join(root, "projects", "demo", ".roundpen", "context", "README.md")
	if _, err := os.Stat(readme); err != nil {
		t.Fatal(err)
	}
	// Second seed must not fail or wipe customizations.
	if err := os.WriteFile(readme, []byte("keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SeedProject(root, "demo"); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(readme)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "keep\n" {
		t.Fatalf("readme overwritten: %q", raw)
	}
	if err := SeedProject(root, "Bad Slug"); err == nil {
		t.Fatal("expected invalid slug error")
	}
}
