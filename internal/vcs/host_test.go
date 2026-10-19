package vcs

import "testing"

func TestDetectKind(t *testing.T) {
	if DetectKind("https://github.com/org/repo.git") != KindGitHub {
		t.Fatal("github")
	}
	if DetectKind("git@gitlab.com:org/repo.git") != KindGitLab {
		t.Fatal("gitlab")
	}
	if DetectKind("https://gitea.example.com/a/b") != KindGitea {
		t.Fatal("gitea")
	}
}
