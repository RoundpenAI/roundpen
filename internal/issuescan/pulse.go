package issuescan

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/RoundpenAI/roundpen/internal/issue"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// Work Pulse advances open issues without waiting for the user to go quiet.
// Board digest (Runner) remains the low-disturbance user-facing channel.
const (
	DefaultPulseTick     = 10 * time.Minute
	DefaultPulseDebounce = 12 * time.Minute
	// DefaultStaleTaskIdle is how long a TSK may sit in_progress/blocked before
	// the coordinator must investigate (matches DefaultRunningLease / WRK timeout).
	DefaultStaleTaskIdle = 2 * time.Hour
)

// PulseReason explains why a work pulse was requested.
type PulseReason string

const (
	PulseCreated     PulseReason = "created"
	PulseTransition  PulseReason = "transition"
	PulseTick        PulseReason = "tick"
	PulseWorkerDone  PulseReason = "worker_done"
	PulseStale       PulseReason = "stale"
	PulseTaskResume  PulseReason = "task_resume" // human cleared a blocked TSK
)

// PulseIssueStore loads issues for work pulses.
type PulseIssueStore interface {
	IssueStore
	GetIssue(ctx context.Context, userID, keyOrID string) (*issue.Issue, error)
	ListTasks(ctx context.Context, userID, keyOrID string) ([]issue.Task, error)
	ListStaleOpenTasks(ctx context.Context, userID string, olderThan time.Time, limit int) ([]issue.StaleOpenTask, error)
}

// ProjectPulseLookup resolves a project's coordinator session.
type ProjectPulseLookup interface {
	Get(ctx context.Context, userID, keyOrID string) (projectID, key, coordAssistantID, coordSessionID string, err error)
	SetCoordinatorSession(ctx context.Context, userID, keyOrID, sessionID string) error
}

// WorkerPulseLookup reports active workers linked to an issue (optional).
type WorkerPulseLookup interface {
	HasActiveWorkerForIssue(ctx context.Context, userID, issueID string) (bool, error)
	HasRunningWorker(ctx context.Context, userID string) (bool, error)
}

// PulseSessionStarter opens system or coordinator chats for a work pulse.
type PulseSessionStarter interface {
	StartSystemSession(ctx context.Context, user *storage.User, assistantID, title string) (sessionID string, err error)
	StartCoordinatorSession(ctx context.Context, user *storage.User, assistantID, title string) (sessionID string, err error)
	EnsureCoordinatorRuntime(ctx context.Context, user *storage.User, sessionID string) error
}

// TurnPrompter runs a pulse through the chat session runner (persist + SSE).
type TurnPrompter interface {
	PromptTurn(ctx context.Context, user *storage.User, sessionID, text string) error
}

type pulseStamp struct {
	at     time.Time
	status string
}

// PulseRunner wakes Coordinator (or system assistant) to advance one issue.
type PulseRunner struct {
	Users      UserLister
	UserGet    UsersByID
	Assistants Assistants
	Issues     PulseIssueStore
	Projects   ProjectPulseLookup
	Workers    WorkerPulseLookup
	Starter    PulseSessionStarter
	ACP        Prompter
	Turns      TurnPrompter // preferred over ACP so the pulse appears in chat
	Log        *slog.Logger

	Tick          time.Duration
	Debounce      time.Duration
	StaleTaskIdle time.Duration
	Now           func() time.Time

	mu       sync.Mutex
	inflight map[string]struct{} // userID
	last     map[string]pulseStamp
}

// Loop periodically pulses actionable open issues.
func (r *PulseRunner) Loop(ctx context.Context) {
	tick := r.Tick
	if tick <= 0 {
		tick = DefaultPulseTick
	}
	// PromptTurn dials the local llmgw on :19001; wait until the HTTP
	// listener is up (Loop is started before ListenAndServe).
	select {
	case <-ctx.Done():
		return
	case <-time.After(3 * time.Second):
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	r.TickOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			r.TickOnce(ctx)
		}
	}
}

