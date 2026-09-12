package sysagent

import "testing"

func TestLoopWatch_repeatThenNudgeThenStop(t *testing.T) {
	var w loopWatch
	same := []toolCallRec{{name: "browser_click", args: `{"ref":"e1"}`, result: `{"ok":true}`}}
	for i := 0; i < 2; i++ {
		nudge, stop := w.observe(same)
		if nudge || stop {
			t.Fatalf("round %d: nudge=%v stop=%v", i+1, nudge, stop)
		}
	}
	nudge, stop := w.observe(same)
	if !nudge || stop {
		t.Fatalf("third identical: nudge=%v stop=%v", nudge, stop)
	}
	// After a nudge, two more identical rounds are not enough; the third is.
	if nudge, stop = w.observe(same); nudge || stop {
		t.Fatalf("first after nudge: nudge=%v stop=%v", nudge, stop)
	}
	if nudge, stop = w.observe(same); nudge || stop {
		t.Fatalf("second after nudge: nudge=%v stop=%v", nudge, stop)
	}
	if nudge, stop = w.observe(same); nudge || !stop {
		t.Fatalf("third after nudge: nudge=%v stop=%v", nudge, stop)
	}
}

func TestLoopWatch_ABAB(t *testing.T) {
	var w loopWatch
	a := []toolCallRec{{name: "browser_navigate", args: `{"url":"http://x/a"}`, result: `{"ok":true}`}}
	b := []toolCallRec{{name: "browser_navigate", args: `{"url":"http://x/b"}`, result: `{"ok":true}`}}
	seq := [][]toolCallRec{a, b, a, b}
	for i, rec := range seq {
		nudge, stop := w.observe(rec)
		if i < 3 && (nudge || stop) {
			t.Fatalf("step %d: nudge=%v stop=%v", i, nudge, stop)
		}
		if i == 3 && (!nudge || stop) {
			t.Fatalf("ABAB should nudge: nudge=%v stop=%v", nudge, stop)
		}
	}
}

func TestLoopWatch_progressIsNotStuck(t *testing.T) {
	var w loopWatch
	for i := 0; i < 6; i++ {
		nudge, stop := w.observe([]toolCallRec{{
			name:   "browser_click",
			args:   `{"ref":"e1"}`,
			result: `{"url":"http://x/` + string(rune('a'+i)) + `"}`,
		}})
		if nudge || stop {
			t.Fatalf("changing results should continue: i=%d", i)
		}
	}
}

func TestCanonArgsStableKeyOrder(t *testing.T) {
	if canonArgs(`{"b":1,"a":2}`) != canonArgs(`{"a":2,"b":1}`) {
		t.Fatal(canonArgs(`{"b":1,"a":2}`), canonArgs(`{"a":2,"b":1}`))
	}
}

func TestLoopWatch_agentAbsentAfterListNudges(t *testing.T) {
	var w loopWatch
	list := []toolCallRec{{
		name:   "ListEnvironments",
		args:   `{}`,
		result: `{"environments":[{"slot":"agent","status":"absent"},{"slot":"browser","status":"running"}],"note":"call Bash or file tools"}`,
	}}
	nudge, stop := w.observe(list)
	if !nudge || stop {
		t.Fatalf("absent agent: nudge=%v stop=%v", nudge, stop)
	}
	if w.nudgeText() != agentAbsentNudgeText {
		t.Fatalf("nudge=%q", w.nudgeText())
	}
}

func TestLoopWatch_cdpInfraStopsOnSecondBrowserTool(t *testing.T) {
	var w loopWatch
	fail := []toolCallRec{{
		name:   "browser_snapshot",
		args:   `{}`,
		result: `error: env cdp (nothing listening on guest :9222): cdp attach: Get "http://127.0.0.1:40495/json/version": read tcp 127.0.0.1:50616->127.0.0.1:40495: read: connection reset by peer`,
	}}
	fail2 := []toolCallRec{{
		name:   "browser_explore",
		args:   `{"url":"http://127.0.0.1:19000"}`,
		result: `error: env cdp (nothing listening on guest :9222): cdp attach: Get "http://127.0.0.1:41111/json/version": connection reset by peer`,
	}}
	nudge, stop := w.observe(fail)
	if !nudge || stop {
		t.Fatalf("first cdp fail: nudge=%v stop=%v", nudge, stop)
	}
	if w.nudgeText() != infraNudgeText {
		t.Fatalf("nudge=%q", w.nudgeText())
	}
	// Diagnosing with a non-browser tool must not reset the infra strike.
	if nudge, stop = w.observe([]toolCallRec{{name: "ListEnvironments", args: `{}`, result: `{}`}}); nudge || stop {
		t.Fatalf("list envs: nudge=%v stop=%v", nudge, stop)
	}
	if nudge, stop = w.observe(fail2); nudge || !stop {
		t.Fatalf("second cdp fail: nudge=%v stop=%v", nudge, stop)
	}
	if w.stopText() != infraStopText {
		t.Fatalf("stop=%q", w.stopText())
	}
}

func TestShrinkOldToolResults(t *testing.T) {
	msgs := []chatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "go"},
		{Role: "tool", Content: "old-1"},
		{Role: "tool", Content: "old-2"},
		{Role: "tool", Content: "keep-1"},
		{Role: "tool", Content: "keep-2"},
	}
	got := shrinkOldToolResults(msgs, 2)
	if got[2].Content != omittedToolResult || got[3].Content != omittedToolResult {
		t.Fatalf("old tools: %#v", got)
	}
	if got[4].Content != "keep-1" || got[5].Content != "keep-2" {
		t.Fatalf("kept: %#v", got)
	}
}
