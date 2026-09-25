package sysagent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
	"github.com/RoundpenAI/roundpen/internal/agentsession"
)

func TestProjectHistory_ClaudeShaped(t *testing.T) {
	toolMeta, _ := json.Marshal(agentsession.ToolMeta{
		Type:   "tool_call",
		ToolID: "call_1",
		Title:  "roundpen_list_sandboxes",
		Status: "completed",
		Input:  map[string]any{},
		Output: `{"sandboxes":[]}`,
	})
	errMeta, _ := json.Marshal(map[string]string{"type": "error"})
	rows := []*agentsession.Message{
		{Role: agentsession.RoleUser, Content: "list envs"},
		{Role: agentsession.RoleThought, Content: "I will call the API"},
		{Role: agentsession.RolePermission, Content: "allow"},
		{Role: agentsession.RoleTool, Content: "roundpen_list_sandboxes", Meta: toolMeta},
		{Role: agentsession.RoleAssistant, Content: "You have none."},
		{Role: agentsession.RoleEvent, Content: "boom", Meta: errMeta},
		{Role: agentsession.RoleUser, Content: "try again"},
	}
	got := projectHistory(rows)
	if formatRoles(got) != "user,assistant/1,tool,assistant,user,user" {
		t.Fatalf("roles: %s", formatRoles(got))
	}
	if got[0].Content != "list envs" || got[3].Content != "You have none." {
		t.Fatalf("text: %+v", got)
	}
	if got[1].ToolCalls[0].Function.Name != "roundpen_list_sandboxes" {
		t.Fatalf("tool name: %+v", got[1].ToolCalls)
	}
	if got[2].Role != "tool" || got[2].Content != `{"sandboxes":[]}` {
		t.Fatalf("tool result: %+v", got[2])
	}
	if !strings.HasPrefix(got[4].Content, "Previous turn error:") {
		t.Fatalf("error: %q", got[4].Content)
	}
}

func TestProjectHistory_SkipEmptyAssistant(t *testing.T) {
	got := projectHistory([]*agentsession.Message{
		{Role: agentsession.RoleUser, Content: "hi"},
		{Role: agentsession.RoleAssistant, Content: "(no response)"},
	})
	if formatRoles(got) != "user" {
		t.Fatalf("roles: %s", formatRoles(got))
	}
}

func TestEndsWithUser(t *testing.T) {
	msgs := []chatMessage{
		{Role: "system", Content: "s"},
		{Role: "user", Content: "hello"},
	}
	if !endsWithUser(msgs, "hello") {
		t.Fatal("expected last user")
	}
	if endsWithUser(msgs, "other") {
		t.Fatal("different text")
	}
}

func TestRestorePreamble_SameProjection(t *testing.T) {
	toolMeta, _ := json.Marshal(agentsession.ToolMeta{
		Type:   "tool_call",
		ToolID: "call_1",
		Title:  "roundpen_list_sandboxes",
		Status: "completed",
		Input:  map[string]any{},
		Output: `{"sandboxes":[]}`,
	})
	rows := []*agentsession.Message{
		{Role: agentsession.RoleUser, Content: "list envs"},
		{Role: agentsession.RoleThought, Content: "thinking"},
		{Role: agentsession.RoleTool, Content: "roundpen_list_sandboxes", Meta: toolMeta},
		{Role: agentsession.RoleAssistant, Content: "You have none."},
		{Role: agentsession.RoleUser, Content: "try again"},
	}
	got := RestorePreamble(rows, "try again")
	if !strings.Contains(got, restoreIntro) {
		t.Fatal("missing intro")
	}
	if !strings.Contains(got, "User: list envs") || !strings.Contains(got, "You called roundpen_list_sandboxes") {
		t.Fatalf("got %s", got)
	}
	if !strings.Contains(got, "Assistant: You have none.") {
		t.Fatalf("got %s", got)
	}
	if strings.Contains(got, "try again") || strings.Contains(got, "thinking") {
		t.Fatalf("should omit current user and thoughts: %s", got)
	}
	if RestorePreamble(rows[:1], "list envs") != "" {
		t.Fatal("only current user should yield empty preamble")
	}
}

func TestCompactHistory_DropsOldestTurns(t *testing.T) {
	var msgs []chatMessage
	for i := 0; i < 6; i++ {
		msgs = append(msgs,
			chatMessage{Role: "user", Content: strings.Repeat("u", 20)},
			chatMessage{Role: "assistant", Content: strings.Repeat("a", 20)},
		)
	}
	got := compactHistoryTo(msgs, 80)
	if got[0].Content != omittedNotice {
		t.Fatalf("notice: %q", got[0].Content)
	}
	if historyChars(got) > 80+len(omittedNotice) {
		t.Fatalf("still too big: %d", historyChars(got))
	}
}

