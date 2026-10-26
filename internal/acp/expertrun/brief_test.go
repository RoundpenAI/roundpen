package expertrun_test

import (
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/expertrun"
)

func TestBuildBrief_NoSecretsShape(t *testing.T) {
	s := expertrun.BuildBrief(expertrun.Request{
		Goal:             "Add logging to foo",
		RepoPath:         "projects/demo",
		Branch:           "feat/log",
		Paths:            []string{"src/foo.go"},
		ChangeSetSummary: "touched bar.go earlier",
		IssueURL:         "https://github.com/org/repo/issues/1",
		Constraints:      "do not touch vendor/",
	})
	for _, want := range []string{"Add logging", "projects/demo", "feat/log", "src/foo.go", "changedPaths", "open_pr", "Do NOT push"} {
		if !strings.Contains(s, want) {
			t.Fatalf("brief missing %q:\n%s", want, s)
		}
	}
}

func TestParseContract_Fenced(t *testing.T) {
	reply := "Done.\n```json\n{\"status\":\"succeeded\",\"summary\":\"ok\",\"changedPaths\":[\"a.go\"],\"prIntent\":{\"action\":\"open_pr\",\"branch\":\"feat/x\"}}\n```\n"
	got := expertrun.ParseContract(reply)
	if got.Status != "succeeded" || got.Summary != "ok" || len(got.ChangedPaths) != 1 || got.ChangedPaths[0] != "a.go" {
		t.Fatalf("%+v", got)
	}
	if got.PRIntent["action"] != "open_pr" {
		t.Fatalf("prIntent=%v", got.PRIntent)
	}
}

func TestParseContract_FallbackProse(t *testing.T) {
	got := expertrun.ParseContract("I edited the file and tests pass.")
	if got.Status != expertrun.StatusSucceeded || !strings.Contains(got.Summary, "tests pass") {
		t.Fatalf("%+v", got)
	}
}
