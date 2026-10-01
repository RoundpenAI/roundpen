package routine

import (
	"context"
	"errors"
	"log/slog"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/assistant"
	"github.com/RoundpenAI/roundpen/internal/assistticket"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

// SessionStarter opens the execution session for one run.
type SessionStarter interface {
	StartRoutine(ctx context.Context, user *storage.User, assistantID, title, runKey, autonomy string) (sessionID string, err error)
	ResumeRoutine(ctx context.Context, user *storage.User, sessionID, runKey, autonomy string) error
}

// Prompter sends a turn to a live session.
type Prompter interface {
	Prompt(ctx context.Context, sessionID, text string) (acp.StopReason, error)
	Cancel(ctx context.Context, sessionID string) error
}

// UserLookup loads the account a run acts as.
type UserLookup interface {
	GetByUsername(ctx context.Context, username string) (*storage.User, error)
}

// EnvReady makes sure the user's agent and browser slots are up.
type EnvReady interface {
	EnsureAgent(ctx context.Context, userID string) (*sandbox.Sandbox, error)
	EnsureBrowser(ctx context.Context, userID string) (*userenv.BrowserTarget, error)
}

// Runner claims due routines and executes them.
type Runner struct {
	Store      *Store
	Users      UserLookup
	Assistants *assistant.Store
	Envs       EnvReady
	Starter    SessionStarter
	ACP        Prompter
	Sessions   *agentsession.Store
	Notify     func(ctx context.Context, assistantID, text string) error
	Log        *slog.Logger
	Tick       time.Duration
	// BudgetLimit caps a run's wall clock in tests. Zero uses the routine's max duration.
	BudgetLimit time.Duration
	// WaitLimit is how long a confirm gate may sit. Zero means 24 hours.
	WaitLimit time.Duration
}

// Loop polls until ctx is cancelled.
func (r *Runner) Loop(ctx context.Context) {
	tick := r.Tick
	if tick <= 0 {
		tick = 15 * time.Second
	}
	timer := time.NewTicker(tick)
	defer timer.Stop()
	r.TickOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			r.TickOnce(ctx)
		}
	}
}

// TickOnce claims due routines, retries crashed claims, and fails confirms that waited too long.
func (r *Runner) TickOnce(ctx context.Context) {
	if r == nil || r.Store == nil {
		return
	}
	claims, err := r.Store.ClaimDue(ctx, 4)
	if err != nil {
		r.log().Error("claim routines", "err", err)
	}
	for _, c := range claims {
		go r.execute(ctx, c)
	}
	r.retryQueued(ctx)
	r.sweepWaiting(ctx)
}

// retryQueued restarts claims that were taken but never started, which happens
// when the process dies after ClaimDue and before MarkRunning.
func (r *Runner) retryQueued(ctx context.Context) {
	orphans, err := r.Store.ListOrphanQueued(ctx, time.Now().Add(-time.Minute))
	if err != nil {
		r.log().Error("list queued routines", "err", err)
		return
	}
	for _, rn := range orphans {
		rt, _, err := r.Store.Get(ctx, rn.UserID, rn.RoutineID)
		if err != nil {
			continue
		}
		go r.execute(ctx, Claim{Routine: *rt, Run: rn})
	}
}

