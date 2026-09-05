package sysagent

import (
	"encoding/json"
	"strings"
	"testing"

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
