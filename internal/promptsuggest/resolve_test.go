package promptsuggest

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/settings"
)

func TestFromConfigKindThenGlobal(t *testing.T) {
	p := settings.SuggestedPrompts{
		Global: []string{"global-a", "global-b"},
		ByKind: map[string][]string{
			"system": {"sys-1", "sys-2"},
		},
	}
	got := FromConfig(p, "system")
	if len(got) != 2 || got[0] != "sys-1" {
		t.Fatalf("kind resolve = %#v", got)
	}
	got = FromConfig(p, "user")
	if len(got) != 2 || got[0] != "global-a" {
		t.Fatalf("global fallback = %#v", got)
	}
	got = FromConfig(settings.SuggestedPrompts{}, "user")
	if len(got) != 0 {
		t.Fatalf("empty = %#v", got)
	}
}

func TestParsePrompts(t *testing.T) {
	list, err := parsePrompts(`["你好", "帮我整理文件", "看看最近任务"]`)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Fatalf("len=%d", len(list))
	}
	wrapped, err := parsePrompts("```json\n{\"prompts\":[\"a\",\"b\"]}\n```")
	if err != nil {
		t.Fatal(err)
	}
	if len(wrapped) != 2 || wrapped[0] != "a" {
		t.Fatalf("wrapped=%#v", wrapped)
	}
}
