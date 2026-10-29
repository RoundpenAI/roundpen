package issuescan

import (
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/issue"
)

func TestRenderPulsePromptPlaybook(t *testing.T) {
	it := &issue.Issue{
		Key: "ISS-9", Title: "配置超时", Status: issue.StatusNew,
		ProjectID: "proj-1", Summary: "可配置",
	}
	got := RenderPulsePrompt(it, PulseCreated, false)
	for _, want := range []string{"ISS-9", "Work Pulse", "new", "WriteIssueDoc", "proj-1", string(PulseCreated)} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
	busy := RenderPulsePrompt(it, PulseTick, true)
	if !strings.Contains(busy, "Worker slot is busy") {
		t.Fatalf("expected busy note:\n%s", busy)
	}
}

func TestRenderPulsePromptStale(t *testing.T) {
	it := &issue.Issue{
		Key: "ISS-9", Title: "配置超时", Status: issue.StatusRunning,
		ProjectID: "proj-1",
	}
	stale := []issue.Task{{
		Key:       "TSK-11",
		Title:     "实现超时",
		Status:    issue.TaskInProgress,
		UpdatedAt: time.Date(2026, 10, 29, 8, 0, 0, 0, time.UTC),
		Detail:    "卡在迁移",
	}}
	got := RenderPulsePrompt(it, PulseStale, false, stale)
	for _, want := range []string{
		string(PulseStale),
		"TSK-11",
		"in_progress",
		"卡在迁移",
		"project manager",
		"UpdateTask",
		"DelegateProjectWork",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestPulseDebounce(t *testing.T) {
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	r := &PulseRunner{
		Debounce: 12 * time.Minute,
		Now:      func() time.Time { return now },
		last: map[string]pulseStamp{
			"u|ISS-1": {at: now.Add(-5 * time.Minute), status: issue.StatusNew},
		},
	}
	it := &issue.Issue{Key: "ISS-1", Status: issue.StatusNew}
	if r.shouldPulse("u", it, PulseTick) {
		t.Fatal("tick within debounce should skip")
	}
	if !r.shouldPulse("u", it, PulseCreated) {
		t.Fatal("created always pulses")
	}
	if !r.shouldPulse("u", it, PulseWorkerDone) {
		t.Fatal("worker_done always pulses")
	}
	if !r.shouldPulse("u", it, PulseStale) {
		t.Fatal("stale always pulses")
	}
	if !r.shouldPulse("u", it, PulseTaskResume) {
		t.Fatal("task_resume always pulses")
	}
	it.Status = issue.StatusTriaged
	if !r.shouldPulse("u", it, PulseTick) {
		t.Fatal("status change should pulse")
	}
	r.last["u|ISS-1"] = pulseStamp{at: now.Add(-20 * time.Minute), status: issue.StatusNew}
	it.Status = issue.StatusNew
	if !r.shouldPulse("u", it, PulseTick) {
		t.Fatal("past debounce should pulse")
	}
}

func TestRenderPulsePromptTaskResume(t *testing.T) {
	it := &issue.Issue{Key: "ISS-9", Title: "配置超时", Status: issue.StatusRunning}
	got := RenderPulsePrompt(it, PulseTaskResume, false)
	for _, want := range []string{
		string(PulseTaskResume), "cleared a blocked TSK", "DelegateProjectWork", "ISS-9",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

func TestHasBlockedTask(t *testing.T) {
	if hasBlockedTask([]issue.Task{{Status: issue.TaskInProgress}}) {
		t.Fatal("in_progress is not blocked")
	}
	if !hasBlockedTask([]issue.Task{
		{Status: issue.TaskInProgress},
		{Status: issue.TaskBlocked},
	}) {
		t.Fatal("expected blocked")
	}
}
