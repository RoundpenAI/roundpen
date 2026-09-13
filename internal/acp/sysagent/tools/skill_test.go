package tools_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

func TestSkillTool(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, tools.NewSkillCatalog(
		tools.Skill{Name: "plain", Description: "no params skill", Prompt: "Do the plain thing."},
		tools.Skill{Name: "paramed", Description: "params skill", Prompt: "Do the paramed thing.", Params: true},
	))

	if _, ok := reg.Get("Skill"); !ok {
		t.Fatal("missing Skill tool")
	}

	// Known skill without params.
	out, err := reg.Call(context.Background(), tools.Actor{}, "Skill", json.RawMessage(`{"skill":"plain"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Do the plain thing.") {
		t.Fatalf("out=%q", out)
	}
	if strings.Contains(out, "arguments:") {
		t.Fatalf("params skill must not attach args: %q", out)
	}

	// Known params skill with args.
	out, err = reg.Call(context.Background(), tools.Actor{}, "Skill", json.RawMessage(`{"skill":"paramed","args":"fix the parser"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Do the paramed thing.") || !strings.Contains(out, "fix the parser") {
		t.Fatalf("out=%q", out)
	}

	// Unknown skill lists the catalog.
	_, err = reg.Call(context.Background(), tools.Actor{}, "Skill", json.RawMessage(`{"skill":"nope"}`))
	if err == nil {
		t.Fatal("expected unknown-skill error")
	}
	if !strings.Contains(err.Error(), "plain") || !strings.Contains(err.Error(), "paramed") {
		t.Fatalf("err=%q", err)
	}

	// Missing skill argument.
	if _, err := reg.Call(context.Background(), tools.Actor{}, "Skill", json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error for missing skill")
	}
}

func TestSkillToolDegradesWithoutCatalog(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterSkill(reg, nil) // nil catalog → tool not registered
	if _, ok := reg.Get("Skill"); ok {
		t.Fatal("Skill must not register without a catalog")
	}
}

func TestDefaultSkillsCatalog(t *testing.T) {
	cat := tools.NewSkillCatalog(tools.DefaultSkills()...)
	list := cat.List()
	if len(list) == 0 {
		t.Fatal("default catalog is empty")
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
