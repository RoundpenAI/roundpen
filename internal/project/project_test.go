package project

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/workspace"
)

func TestResolveSlug(t *testing.T) {
	slug, err := ResolveSlug("", "My Cool App")
	if err != nil || slug != "my-cool-app" {
		t.Fatalf("got %q %v", slug, err)
	}
	slug, err = ResolveSlug("Acme_API", "ignored")
	if err != nil || slug != "acme_api" {
		t.Fatalf("got %q %v", slug, err)
	}
	if _, err := ResolveSlug("Bad Slug", "x"); err == nil {
		t.Fatal("expected error for spaced explicit slug")
	}
}

func TestValidateRepoURL(t *testing.T) {
	for _, raw := range []string{
		"",
		"https://github.com/acme/app.git",
		"git@github.com:acme/app.git",
		"ssh://git@github.com/acme/app.git",
	} {
		if err := ValidateRepoURL(raw); err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
	}
	if err := ValidateRepoURL("javascript:alert(1)"); err == nil {
		t.Fatal("expected scheme error")
	}
}

func TestProjectPaths(t *testing.T) {
	if got := workspace.ProjectRelPath("foo"); got != "projects/foo" {
		t.Fatalf("path: %q", got)
	}
	if got := workspace.ProjectContextRelPath("foo"); got != "projects/foo/.roundpen/context" {
		t.Fatalf("context: %q", got)
	}
}