func (r *Runner) execute(ctx context.Context, c Claim) {
	rn, ok, err := r.Store.MarkRunning(ctx, c.Run.ID)
	if err != nil || !ok {
		if err != nil {
			r.log().Error("mark running", "run", c.Run.Key, "err", err)
		}
		return
	}
	c.Run = *rn
	if reason, pause := r.blocked(ctx, c); reason != "" {
		_ = r.Store.FailRun(ctx, c.Run.ID, reason, reason)
		if pause {
			_ = r.Store.Pause(ctx, c.Routine.ID)
		}
		r.deliver(ctx, c, RunFailed, reason)
		return
	}
	if r.Envs != nil {
		if _, err := r.Envs.EnsureAgent(ctx, c.Routine.UserID); err != nil {
			_ = r.Store.FailRun(ctx, c.Run.ID, "Agent 环境没有就绪", err.Error())
			r.deliver(ctx, c, RunFailed, "Agent 环境没有就绪")
			return
		}
		if c.Routine.Autonomy == AutonomyBrowse {
			if _, err := r.Envs.EnsureBrowser(ctx, c.Routine.UserID); err != nil {
				_ = r.Store.FailRun(ctx, c.Run.ID, "浏览器环境没有就绪", err.Error())
				r.deliver(ctx, c, RunFailed, "浏览器环境没有就绪")
				return
			}
		}
	}
	user, err := r.user(ctx, c.Routine.UserID)
	if err != nil {
		_ = r.Store.FailRun(ctx, c.Run.ID, "找不到任务所属用户", err.Error())
		return
	}
	title := c.Routine.Key + " " + c.Run.Key
	sessionID, err := r.Starter.StartRoutine(ctx, user, c.Routine.AssigneeAssistantID, title, c.Run.Key, c.Routine.Autonomy)
	if err != nil {
		_ = r.Store.FailRun(ctx, c.Run.ID, "无法启动执行会话", err.Error())
		r.deliver(ctx, c, RunFailed, "无法启动执行会话")
		return
	}
	if err := r.Store.AttachSession(ctx, c.Run.ID, sessionID); err != nil {
		_ = r.Store.FailRun(ctx, c.Run.ID, "无法绑定执行会话", err.Error())
		return
	}
	c.Run.SessionID = sessionID
	last, _ := r.Store.LastSuccessSummary(ctx, c.Routine.ID)
	r.prompt(ctx, c, sessionID, RenderPrompt(c, last), r.budget(c.Routine.MaxDurationSec))
}

func (r *Runner) prompt(ctx context.Context, c Claim, sessionID, text string, budget time.Duration) {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stop := make(chan struct{})
	go r.watch(runCtx, c.Run.ID, sessionID, budget, cancel, stop)
	_, err := r.ACP.Prompt(runCtx, sessionID, text)
	close(stop)
	rn, gerr := r.Store.GetRunByKey(ctx, c.Routine.UserID, c.Run.ID)
	if gerr != nil {
		r.log().Error("reload run", "run", c.Run.Key, "err", gerr)
		return
	}
	switch rn.Status {
	case RunSucceeded, RunFailed, RunCancelled:
		r.deliver(ctx, c, rn.Status, rn.Summary)
		return
	case RunWaitingUser:
		return
	}
	if err != nil && runCtx.Err() != nil && ctx.Err() == nil {
		_ = r.Store.FailRun(ctx, c.Run.ID, "超过时限", err.Error())
		r.deliver(ctx, c, RunFailed, "超过时限")
		return
	}
	if err != nil {
		_ = r.Store.FailRun(ctx, c.Run.ID, "运行失败", err.Error())
		r.deliver(ctx, c, RunFailed, "运行失败")
		return
	}
	_ = r.Store.FailRun(ctx, c.Run.ID, "运行结束但没有提交结果", "")
	r.deliver(ctx, c, RunFailed, "运行结束但没有提交结果")
}

func (r *Runner) watch(ctx context.Context, runID, sessionID string, budget time.Duration, cancel context.CancelFunc, stop <-chan struct{}) {
	timer := time.NewTimer(budget)
	defer timer.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-timer.C:
			if r.ACP != nil {
				_ = r.ACP.Cancel(context.Background(), sessionID)
			}
			cancel()
			return
		case <-tick.C:
			rn, err := r.runStatus(ctx, runID)
			if err == nil && rn.Status == RunWaitingUser {
				return
			}
		}
	}
}

func (r *Runner) runStatus(ctx context.Context, runID string) (*Run, error) {
	row := r.Store.DB.QueryRowContext(ctx, `SELECT `+runCols+` FROM routine_runs WHERE id=$1`, runID)
	return scanRun(row)
}

func (r *Runner) budget(sec int) time.Duration {
	d := time.Duration(sec) * time.Second
	if r.BudgetLimit > 0 && r.BudgetLimit < d {
		return r.BudgetLimit
	}
	if d <= 0 {
		return time.Duration(DefaultMaxDuration) * time.Second
	}
	return d
}

