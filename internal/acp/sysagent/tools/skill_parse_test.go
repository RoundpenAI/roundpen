package tools

import (
	"strings"
	"testing"
)

func TestParseSkillMultilineDescriptionLiteral(t *testing.T) {
	src := `---
name: release
description: |
  第一步：更新版本号。
  第二步：构建并推送镜像。
  第三步：写 changelog。
args: true
allowedTools:
- Bash
---
Run the release flow.
`
	s, body, err := parseSkillFile([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	want := "第一步：更新版本号。\n第二步：构建并推送镜像。\n第三步：写 changelog。"
	if s.Description != want {
		t.Fatalf("description=%q want %q", s.Description, want)
	}
	if !strings.Contains(body, "Run the release flow.") {
		t.Fatalf("body=%q", body)
	}

	// Round-trips through marshal with the block preserved.
	out := marshalSkillFile(s)
	if !strings.Contains(out, "description: |") {
		t.Fatalf("marshal should emit a literal block: %q", out)
	}
	s2, _, err := parseSkillFile([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if s2.Description != want {
		t.Fatalf("round-trip description=%q want %q", s2.Description, want)
	}
}

func TestParseSkillDescriptionFolded(t *testing.T) {
	src := `---
name: x
description: >
  这是一段
  折叠的文字。
---
body
`
	s, _, err := parseSkillFile([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if s.Description != "这是一段 折叠的文字。" {
		t.Fatalf("description=%q", s.Description)
	}
}

func TestParseSkillDescriptionQuotedAndPlain(t *testing.T) {
	for _, src := range []string{
		"---\nname: a\ndescription: single line.\n---\nbody\n",
		"---\nname: b\ndescription: \"quoted line\"\n---\nbody\n",
		"---\nname: c\ndescription: 'quoted line sigle'\n---\nbody\n",
	} {
		s, _, err := parseSkillFile([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if s.Description == "" {
			t.Fatalf("empty description for %q", src)
		}
	}
}
