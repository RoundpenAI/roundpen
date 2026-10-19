package project

import "testing"

func TestRenderWorkerPrompt(t *testing.T) {
	p := &Project{Name: "Demo", Key: "PRJ-1", RepoPath: "projects/demo", RepoURL: "https://example.com/r.git"}
	w := &Worker{Title: "Add login", Brief: "Implement OAuth login form."}
	got := RenderWorkerPrompt(p, w)
	for _, want := range []string{"projects/demo", "Add login", "OAuth", "PRJ-1", ".roundpen/context"} {
		if !contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && (func() bool {
			for i := 0; i+len(sub) <= len(s); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		})()))
}