// TickOnce scans every user for actionable issues and enqueues pulses.
func (r *PulseRunner) TickOnce(ctx context.Context) {
	if r == nil || r.Users == nil || r.Issues == nil {
		return
	}
	users, err := r.Users.ListAll(ctx)
	if err != nil {
		r.log().Error("work pulse list users", "err", err)
		return
	}
	for _, u := range users {
		userID := strings.TrimSpace(u.Username)
		if userID == "" {
			continue
		}
		r.pulseUserActionable(ctx, userID)
	}
}

func (r *PulseRunner) pulseUserActionable(ctx context.Context, userID string) {
	// Stale tasks first: coordinator must unblock before soft column nudges.
	r.pulseUserStaleTasks(ctx, userID)
	for _, st := range actionablePulseStatuses() {
		rows, err := r.Issues.List(ctx, userID, issue.ListFilter{Status: st, Limit: 20})
		if err != nil {
			r.log().Warn("work pulse list", "user", userID, "status", st, "err", err)
			continue
		}
		for _, it := range rows {
			r.Enqueue(ctx, userID, it.Key, PulseTick)
		}
	}
}

func (r *PulseRunner) pulseUserStaleTasks(ctx context.Context, userID string) {
	idle := r.StaleTaskIdle
	if idle <= 0 {
		idle = DefaultStaleTaskIdle
	}
	rows, err := r.Issues.ListStaleOpenTasks(ctx, userID, r.now().Add(-idle), 20)
	if err != nil {
		r.log().Warn("work pulse stale tasks", "user", userID, "err", err)
		return
	}
	seen := map[string]struct{}{}
	for _, row := range rows {
		key := strings.TrimSpace(row.IssueKey)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		r.Enqueue(ctx, userID, key, PulseStale)
	}
}

func actionablePulseStatuses() []string {
	return []string{
		issue.StatusNew,
		issue.StatusSpecReview,
		issue.StatusNeedsDiscussion,
		issue.StatusTriaged,
		issue.StatusClaimed,
		issue.StatusRunning,
		issue.StatusVerified,
		issue.StatusNeedsReview,
		issue.StatusFailed,
	}
}

// Enqueue schedules a work pulse for one issue (non-blocking).
func (r *PulseRunner) Enqueue(ctx context.Context, userID, issueKey string, reason PulseReason) {
	if r == nil || r.Issues == nil || strings.TrimSpace(userID) == "" || strings.TrimSpace(issueKey) == "" {
		return
	}
	go r.runOne(context.WithoutCancel(ctx), userID, issueKey, reason)
}

func (r *PulseRunner) runOne(ctx context.Context, userID, issueKey string, reason PulseReason) {
	if !r.tryBegin(userID) {
		r.log().Info("work pulse skipped: user busy", "user", userID, "key", issueKey, "reason", reason)
		return
	}
	defer r.end(userID)

	it, err := r.Issues.GetIssue(ctx, userID, issueKey)
	if err != nil || it == nil {
		if err != nil {
			r.log().Warn("work pulse load issue", "user", userID, "key", issueKey, "err", err)
		}
		return
	}
	if issue.IsClosedStatus(it.Status) {
		return
	}
	if !r.shouldPulse(userID, it, reason) {
		return
	}

	staleTasks := r.loadStaleTasksForIssue(ctx, userID, it, reason)

	// Claimed/running with an active worker: wait for worker_done — unless a
	// stale blocked TSK needs the coordinator to unblock / replan.
	if (it.Status == issue.StatusClaimed || it.Status == issue.StatusRunning) &&
		reason != PulseWorkerDone && r.Workers != nil && it.ID != "" {
		if ok, err := r.Workers.HasActiveWorkerForIssue(ctx, userID, it.ID); err == nil && ok {
			if reason != PulseStale || !hasBlockedTask(staleTasks) {
				r.log().Info("work pulse skipped: active worker", "user", userID, "key", it.Key, "reason", reason)
				r.markPulsed(userID, it)
				return
			}
		}
	}

	sessionID, err := r.resolveSession(ctx, userID, it)
	if err != nil || sessionID == "" {
		r.log().Warn("work pulse session", "user", userID, "key", it.Key, "err", err)
		return
	}

	slotBusy := false
	if r.Workers != nil {
		if ok, err := r.Workers.HasRunningWorker(ctx, userID); err == nil {
			slotBusy = ok
		}
	}

	user, err := r.UserGet.GetByUsername(ctx, userID)
	if err != nil || user == nil {
		r.log().Warn("work pulse user", "user", userID, "err", err)
		return
	}
	text := RenderPulsePrompt(it, reason, slotBusy, staleTasks)
	if r.Turns != nil {
		err = r.Turns.PromptTurn(ctx, user, sessionID, text)
	} else if r.ACP != nil {
		err = r.ACP.PromptBoard(ctx, sessionID, text)
	} else {
		err = fmt.Errorf("no prompt backend")
	}
	if err != nil {
		r.log().Warn("work pulse prompt", "user", userID, "key", it.Key, "err", err)
		return
	}
	r.markPulsed(userID, it)
	r.log().Info("work pulse sent", "user", userID, "key", it.Key, "status", it.Status, "reason", reason)
}

