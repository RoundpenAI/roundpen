package sysagent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/agentsession"
)

// MessageSource loads persisted chat rows for prompt replay.
type MessageSource interface {
	ListMessages(ctx context.Context, sessionID string, limit int) ([]*agentsession.Message, error)
}

const (
	historyLoadLimit   = 2000
	maxHistoryChars    = 80_000
	minKeepTurns       = 2
	omittedNotice      = "[Earlier conversation omitted to fit context.]"
	interruptedResult  = "interrupted"
	noResponseSentinel = "(no response)"
)

// buildPromptMessages is the Claude Code-shaped replay:
// system + prior user/assistant/tool pairs from the database + this turn's user text.
func (a *Agent) buildPromptMessages(ctx context.Context, userText string) []chatMessage {
	system := `You are Roundpen System Agent. You help the signed-in user manage Roundpen resources they are allowed to access.
Use tools for factual actions (list/create/delete sandboxes, templates, sessions, settings, browser).
Do not invent sandbox ids or API results. Prefer concise answers.
Prior user messages, your replies, and tool calls/results are included when this session has history.`

	out := []chatMessage{{Role: "system", Content: system}}
	if a.deps.History != nil && a.deps.SessionID != "" {
		rows, err := a.deps.History.ListMessages(ctx, a.deps.SessionID, historyLoadLimit)
		if err == nil {
			out = append(out, compactHistory(projectHistory(rows))...)
		}
	}
	if !endsWithUser(out, userText) {
		out = append(out, chatMessage{Role: "user", Content: userText})
	}
	return out
}

func endsWithUser(msgs []chatMessage, userText string) bool {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "system" {
			continue
		}
		return msgs[i].Role == "user" && msgs[i].Content == userText
	}
	return false
}

func projectHistory(rows []*agentsession.Message) []chatMessage {
	var out []chatMessage
	var pending []*agentsession.Message
	flushTools := func() {
		if len(pending) == 0 {
			return
		}
		var calls []toolCall
		var results []chatMessage
		for _, m := range pending {
			meta := parseToolMeta(m)
			id := strings.TrimSpace(meta.ToolID)
			if id == "" {
				id = m.ID
			}
			name := toolName(meta, m.Content)
			calls = append(calls, toolCall{
				ID:   id,
				Type: "function",
				Function: toolCallFn{
					Name:      name,
					Arguments: jsonArgs(meta.Input),
				},
			})
			results = append(results, chatMessage{
				Role:       "tool",
				ToolCallID: id,
				Name:       name,
				Content:    toolResult(meta, m.Content),
			})
		}
		pending = nil
		if len(calls) == 0 {
			return
		}
		out = append(out, chatMessage{Role: "assistant", ToolCalls: calls})
		out = append(out, results...)
	}

	for _, m := range rows {
		if m == nil {
			continue
		}
		switch m.Role {
		case agentsession.RoleUser:
			flushTools()
			if text := strings.TrimSpace(m.Content); text != "" {
				out = append(out, chatMessage{Role: "user", Content: text})
			}
		case agentsession.RoleAssistant:
			flushTools()
			text := strings.TrimSpace(m.Content)
			if text == "" || text == noResponseSentinel {
				continue
			}
			out = append(out, chatMessage{Role: "assistant", Content: text})
		case agentsession.RoleTool:
			pending = append(pending, m)
		case agentsession.RoleEvent:
			flushTools()
			if metaType(m) == "error" && strings.TrimSpace(m.Content) != "" {
				out = append(out, chatMessage{
					Role:    "user",
					Content: "Previous turn error: " + strings.TrimSpace(m.Content),
				})
			}
		default:
			// thought / permission: UI + audit only (Claude Code does not replay old thinking).
		}
	}
	flushTools()
	return out
}

func compactHistory(msgs []chatMessage) []chatMessage {
	return compactHistoryTo(msgs, maxHistoryChars)
}

func compactHistoryTo(msgs []chatMessage, budget int) []chatMessage {
	if historyChars(msgs) <= budget {
		return msgs
	}
	turns := splitTurns(msgs)
	if len(turns) <= minKeepTurns {
		return msgs
	}
	drop := 0
	for drop < len(turns)-minKeepTurns && historyChars(joinTurns(turns[drop:])) > budget {
		drop++
	}
	if drop == 0 {
		return msgs
	}
	kept := joinTurns(turns[drop:])
	return append([]chatMessage{{Role: "user", Content: omittedNotice}}, kept...)
}

func splitTurns(msgs []chatMessage) [][]chatMessage {
	var turns [][]chatMessage
	var cur []chatMessage
	for _, m := range msgs {
		if m.Role == "user" && len(cur) > 0 {
			turns = append(turns, cur)
			cur = nil
		}
		cur = append(cur, m)
	}
	if len(cur) > 0 {
		turns = append(turns, cur)
	}
	return turns
}

func joinTurns(turns [][]chatMessage) []chatMessage {
	var out []chatMessage
	for _, t := range turns {
		out = append(out, t...)
	}
	return out
}

func historyChars(msgs []chatMessage) int {
	n := 0
	for _, m := range msgs {
		n += len(m.Content) + len(m.Name) + len(m.ToolCallID)
		for _, tc := range m.ToolCalls {
			n += len(tc.ID) + len(tc.Function.Name) + len(tc.Function.Arguments)
		}
	}
	return n
}

func parseToolMeta(m *agentsession.Message) agentsession.ToolMeta {
	var meta agentsession.ToolMeta
	if m != nil && len(m.Meta) > 0 {
		_ = json.Unmarshal(m.Meta, &meta)
	}
	return meta
}

func metaType(m *agentsession.Message) string {
	if m == nil || len(m.Meta) == 0 {
		return ""
	}
	var wrap struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(m.Meta, &wrap)
	return wrap.Type
}

func toolName(meta agentsession.ToolMeta, content string) string {
	if n := strings.TrimSpace(meta.Title); n != "" {
		return n
	}
	if n := strings.TrimSpace(content); n != "" {
		return n
	}
	if n := strings.TrimSpace(meta.ToolID); n != "" {
		return n
	}
	return "tool"
}

func jsonArgs(v any) string {
	if v == nil {
		return "{}"
	}
	if s, ok := v.(string); ok {
		if json.Valid([]byte(s)) {
			return s
		}
		b, err := json.Marshal(s)
		if err != nil {
			return "{}"
		}
		return string(b)
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func toolResult(meta agentsession.ToolMeta, content string) string {
	if meta.Output != nil {
		switch t := meta.Output.(type) {
		case string:
			if t != "" {
				return t
			}
		default:
			b, err := json.Marshal(t)
			if err == nil {
				return string(b)
			}
		}
	}
	st := strings.ToLower(strings.TrimSpace(meta.Status))
	if st == "pending" || st == "in_progress" || st == "running" {
		return interruptedResult
	}
	if s := strings.TrimSpace(content); s != "" {
		return s
	}
	return interruptedResult
}

// used by tests to format a debug dump
func formatRoles(msgs []chatMessage) string {
	var b strings.Builder
	for i, m := range msgs {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%s", m.Role)
		if len(m.ToolCalls) > 0 {
			fmt.Fprintf(&b, "/%d", len(m.ToolCalls))
		}
	}
	return b.String()
}
