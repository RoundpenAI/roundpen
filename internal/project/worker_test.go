package project

import (
	"strings"
	"testing"
)

func TestRenderWorkerPrompt(t *testing.T) {
	p := &Project{Name: "Demo", Key: "PRJ-1", RepoPath: "projects/demo", RepoURL: "https://example.com/r.git"}
	w := &Worker{Title: "Add login", Brief: "Implement OAuth login form.", Role: RoleImplementer}
	got := RenderWorkerPrompt(p, w)
	for _, want := range []string{"projects/demo", "Add login", "OAuth", "PRJ-1", ".roundpen/context", "implementer"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	review := RenderWorkerPrompt(p, &Worker{Title: "Review PR", Role: RoleReviewer})
	if !strings.Contains(review, "Review assignment") {
		t.Fatalf("reviewer prompt: %s", review)
	}
}

func TestNormalizeWorkerRole(t *testing.T) {
	got, err := NormalizeWorkerRole("")
	if err != nil || got != RoleImplementer {
		t.Fatalf("default: %q %v", got, err)
	}
	if _, err := NormalizeWorkerRole("designer"); err == nil {
		t.Fatal("expected error")
	}
}

func TestRenderWorkerDoneNotice(t *testing.T) {
	w := &Worker{Key: "WRK-3", Title: "Add login", Role: RoleImplementer, Status: WorkerSucceeded}
	got := RenderWorkerDoneNotice(w, WorkerSucceeded)
	for _, want := range []string{"WRK-3", "implementer", "succeeded", "Add login", "ListProjectWorkers"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestRenderWorkerPromptSharedContext(t *testing.T) {
	got := RenderWorkerPrompt(&Project{RepoPath: "projects/x"}, &Worker{Title: "t", Role: RoleTester})
	if !strings.Contains(got, "Shared context") || !strings.Contains(got, "append a short note") {
		t.Fatalf("expected shared-context guidance:\n%s", got)
	}
}