func (r *PulseRunner) resolveSession(ctx context.Context, userID string, it *issue.Issue) (string, error) {
	if r.Starter == nil || r.UserGet == nil || r.Assistants == nil || (r.ACP == nil && r.Turns == nil) {
		return "", fmt.Errorf("work pulse wiring incomplete")
	}
	user, err := r.UserGet.GetByUsername(ctx, userID)
	if err != nil || user == nil {
		return "", fmt.Errorf("user not found")
	}

	if pid := strings.TrimSpace(it.ProjectID); pid != "" && r.Projects != nil {
		_, key, coordAsst, coordSess, err := r.Projects.Get(ctx, userID, pid)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(coordSess) != "" {
			_ = r.Starter.EnsureCoordinatorRuntime(ctx, user, coordSess)
			return coordSess, nil
		}
		if strings.TrimSpace(coordAsst) == "" {
			return "", fmt.Errorf("project has no coordinator")
		}
		sid, err := r.Starter.StartCoordinatorSession(ctx, user, coordAsst, "事项跟进")
		if err != nil {
			return "", err
		}
		_ = r.Projects.SetCoordinatorSession(ctx, userID, key, sid)
		return sid, nil
	}

	a, err := r.Assistants.EnsureSystem(ctx, userID)
	if err != nil {
		return "", err
	}
	return r.Starter.StartSystemSession(ctx, user, a.ID, "事项跟进")
}

func (r *PulseRunner) loadStaleTasksForIssue(ctx context.Context, userID string, it *issue.Issue, reason PulseReason) []issue.Task {
	if reason != PulseStale || it == nil {
		return nil
	}
	idle := r.StaleTaskIdle
	if idle <= 0 {
		idle = DefaultStaleTaskIdle
	}
	cutoff := r.now().Add(-idle)
	tasks, err := r.Issues.ListTasks(ctx, userID, it.Key)
	if err != nil {
		r.log().Warn("work pulse list tasks", "user", userID, "key", it.Key, "err", err)
		return nil
	}
	out := make([]issue.Task, 0, len(tasks))
	for _, tk := range tasks {
		if tk.Status != issue.TaskInProgress && tk.Status != issue.TaskBlocked {
			continue
		}
		if tk.UpdatedAt.After(cutoff) {
			continue
		}
		out = append(out, tk)
	}
	return out
}

func hasBlockedTask(tasks []issue.Task) bool {
	for _, tk := range tasks {
		if tk.Status == issue.TaskBlocked {
			return true
		}
	}
	return false
}

func (r *PulseRunner) shouldPulse(userID string, it *issue.Issue, reason PulseReason) bool {
	if reason == PulseCreated || reason == PulseWorkerDone || reason == PulseTransition ||
		reason == PulseStale || reason == PulseTaskResume {
		return true
	}
	debounce := r.Debounce
	if debounce <= 0 {
		debounce = DefaultPulseDebounce
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.last == nil {
		return true
	}
	st, ok := r.last[pulseKey(userID, it.Key)]
	if !ok {
		return true
	}
	if st.status != it.Status {
		return true
	}
	return r.now().Sub(st.at) >= debounce
}

func (r *PulseRunner) markPulsed(userID string, it *issue.Issue) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.last == nil {
		r.last = map[string]pulseStamp{}
	}
	r.last[pulseKey(userID, it.Key)] = pulseStamp{at: r.now(), status: it.Status}
}

