package project

import (
	"strings"
	"testing"
)

func TestRenderWorkerPrompt(t *testing.T) {
	p := &Project{Name: "Demo", Key: "PRJ-1", RepoPath: "projects/demo", RepoURL: "https://example.com/r.git"}
	w := &Worker{Title: "Add login", Brief: "Implement OAuth login form."}
	got := RenderWorkerPrompt(p, w)
	for _, want := range []string{"projects/demo", "Add login", "OAuth", "PRJ-1", ".roundpen/context"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}
