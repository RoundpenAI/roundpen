package tools_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

const gsyncSkillFile = `---
name: gsync
description: Sync generated code across the workspace.
args: true
allowedTools:
- Bash
- Read
---
Run gsync from the repository root and fix any drift it reports.
`

func skillBinder() (*tools.AgentBinder, *memFiles, *stubExec) {
	fs := &memFiles{data: map[string][]byte{}}
	ex := &stubExec{ws: "host-ws", res: &sandbox.ExecResult{ExitCode: 0, Stdout: nil}}
	return &tools.AgentBinder{
		Slots: &stubAgentSlots{id: "sb"},
		Exec:  ex,
		Files: fs,
	}, fs, ex
}

func confirmCtx(t *testing.T, answer string) context.Context {
	t.Helper()
	return tools.WithInteractor(context.Background(), &tools.Interactor{
		AskUser: func(_ context.Context, _ tools.AskQuestion) (string, error) { return answer, nil },
	})
}

func TestSkillInvoke(t *testing.T) {
	binder, fs, _ := skillBinder()
	fs.data[".roundpen/skills/gsync.md"] = []byte(gsyncSkillFile)
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, binder, nil)
	if _, ok := reg.Get("Skill"); !ok {
		t.Fatal("missing Skill tool")
	}
	actor := tools.Actor{Username: "alice"}

	// Workspace skill, with args.
	out, err := reg.Call(context.Background(), actor, "Skill", json.RawMessage(`{"skill":"gsync","args":"force"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Run gsync from the repository root") || !strings.Contains(out, "force") || !strings.Contains(out, "workspace") {
		t.Fatalf("out=%q", out)
	}

	// Built-in fallback.
	out, err = reg.Call(context.Background(), actor, "Skill", json.RawMessage(`{"skill":"review"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "builtin") || !strings.Contains(out, "git diff HEAD") {
		t.Fatalf("out=%q", out)
	}

	// Unknown skill.
	_, err = reg.Call(context.Background(), actor, "Skill", json.RawMessage(`{"skill":"nope"}`))
	if err == nil {
		t.Fatal("expected unknown-skill error")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Fatalf("err=%q", err)
	}

	// Missing argument.
	if _, err := reg.Call(context.Background(), actor, "Skill", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error for missing skill")
	}
	// Invalid name.
	if _, err := reg.Call(context.Background(), actor, "Skill", json.RawMessage(`{"skill":"Bad Name"}`)); err == nil {
		t.Fatal("expected invalid-name error")
	}
}

func TestSkillWorkspaceOverridesBuiltin(t *testing.T) {
	binder, fs, _ := skillBinder()
	fs.data[".roundpen/skills/review.md"] = []byte(`---
name: review
description: Custom review workflow for this repo.
---
Follow the team's custom review checklist in CONTRIBUTING.md.
`)
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, binder, nil)
	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "Skill", json.RawMessage(`{"skill":"review"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "custom review checklist") || !strings.Contains(out, "workspace") {
		t.Fatalf("workspace override did not win: %q", out)
	}
}

func TestSkillListMergesSources(t *testing.T) {
	binder, fs, ex := skillBinder()
	fs.data[".roundpen/skills/gsync.md"] = []byte(gsyncSkillFile)
	ex.res = &sandbox.ExecResult{ExitCode: 0, Stdout: []byte("gsync\n")}
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, binder, nil)

	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "Skill", json.RawMessage(`{"action":"list"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "commit") || !strings.Contains(out, "[builtin]") {
		t.Fatalf("missing builtin: %q", out)
	}
	if !strings.Contains(out, "gsync") || !strings.Contains(out, "[workspace]") {
		t.Fatalf("missing workspace skill: %q", out)
	}
	if !strings.Contains(out, "review") { // review is a builtin too
		t.Fatalf("expected review: %q", out)
	}
}

func TestSkillInstallInline(t *testing.T) {
	binder, fs, _ := skillBinder()
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, binder, nil)
	actor := tools.Actor{Username: "alice"}

	// Fresh install needs no interactor.
	out, err := reg.Call(context.Background(), actor, "Skill", json.RawMessage(`{
		"action":"install",
		"content":`+jsonContent(gsyncSkillFile)+`
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Installed skill \"gsync\"") {
		t.Fatalf("out=%q", out)
	}
	if _, ok := fs.data[".roundpen/skills/gsync.md"]; !ok {
		t.Fatal("skill file not written")
	}
	// The stored file round-trips.
	rc, err := fs.ReadFile(context.Background(), "sb", ".roundpen/skills/gsync.md")
	if err != nil {
		t.Fatal(err)
	}
	rc.Close()
}

func TestSkillInstallOverwriteConfirm(t *testing.T) {
	newContent := `---
name: gsync
description: New syncing workflow.
---
Use gsync with --deep always.
`
	for _, tc := range []struct{ answer, want string }{
		{"Cancel", "not installed"},
		{"Overwrite", "Installed skill \"gsync\""},
	} {
		binder, fs, _ := skillBinder()
		fs.data[".roundpen/skills/gsync.md"] = []byte(gsyncSkillFile)
		reg := tools.NewRegistry()
		tools.RegisterSkill(reg, binder, nil)
		out, err := reg.Call(confirmCtx(t, tc.answer), tools.Actor{Username: "alice"}, "Skill", json.RawMessage(`{
			"action":"install",
			"content":`+jsonContent(newContent)+`
		}`))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out, tc.want) {
			t.Fatalf("answer=%q out=%q want=%q", tc.answer, out, tc.want)
		}
	}
}

func TestSkillInstallShadowsBuiltinNeedsConfirm(t *testing.T) {
	binder, _, _ := skillBinder()
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, binder, nil)
	// No workspace skill, but "commit" is a builtin → confirm required.
	out, err := reg.Call(confirmCtx(t, "Cancel"), tools.Actor{Username: "alice"}, "Skill", json.RawMessage(`{
		"action":"install",
		"content":`+jsonContent(`---
name: commit
description: Custom commit flow.
---
Do the custom commit flow.
`)+`
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not installed") {
		t.Fatalf("out=%q", out)
	}
}

func TestSkillInstallFromURL(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/gsync.md", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(gsyncSkillFile))
	})
	mux.HandleFunc("/no-name.md", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("---\ndescription: A skill without a name field.\n---\nDo the thing.\n"))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	binder, _, _ := skillBinder()
	adminReg := tools.NewRegistry()
	tools.RegisterSkill(adminReg, binder, tools.NewWebHTTPClient(tools.WebClientOptions{AllowLoopback: true}))
	actor := tools.Actor{Username: "alice"}

	out, err := adminReg.Call(context.Background(), actor, "Skill", json.RawMessage(`{
		"action":"install",
		"url":"`+ts.URL+`/gsync.md"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Installed skill \"gsync\"") {
		t.Fatalf("out=%q", out)
	}

	// Name derived from URL basename when frontmatter has none.
	out, err = adminReg.Call(context.Background(), actor, "Skill", json.RawMessage(`{
		"action":"install",
		"url":"`+ts.URL+`/no-name.md"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Installed skill \"no-name\"") {
		t.Fatalf("out=%q", out)
	}

	// URL install without a web client is refused.
	noWeb := tools.NewRegistry()
	tools.RegisterSkill(noWeb, binder, nil)
	if _, err := noWeb.Call(context.Background(), actor, "Skill", json.RawMessage(`{
		"action":"install",
		"url":"https://example.com/x.md"
	}`)); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("expected unsupported error, got %v", err)
	}
}

func TestSkillInstallValidation(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, &tools.AgentBinder{
		Slots: &stubAgentSlots{id: "sb"},
		Files: &memFiles{data: map[string][]byte{}},
		Exec:  &stubExec{},
	}, nil)
	actor := tools.Actor{Username: "alice"}
	cases := []struct {
		name string
		body string
	}{
		{"missing url and content", `{"action":"install"}`},
		{"both url and content", `{"action":"install","url":"https://example.com/x.md","content":"nope"}`},
		{"uppercase name", `{"action":"install","content":` + jsonContent("---\nname: GSync\ndescription: bad name.\n---\nbody\n") + `}`},
		{"name with space", `{"action":"install","content":` + jsonContent("---\nname: bad name\ndescription: bad name.\n---\nbody\n") + `}`},
		{"missing description", `{"action":"install","content":` + jsonContent("---\nname: gsync\n---\ndo things\n") + `}`},
		{"empty body", `{"action":"install","content":` + jsonContent("---\nname: gsync\ndescription: nothing.\n---\n") + `}`},
		{"unknown action", `{"action":"banana"}`},
	}
	for _, tc := range cases {
		if _, err := reg.Call(context.Background(), actor, "Skill", json.RawMessage(tc.body)); err == nil {
			t.Fatalf("%s: expected error", tc.name)
		}
	}
}

