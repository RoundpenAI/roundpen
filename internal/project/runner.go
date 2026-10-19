package project

import (
	"context"
	"log/slog"
	"time"

	acp "github.com/coder/acp-go-sdk"

	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// WorkerStarter creates and starts worker sessions.
type WorkerStarter interface {
	CreateWorkerSession(ctx context.Context, user *storage.User, assistantID, title string) (sessionID string, err error)
	StartWorkerRuntime(ctx context.Context, user *storage.User, sessionID, role string) error
}

// Prompter sends a turn to a live session.
type Prompter interface {
	Prompt(ctx context.Context, sessionID, text string) (acp.StopReason, error)
}

// UserLookup loads the account a worker acts as.
type UserLookup interface {
	GetByUsername(ctx context.Context, username string) (*storage.User, error)
}

// Transcript appends rows to a chat session (coordinator notify).
type Transcript interface {
	AddMessage(ctx context.Context, sessionID, role, content string, meta any) (*agentsession.Message, error)
}

// Runner drains the per-user serial worker queue.
type Runner struct {
	Store   *Store
	Users   UserLookup
	Starter WorkerStarter
	ACP     Prompter
	Notes   Transcript
	Log     *slog.Logger
}

// Kick tries to start the next queued worker for userID.
func (r *Runner) Kick(ctx context.Context, userID string) {
	if r == nil || r.Store == nil || r.Starter == nil || r.Users == nil || r.ACP == nil {
		return
	}
	w, err := r.Store.ClaimNextQueued(ctx, userID)
	if err != nil {
		r.log().Error("claim worker", "user", userID, "err", err)
		return
	}
	if w == nil {
		return
	}
	go r.execute(ctx, w)
}

func (r *Runner) execute(ctx context.Context, w *Worker) {
	user, err := r.Users.GetByUsername(ctx, w.UserID)
	if err != nil || user == nil {
		_ = r.Store.FinishWorker(ctx, w.ID, WorkerFailed, "user not found")
		r.Kick(ctx, w.UserID)
		return
	}
	p, err := r.Store.Get(ctx, w.UserID, w.ProjectID)
	if err != nil {
		// Try by id via Get - Get accepts key or id
		_ = r.Store.FinishWorker(ctx, w.ID, WorkerFailed, "project not found")
		r.Kick(ctx, w.UserID)
		return
	}
	if w.SessionID == "" {
		_ = r.Store.FinishWorker(ctx, w.ID, WorkerFailed, "missing session")
		r.Kick(ctx, w.UserID)
		return
	}
	if err := r.Starter.StartWorkerRuntime(ctx, user, w.SessionID, w.Role); err != nil {
		_ = r.Store.FinishWorker(ctx, w.ID, WorkerFailed, err.Error())
		r.Kick(ctx, w.UserID)
		return
	}
	// Align with chat turnTimeout / routine segment budget (2h).
	runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Hour)
	defer cancel()
	_, err = r.ACP.Prompt(runCtx, w.SessionID, RenderWorkerPrompt(p, w))
	status := WorkerSucceeded
	errMsg := ""
	if err != nil {
		status = WorkerFailed
		errMsg = err.Error()
	}
	_ = r.Store.FinishWorker(ctx, w.ID, status, errMsg)
	if fresh, getErr := r.Store.GetWorker(ctx, w.UserID, w.ID); getErr == nil {
		w = fresh
	} else {
		w.Status = status
		w.Error = errMsg
	}
	r.notifyCoordinator(ctx, p, w, status)
	r.Kick(ctx, w.UserID)
}

func (r *Runner) notifyCoordinator(ctx context.Context, p *Project, w *Worker, status string) {
	if r.Notes == nil || p == nil || p.CoordinatorSessionID == "" || w == nil {
		return
	}
	_, err := r.Notes.AddMessage(ctx, p.CoordinatorSessionID, agentsession.RoleEvent,
		RenderWorkerDoneNotice(w, status), map[string]string{"type": "worker_done"})
	if err != nil {
		r.log().Warn("coordinator worker_done note", "worker", w.Key, "err", err)
	}
}

func (r *Runner) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}
