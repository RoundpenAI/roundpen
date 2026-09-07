package sysagent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

const (
	loopRepeatRounds = 3
	loopCycleRounds  = 4
	loopWatchKeep    = 8
)

const (
	loopNudgeText        = "You are repeating the same tool calls without new information. Change approach, try a different control or URL, or write your final report now."
	loopStopText         = "Stopped: the same actions kept repeating without progress. Say what you still need, or try a different starting point."
	infraNudgeText       = "Browser sandbox CDP failed — guest Chrome on :9222 is not ready. Do not keep clicking page tools. Call roundpen_ensure_browser, wait for the VM, then retry browser_* once. If DevTools is still down, report that the Browser environment Chrome is not listening."
	infraStopText        = "Stopped: guest Chrome DevTools on :9222 is not reachable. Retrying page tools will not help. Start/resume the Browser environment and wait until CDP is up."
	agentAbsentNudgeText = "Cloud Agent is not started (status=absent). That is not a missing capability. Call roundpen_ensure_agent or sandbox_exec now. Do not stop after listing, and do not use the Browser slot for git or shell."
)

type toolCallRec struct {
	name   string
	args   string
	result string
}

type loopWatch struct {
	recent   []string
	nudged   bool
	infra    int
	lastKind string
}

func (w *loopWatch) observe(calls []toolCallRec) (nudge, stop bool) {
	if w == nil || len(calls) == 0 {
		return false, false
	}
	w.lastKind = ""
	if browserInfraRound(calls) {
		w.infra++
		w.lastKind = "infra"
		if w.infra == 1 {
			return true, false
		}
		return false, true
	}
	if agentAbsentOnly(calls) {
		w.lastKind = "agent"
		return true, false
	}
	sig := roundSignature(calls)
	w.recent = append(w.recent, sig)
	if len(w.recent) > loopWatchKeep {
		w.recent = append([]string(nil), w.recent[len(w.recent)-loopWatchKeep:]...)
	}
	if !roundStuck(w.recent) {
		return false, false
	}
	if !w.nudged {
		w.nudged = true
		w.recent = nil
		return true, false
	}
	return false, true
}

func (w *loopWatch) nudgeText() string {
	if w != nil && w.lastKind == "agent" {
		return agentAbsentNudgeText
	}
	if w != nil && w.infra > 0 {
		return infraNudgeText
	}
	return loopNudgeText
}

func (w *loopWatch) stopText() string {
	if w != nil && w.infra > 0 {
		return infraStopText
	}
	return loopStopText
}

func agentAbsentOnly(calls []toolCallRec) bool {
	if len(calls) != 1 || calls[0].name != "roundpen_list_environments" {
		return false
	}
	s := strings.ToLower(calls[0].result)
	return strings.Contains(s, `"slot":"agent"`) && strings.Contains(s, "absent")
}

func browserInfraRound(calls []toolCallRec) bool {
	for _, c := range calls {
		if strings.HasPrefix(c.name, "browser_") && tools.InfraKind(c.result) != "" {
			return true
		}
	}
	return false
}

func roundStuck(recent []string) bool {
	n := len(recent)
	if n >= loopRepeatRounds {
		a, b, c := recent[n-3], recent[n-2], recent[n-1]
		if a != "" && a == b && b == c {
			return true
		}
	}
	if n >= loopCycleRounds {
		a, b, c, d := recent[n-4], recent[n-3], recent[n-2], recent[n-1]
		if a != "" && a == c && b == d && a != b {
			return true
		}
	}
	return false
}

func roundSignature(calls []toolCallRec) string {
	parts := make([]string, 0, len(calls))
	for _, c := range calls {
		if kind := tools.InfraKind(c.result); kind != "" && strings.HasPrefix(c.name, "browser_") {
			parts = append(parts, "browser_infra\x1e"+kind)
			continue
		}
		parts = append(parts, c.name+"\x1e"+canonArgs(c.args)+"\x1e"+resultKey(c.result))
	}
	return strings.Join(parts, "\x1f")
}

func canonArgs(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "{}"
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	return stableJSON(v)
}

func stableJSON(v any) string {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			b.Write(kb)
			b.WriteByte(':')
			b.WriteString(stableJSON(t[k]))
		}
		b.WriteByte('}')
		return b.String()
	case []any:
		var b strings.Builder
		b.WriteByte('[')
		for i, item := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(stableJSON(item))
		}
		b.WriteByte(']')
		return b.String()
	default:
		out, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprint(t)
		}
		return string(out)
	}
}

func resultKey(s string) string {
	if len(s) <= 256 {
		return s
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8]) + ":" + fmt.Sprintf("%d", len(s))
}

const omittedToolResult = "[Earlier tool result omitted to fit context.]"

func shrinkOldToolResults(msgs []chatMessage, keepLast int) []chatMessage {
	if keepLast < 1 {
		keepLast = 2
	}
	var toolIdx []int
	for i, m := range msgs {
		if m.Role == "tool" {
			toolIdx = append(toolIdx, i)
		}
	}
	if len(toolIdx) <= keepLast {
		return msgs
	}
	cut := len(toolIdx) - keepLast
	out := append([]chatMessage(nil), msgs...)
	for _, i := range toolIdx[:cut] {
		if out[i].Content == omittedToolResult {
			continue
		}
		out[i].Content = omittedToolResult
	}
	return out
}