func TestSystemPromptMentionsWebTools(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterWebSearch(reg, &tools.WebSearchBinder{APIKey: "k"})
	a := New(Deps{Tools: reg})
	msgs := a.buildPromptMessages(context.Background(), "hi")
	if len(msgs) == 0 || msgs[0].Role != "system" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	for _, want := range []string{"WebFetch", "WebSearch"} {
		if !strings.Contains(msgs[0].Content, want) {
			t.Fatalf("system prompt missing %q", want)
		}
	}
}

func TestProjectHistoryStopsAtClearMarker(t *testing.T) {
	clearMeta, _ := json.Marshal(map[string]string{"type": "clear"})
	toolMeta, _ := json.Marshal(agentsession.ToolMeta{
		Type:   "tool_call",
		ToolID: "call_old",
		Title:  "Bash",
		Status: "completed",
		Output: "old tool output",
	})
	rows := []*agentsession.Message{
		{Role: agentsession.RoleUser, Content: "old ask"},
		{Role: agentsession.RoleAssistant, Content: "old reply"},
		{Role: agentsession.RoleTool, Content: "Bash", Meta: toolMeta},
		{Role: agentsession.RoleEvent, Content: "上下文已清空", Meta: clearMeta},
		{Role: agentsession.RoleUser, Content: "new ask"},
		{Role: agentsession.RoleAssistant, Content: "new reply"},
	}
	got := projectHistory(rows)
	if formatRoles(got) != "user,assistant" {
		t.Fatalf("roles: %s", formatRoles(got))
	}
	if got[0].Content != "new ask" || got[1].Content != "new reply" {
		t.Fatalf("kept pre-clear text: %+v", got)
	}
}

func TestProjectHistoryKeepsEverythingWithoutMarker(t *testing.T) {
	rows := []*agentsession.Message{
		{Role: agentsession.RoleUser, Content: "one"},
		{Role: agentsession.RoleAssistant, Content: "two"},
	}
	if formatRoles(projectHistory(rows)) != "user,assistant" {
		t.Fatalf("roles: %s", formatRoles(projectHistory(rows)))
	}
}

func TestRestorePreambleHonorsClearMarker(t *testing.T) {
	clearMeta, _ := json.Marshal(map[string]string{"type": "clear"})
	rows := []*agentsession.Message{
		{Role: agentsession.RoleUser, Content: "old ask"},
		{Role: agentsession.RoleAssistant, Content: "old reply"},
		{Role: agentsession.RoleEvent, Content: "上下文已清空", Meta: clearMeta},
	}
	out := RestorePreamble(rows, "new ask")
	if strings.Contains(out, "old ask") || strings.Contains(out, "old reply") {
		t.Fatalf("pre-clear turns leaked into the stdio preamble: %q", out)
	}
}

func TestSystemPromptOmitsWebSearchWhenUnconfigured(t *testing.T) {
	a := New(Deps{})
	msgs := a.buildPromptMessages(context.Background(), "hi")
	if len(msgs) == 0 || msgs[0].Role != "system" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	if !strings.Contains(msgs[0].Content, "WebFetch") {
		t.Fatal("system prompt must still mention WebFetch")
	}
	if strings.Contains(msgs[0].Content, "WebSearch") {
		t.Fatalf("system prompt must not promise WebSearch when it is not registered: %q", msgs[0].Content)
	}
}

func TestProjectHistorySkipsCancelledRows(t *testing.T) {
	cancelled, _ := json.Marshal(map[string]string{"type": "user", "state": "cancelled"})
	normal, _ := json.Marshal(map[string]string{"type": "user"})
	rows := []*agentsession.Message{
		{Role: agentsession.RoleUser, Content: "keep me", Meta: normal},
		{Role: agentsession.RoleAssistant, Content: "ok"},
		{Role: agentsession.RoleUser, Content: "pulled back", Meta: cancelled},
		{Role: agentsession.RoleUser, Content: "next", Meta: normal},
	}
	got := projectHistory(rows)
	if formatRoles(got) != "user,assistant,user" {
		t.Fatalf("roles: %s", formatRoles(got))
	}
	for _, m := range got {
		if strings.Contains(m.Content, "pulled back") {
			t.Fatalf("cancelled row leaked into model context: %+v", got)
		}
	}

	// The same projection feeds the stdio restore preamble.
	out := RestorePreamble(rows, "current")
	if strings.Contains(out, "pulled back") {
		t.Fatalf("cancelled row leaked into the restore preamble: %q", out)
	}
}
