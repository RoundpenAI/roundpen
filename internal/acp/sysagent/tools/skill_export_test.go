package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

// Slash commands and the model-invoked Skill tool must inject byte-identical
// instruction text; this guards the shared formatting.
func TestExpandSkillMatchesToolInvocation(t *testing.T) {
	cases := []struct{ name, args string }{
		{"review", ""},
		{"commit", "写中文提交信息"},
		{"fix", "  lint  "},
		{"summarize", ""},
	}
	for _, tc := range cases {
		binder, ex := skillBinder()
		ex.queue = []*sandbox.ExecResult{notFound()} // no installed override → builtin
		reg := tools.NewRegistry()
		tools.RegisterSkill(reg, binder, nil)
		actor := tools.Actor{Username: "alice"}

		payload, err := json.Marshal(map[string]string{"skill": tc.name, "args": tc.args})
		if err != nil {
			t.Fatal(err)
		}
		viaTool, err := reg.Call(context.Background(), actor, "Skill", payload)
		if err != nil {
			t.Fatalf("%s: tool: %v", tc.name, err)
		}
		expanded, err := tools.ExpandSkill(tc.name, tc.args)
		if err != nil {
			t.Fatalf("%s: ExpandSkill: %v", tc.name, err)
		}
		if expanded != viaTool {
			t.Fatalf("%s: expansion differs\nExpandSkill: %q\ntool:        %q", tc.name, expanded, viaTool)
		}
	}
}

func TestExpandSkillUnknown(t *testing.T) {
	_, err := tools.ExpandSkill("nope", "")
	if err == nil || !strings.Contains(err.Error(), `unknown skill "nope"`) {
		t.Fatalf("err=%v", err)
	}
	if _, err := tools.ExpandSkill("  ", ""); err == nil {
		t.Fatal("blank name must be rejected")
	}
}

func TestExpandInstalledSkillPrefersInstalledAndFallsBack(t *testing.T) {
	binder, ex := skillBinder()
	ex.queue = []*sandbox.ExecResult{
		execStdout(gsyncSkillFile), // gsync installed
		notFound(),                 // review → builtin fallback
		notFound(),                 // nope → unknown
	}
	actor := tools.Actor{Username: "alice"}

	out, err := tools.ExpandInstalledSkill(context.Background(), binder, actor, "gsync", "force")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Run gsync from the repository root") || !strings.Contains(out, "force") || !strings.Contains(out, "installed") {
		t.Fatalf("out=%q", out)
	}

	out, err = tools.ExpandInstalledSkill(context.Background(), binder, actor, "review", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "builtin") || !strings.Contains(out, "git diff HEAD") {
		t.Fatalf("out=%q", out)
	}

	if _, err := tools.ExpandInstalledSkill(context.Background(), binder, actor, "nope", ""); err == nil {
		t.Fatal("unknown skill must error")
	}
}

func TestListInstalledSkills(t *testing.T) {
	binder, ex := skillBinder()
	ex.queue = []*sandbox.ExecResult{
		execStdout("borked\ngsync\n"),
		execStdout("---\nname: borked\n---\n"), // empty body → unreadable, must not fail the listing
		execStdout(gsyncSkillFile),
	}
	got, err := tools.ListInstalledSkills(context.Background(), binder, tools.Actor{Username: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("want 2 skills, got %d (%+v)", len(got), got)
	}
	if got[0].Name != "borked" || !strings.Contains(got[0].Description, "unreadable") {
		t.Fatalf("unreadable skill degraded wrong: %+v", got[0])
	}
	if got[1].Name != "gsync" || got[1].Description != "Sync generated code across the workspace." {
		t.Fatalf("gsync=%+v", got[1])
	}
	if got[1].Source != tools.SkillSourceInstalled {
		t.Fatalf("source=%q", got[1].Source)
	}
}

func TestListInstalledSkillsSurfacesExecFailure(t *testing.T) {
	binder, ex := skillBinder()
	ex.fail = errors.New("exec boom")
	if _, err := tools.ListInstalledSkills(context.Background(), binder, tools.Actor{Username: "alice"}); err == nil {
		t.Fatal("exec failure must surface")
	}
}