func (r *PulseRunner) tryBegin(userID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.inflight == nil {
		r.inflight = map[string]struct{}{}
	}
	if _, ok := r.inflight[userID]; ok {
		return false
	}
	r.inflight[userID] = struct{}{}
	return true
}

func (r *PulseRunner) end(userID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.inflight, userID)
}

func pulseKey(userID, issueKey string) string {
	return userID + "|" + issueKey
}

func (r *PulseRunner) now() time.Time {
	if r != nil && r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *PulseRunner) log() *slog.Logger {
	if r != nil && r.Log != nil {
		return r.Log
	}
	return slog.Default()
}

// RenderPulsePrompt builds the agent turn that advances one issue.
func RenderPulsePrompt(it *issue.Issue, reason PulseReason, slotBusy bool, staleTasks ...[]issue.Task) string {
	if it == nil {
		return ""
	}
	var stale []issue.Task
	if len(staleTasks) > 0 {
		stale = staleTasks[0]
	}
	var b strings.Builder
	b.WriteString("[Work Pulse] Autonomy: advance this 事项 with tools now. Do NOT only acknowledge.\n")
	b.WriteString("Reason: ")
	b.WriteString(string(reason))
	b.WriteString("\n")
	b.WriteString("Issue: ")
	b.WriteString(it.Key)
	b.WriteString(" · ")
	b.WriteString(strings.TrimSpace(it.Title))
	b.WriteString("\nStatus: ")
	b.WriteString(it.Status)
	if it.ClaimOwner != "" {
		b.WriteString("\nclaim_owner: ")
		b.WriteString(it.ClaimOwner)
	}
	if it.ProjectID != "" {
		b.WriteString("\nproject_id: ")
		b.WriteString(it.ProjectID)
		b.WriteString(" (use GetProject / projectKey when delegating)")
	}
	if strings.TrimSpace(it.Summary) != "" {
		b.WriteString("\nSummary: ")
		b.WriteString(strings.TrimSpace(it.Summary))
	}
	if len(stale) > 0 {
		b.WriteString("\n\nStale tasks (idle too long — you own unblocking them):\n")
		for _, tk := range stale {
			b.WriteString("- ")
			b.WriteString(tk.Key)
			b.WriteString(" · ")
			b.WriteString(tk.Status)
			b.WriteString(" · ")
			b.WriteString(strings.TrimSpace(tk.Title))
			b.WriteString(" · updated ")
			b.WriteString(tk.UpdatedAt.UTC().Format(time.RFC3339))
			if strings.TrimSpace(tk.Detail) != "" {
				b.WriteString("\n  detail: ")
				b.WriteString(strings.TrimSpace(tk.Detail))
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("\nPlaybook for status ")
	b.WriteString(it.Status)
	b.WriteString(":\n")
	switch reason {
	case PulseStale:
		b.WriteString(pulseStalePlaybook(slotBusy))
	case PulseTaskResume:
		b.WriteString(pulseTaskResumePlaybook(slotBusy))
	default:
		b.WriteString(pulsePlaybook(it.Status, slotBusy))
	}
	b.WriteString("\nAlways TransitionIssue with a clear reason when changing status. ")
	b.WriteString("Respect claim_owner. AskUserQuestion only for real product decisions or blockers.\n")
	return b.String()
}

func pulseStalePlaybook(slotBusy bool) string {
	busyNote := ""
	if slotBusy {
		busyNote = " Worker slot is busy: plan/unblock/UpdateTask now; queue DelegateProjectWork only when the slot frees.\n"
	}
	return "- You are the project manager: intermediate states must not linger. Investigate and push.\n" +
		"- GetIssue + ListProjectWorkers. Read stale TSK detail / blocker notes.\n" +
		"- If blocked: clear the blocker (AskUserQuestion once if only the human can), then UpdateTask → in_progress or re-scope, and DelegateProjectWork if slot free.\n" +
		"- If in_progress with no live WRK: DelegateProjectWork (issueKey set) or UpdateTask with a concrete next step; do not leave it idle.\n" +
		"- If a WRK failed/cancelled: learn why, TransitionIssue → triaged/failed as appropriate, then re-delegate.\n" +
		"- Do NOT only acknowledge. Do NOT wait for the user to notice.\n" + busyNote
}

func pulseTaskResumePlaybook(slotBusy bool) string {
	busyNote := ""
	if slotBusy {
		busyNote = " Worker slot is busy: wait for it; do not stack another DelegateProjectWork.\n"
	}
	return "- Human cleared a blocked TSK (now in_progress). A continue turn may already be on the linked worker session.\n" +
		"- GetIssue + list tasks. Confirm the unblocked TSK has a live WRK or an in-flight continue.\n" +
		"- If no live worker and the slot is free → DelegateProjectWork (same role as before, issueKey set) so verification/build actually runs.\n" +
		"- Do NOT mark the issue verified until evidence lands. Do NOT only acknowledge.\n" + busyNote
}

func pulsePlaybook(status string, slotBusy bool) string {
	busyNote := ""
	if slotBusy {
		busyNote = " Worker slot is busy (another WRK running): claim/plan only — do NOT DelegateProjectWork until the slot frees; say so briefly.\n"
	}
	switch status {
	case issue.StatusNew:
		return "- Light research (Read/Grep/WebFetch as needed).\n" +
			"- WriteIssueDoc kind=spec when you understand the ask.\n" +
			"- If scope is clear → TransitionIssue to spec_review or triaged; if contradictory → AskUserQuestion once.\n" + busyNote
	case issue.StatusSpecReview, issue.StatusNeedsDiscussion:
		return "- Tighten the spec or ask one clarifying question.\n" +
			"- Prefer TransitionIssue → triaged when ready; do not ping-pong discussion forever.\n" + busyNote
	case issue.StatusTriaged:
		return "- TransitionIssue → claimed (you are claim_owner).\n" +
			"- WriteIssueDoc kind=plan + CreateTask for steps.\n" +
			"- Then DelegateProjectWork (implementer) with issueKey set, unless the worker slot is busy.\n" + busyNote
	case issue.StatusClaimed, issue.StatusRunning:
		return "- Ensure an active WRK is linked (ListProjectWorkers). If none and slot free → DelegateProjectWork.\n" +
			"- Keep TSK status current (UpdateTask). If a TSK is blocked or idle, unblock or re-delegate — do not leave middle states.\n" +
			"- If a worker is already running, wait — do not steal the claim.\n" +
			"- On blockage (credentials/env), AskUserQuestion and stop.\n" + busyNote
	case issue.StatusVerified, issue.StatusNeedsReview:
		return "- Delegate reviewer/tester if verification is thin; otherwise TransitionIssue → closed and summarize for the user.\n" + busyNote
	case issue.StatusFailed:
		return "- Read failure context; if fixable → TransitionIssue → triaged and re-delegate.\n" +
			"- If stuck twice, AskUserQuestion with the blocker.\n" + busyNote
	default:
		return "- Inspect ListIssueBoard for this key and take the next legal TransitionIssue / DelegateProjectWork step.\n" + busyNote
	}
}

// IssueStoreAdapter wraps issue.Store for PulseIssueStore.
type IssueStoreAdapter struct {
	*issue.Store
}

func (a IssueStoreAdapter) GetIssue(ctx context.Context, userID, keyOrID string) (*issue.Issue, error) {
	it, _, _, err := a.Store.Get(ctx, userID, keyOrID)
	return it, err
}

func (a IssueStoreAdapter) ListTasks(ctx context.Context, userID, keyOrID string) ([]issue.Task, error) {
	return a.Store.ListTasks(ctx, userID, keyOrID)
}

func (a IssueStoreAdapter) ListStaleOpenTasks(ctx context.Context, userID string, olderThan time.Time, limit int) ([]issue.StaleOpenTask, error) {
	return a.Store.ListStaleOpenTasks(ctx, userID, olderThan, limit)
}

var _ PulseIssueStore = IssueStoreAdapter{}