func TestSkillRemove(t *testing.T) {
	binder, fs, ex := skillBinder()
	fs.data[".roundpen/skills/gsync.md"] = []byte(gsyncSkillFile)

	// Cancel keeps the skill.
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, binder, nil)
	out, err := reg.Call(confirmCtx(t, "Cancel"), tools.Actor{Username: "alice"}, "Skill", json.RawMessage(`{"action":"remove","skill":"gsync"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "not removed") {
		t.Fatalf("out=%q", out)
	}

	// Confirm removes (exec runs rm).
	out, err = reg.Call(confirmCtx(t, "Remove"), tools.Actor{Username: "alice"}, "Skill", json.RawMessage(`{"action":"remove","skill":"gsync"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Removed skill \"gsync\"") {
		t.Fatalf("out=%q", out)
	}
	if !strings.Contains(strings.Join(ex.cmd, " "), "rm -f") {
		t.Fatalf("expected rm script, cmd=%q", ex.cmd)
	}

	// Built-in cannot be removed.
	_, err = reg.Call(context.Background(), tools.Actor{Username: "alice"}, "Skill", json.RawMessage(`{"action":"remove","skill":"commit"}`))
	if err == nil || !strings.Contains(err.Error(), "built-in") {
		t.Fatalf("expected built-in error, got %v", err)
	}

	// Unknown workspace skill.
	_, err = reg.Call(context.Background(), tools.Actor{Username: "alice"}, "Skill", json.RawMessage(`{"action":"remove","skill":"nope"}`))
	if err == nil {
		t.Fatal("expected not-installed error")
	}
}

func TestSkillRequiresBinder(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, nil, nil)
	if _, ok := reg.Get("Skill"); ok {
		t.Fatal("Skill must not register without a binder")
	}
}

func TestDefaultSkills(t *testing.T) {
	list := tools.DefaultSkills()
	if len(list) == 0 {
		t.Fatal("default skills are empty")
	}
	seen := map[string]bool{}
	for _, s := range list {
		if s.Name == "" || s.Description == "" || s.Prompt == "" {
			t.Fatalf("skill %+v missing fields", s)
		}
		seen[s.Name] = true
	}
	for _, want := range []string{"commit", "review", "fix", "summarize"} {
		if !seen[want] {
			t.Fatalf("missing default skill %q", want)
		}
	}
}

// jsonContent returns its argument as a JSON string literal for embedding.
func jsonContent(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