func (r *Runner) blocked(ctx context.Context, c Claim) (reason string, pause bool) {
	if r.Assistants == nil || c.Routine.AssigneeAssistantID == "" {
		return "负责人不在了", true
	}
	a, err := r.Assistants.Get(ctx, c.Routine.AssigneeAssistantID)
	if err != nil {
		return "负责人不在了", true
	}
	if a.Status != assistant.StatusActive {
		return "负责人已停用", true
	}
	if a.NetworkTier == assistant.NetworkNone {
		return "负责人的网络已关闭", true
	}
	if c.Routine.Autonomy == AutonomyBrowse && !a.Capabilities.Browser {
		return "负责人没有浏览器能力", true
	}
	return "", false
}

func (r *Runner) user(ctx context.Context, username string) (*storage.User, error) {
	if r.Users == nil {
		return nil, errors.New("user lookup unavailable")
	}
	return r.Users.GetByUsername(ctx, username)
}

func (r *Runner) deliver(ctx context.Context, c Claim, status, summary string) {
	line := SummaryLine(c.Routine.Key, c.Run.Key, status, summary)
	if r.Sessions != nil && c.Routine.AssigneeAssistantID != "" {
		list, err := r.Sessions.ListByAssistant(ctx, c.Routine.UserID, c.Routine.AssigneeAssistantID, 1)
		if err == nil && len(list) > 0 {
			_, _ = r.Sessions.AddMessage(ctx, list[0].ID, agentsession.RoleAssistant, line, nil)
		}
	}
	if c.Routine.DeliverIM && r.Notify != nil && c.Routine.AssigneeAssistantID != "" {
		if err := r.Notify(ctx, c.Routine.AssigneeAssistantID, line); err != nil {
			_ = r.Store.SetDeliverError(ctx, c.Run.ID, err.Error())
		}
	}
}

// HandleTicket continues or fails the run parked on a resolved assist ticket.
func (r *Runner) HandleTicket(ctx context.Context, t *assistticket.Ticket) {
	if r == nil || r.Store == nil || t == nil {
		return
	}
	rn, err := r.Store.FindWaitingByTicket(ctx, t.ID)
	if err != nil {
		return
	}
	rt, _, err := r.Store.Get(ctx, rn.UserID, rn.RoutineID)
	if err != nil {
		return
	}
	c := Claim{Routine: *rt, Run: *rn}
	if t.Resolution == assistticket.ResReject {
		note := t.ResolutionNote
		if note == "" {
			note = "用户否决了这一步"
		}
		_ = r.Store.FailRun(ctx, rn.ID, note, "")
		r.deliver(ctx, c, RunFailed, note)
		return
	}
	resumed, err := r.Store.Resume(ctx, rn.ID)
	if err != nil {
		return
	}
	c.Run = *resumed
	user, err := r.user(ctx, rn.UserID)
	if err != nil {
		return
	}
	if err := r.Starter.ResumeRoutine(ctx, user, rn.SessionID, rn.Key, rt.Autonomy); err != nil {
		_ = r.Store.FailRun(ctx, rn.ID, "无法恢复执行会话", err.Error())
		r.deliver(ctx, c, RunFailed, "无法恢复执行会话")
		return
	}
	left := DefaultMaxDuration
	if rn.BudgetLeftSec != nil {
		left = *rn.BudgetLeftSec
	}
	note := t.ResolutionNote
	if note == "" {
		note = "可以继续"
	}
	text := "用户已确认继续。确认说明：" + note + "。不可逆动作仍然要再停一次。"
	r.prompt(ctx, c, rn.SessionID, text, r.budget(left))
}

func (r *Runner) sweepWaiting(ctx context.Context) {
	limit := 24 * time.Hour
	if r.WaitLimit > 0 {
		limit = r.WaitLimit
	}
	stale, err := r.Store.ListStaleWaiting(ctx, time.Now().Add(-limit))
	if err != nil {
		return
	}
	for _, rn := range stale {
		_ = r.Store.FailRun(ctx, rn.ID, "等待确认超过 24 小时", "")
		rt, _, err := r.Store.Get(ctx, rn.UserID, rn.RoutineID)
		if err != nil {
			continue
		}
		r.deliver(ctx, Claim{Routine: *rt, Run: rn}, RunFailed, "等待确认超过 24 小时")
	}
}

func (r *Runner) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}
