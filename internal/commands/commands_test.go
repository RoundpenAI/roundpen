package commands

import (
	"strings"
	"testing"
)

func TestActionsCoverClearAndHelp(t *testing.T) {
	acts := Actions()
	if len(acts) != 2 {
		t.Fatalf("want 2 action commands, got %d", len(acts))
	}
	byName := make(map[string]Command, len(acts))
	for _, c := range acts {
		byName[c.Name] = c
		if c.Kind != KindAction {
			t.Fatalf("%s: kind = %q, want %q", c.Name, c.Kind, KindAction)
		}
		if c.Source != SourceAction {
			t.Fatalf("%s: source = %q, want %q", c.Name, c.Source, SourceAction)
		}
		if strings.TrimSpace(c.Description) == "" {
			t.Fatalf("%s: description is empty", c.Name)
		}
		if c.Args {
			t.Fatalf("%s: action command must not advertise free-form args", c.Name)
		}
	}
	for _, want := range []string{"clear", "help"} {
		if _, ok := byName[want]; !ok {
			t.Fatalf("action command %q missing", want)
		}
	}
}

func TestSkillDisplayDescriptionCoversBuiltins(t *testing.T) {
	for _, name := range []string{"commit", "review", "fix", "summarize"} {
		desc, ok := SkillDisplayDescription(name)
		if !ok || strings.TrimSpace(desc) == "" {
			t.Fatalf("builtin skill %q: ok=%v desc=%q", name, ok, desc)
		}
	}
	if _, ok := SkillDisplayDescription("nope"); ok {
		t.Fatal("unknown skill must not resolve to a display description")
	}
	if len(skillDisplayDescriptions) != 4 {
		t.Fatalf("override table should only cover the 4 builtins, has %d entries", len(skillDisplayDescriptions))
	}
}

func TestHelpTextListsEveryCommand(t *testing.T) {
	cmds := append(Actions(),
		Command{Name: "review", Kind: KindSkill, Source: SourceBuiltin, Description: "审查最近改动"},
		Command{Name: "my-flow", Kind: KindSkill, Source: SourceInstalled, Description: "自定义流程", Args: true},
	)
	text := HelpText(cmds)
	for _, want := range []string{"/clear", "/help", "/review", "/my-flow", "审查最近改动", "自定义流程"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help text missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, "/my-flow [可带参数]") {
		t.Fatalf("help text should mark commands that accept args:\n%s", text)
	}
	if strings.Contains(text, "/review [可带参数]") {
		t.Fatalf("commands without args must not carry the args marker:\n%s", text)
	}
}

func TestHelpTextWithoutSkillsStillRenders(t *testing.T) {
	text := HelpText(Actions())
	if text == "" {
		t.Fatal("help text is empty for the action-only catalog")
	}
	if strings.Contains(text, "技能命令") {
		t.Fatalf("no skills passed, skill section should be omitted:\n%s", text)
	}
}
