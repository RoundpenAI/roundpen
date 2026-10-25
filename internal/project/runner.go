package project

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
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

// Prompter sends a turn to a live session and can cancel an in-flight turn.
type Prompter interface {
	Prompt(ctx context.Context, sessionID, text string) (acp.StopReason, error)
	Cancel(ctx context.Context, sessionID string) error
}

// TurnRunner runs a turn through the chat session runner (persist + SSE).
// When set, workers use this instead of raw ACP.Prompt so "open live" shows
// the same transcript as an interactive assistant chat.
type TurnRunner interface {
	PromptTurn(ctx context.Context, user *storage.User, sessionID, text string) error
}

// UserLookup loads the account a worker acts as.
type UserLookup interface {
	GetByUsername(ctx context.Context, username string) (*storage.User, error)
}

// Transcript appends rows to a chat session (coordinator notify).
type Transcript interface {
	AddMessage(ctx context.Context, sessionID, role, content string, meta any) (*agentsession.Message, error)
}

// MessageSource reads recent session messages (to parse reviewer JSON).
type MessageSource interface {
	ListRecentMessages(ctx context.Context, sessionID string, limit int) ([]*agentsession.Message, error)
}

// Runner drains the per-user serial worker queue.
type Runner struct {
	Store    *Store
	Users    UserLookup
	Starter  WorkerStarter
	ACP      Prompter
	Turns    TurnRunner // optional; preferred over ACP.Prompt for workers
	Notes    Transcript
	Messages MessageSource
	Log      *slog.Logger
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
	prompt := RenderWorkerPrompt(p, w)
	if r.Turns != nil {
		err = r.Turns.PromptTurn(runCtx, user, w.SessionID, prompt)
	} else if r.ACP != nil {
		_, err = r.ACP.Prompt(runCtx, w.SessionID, prompt)
	} else {
		err = fmt.Errorf("no prompt backend")
	}
	if fresh, getErr := r.Store.GetWorker(ctx, w.UserID, w.ID); getErr == nil &&
		fresh.Status == WorkerCancelled {
		// User stop already marked cancelled + notified; just free the slot.
		r.Kick(ctx, w.UserID)
		return
	}
	status := WorkerSucceeded
	errMsg := ""
	if err != nil {
		status = WorkerFailed
		errMsg = err.Error()
	}
	_ = r.Store.FinishWorker(ctx, w.ID, status, errMsg)
	if w.Role == RoleReviewer && r.Notes != nil && w.SessionID != "" {
		if review := r.extractReview(ctx, w.SessionID); review != nil {
			_ = r.Store.SetWorkerReview(ctx, w.ID, review)
			w.Review = review
			w.ReviewJSON = MarshalReview(review)
		}
	}
	if fresh, getErr := r.Store.GetWorker(ctx, w.UserID, w.ID); getErr == nil {
		w = fresh
	} else {
		w.Status = status
		w.Error = errMsg
	}
	r.notifyCoordinator(ctx, p, w, w.Status)
	r.Kick(ctx, w.UserID)
}

// CancelWorker stops a queued or running worker. Running workers get an ACP
// cancel on their session so the execute goroutine unwinds.
func (r *Runner) CancelWorker(ctx context.Context, userID, keyOrID string) (*Worker, error) {
	if r == nil || r.Store == nil {
		return nil, fmt.Errorf("worker runner not configured")
	}
	w, err := r.Store.GetWorker(ctx, userID, keyOrID)
	if err != nil {
		return nil, err
	}
	switch w.Status {
	case WorkerQueued, WorkerRunning:
	default:
		return nil, fmt.Errorf("worker is not active")
	}
	wasRunning := w.Status == WorkerRunning
	sessionID := w.SessionID
	ok, err := r.Store.CancelWorker(ctx, w.ID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("worker is not active")
	}
	if wasRunning && sessionID != "" && r.ACP != nil {
		_ = r.ACP.Cancel(ctx, sessionID)
	}
	fresh, err := r.Store.GetWorker(ctx, userID, w.ID)
	if err != nil {
		return nil, err
	}
	p, _ := r.Store.Get(ctx, userID, w.ProjectID)
	r.notifyCoordinator(ctx, p, fresh, WorkerCancelled)
	r.Kick(ctx, userID)
	return fresh, nil
}

func (r *Runner) extractReview(ctx context.Context, sessionID string) *ReviewResult {
	src := r.Messages
	if src == nil {
		if store, ok := r.Notes.(*agentsession.Store); ok {
			src = store
		}
	}
	if src == nil {
		return nil
	}
	rows, err := src.ListRecentMessages(ctx, sessionID, 40)
	if err != nil {
		return nil
	}
	var texts []string
	for _, m := range agentsession.AfterLastClear(rows) {
		if m.Role == agentsession.RoleAssistant && strings.TrimSpace(m.Content) != "" {
			texts = append(texts, m.Content)
		}
	}
	return ExtractReviewResult(texts...)
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

// NotifyGitHubEvent writes a subscription notice into the coordinator transcript
// and best-effort starts a short turn so the coordinator can summarize.
func (r *Runner) NotifyGitHubEvent(ctx context.Context, p *Project, kind, repo, summary string) {
	if r == nil || p == nil || p.CoordinatorSessionID == "" {
		return
	}
	notice := FormatHookNotice(kind, repo, summary)
	if r.Notes != nil {
		if _, err := r.Notes.AddMessage(ctx, p.CoordinatorSessionID, agentsession.RoleEvent,
			notice, map[string]string{"type": "github_hook", "kind": kind}); err != nil {
			r.log().Warn("coordinator github_hook note", "project", p.Key, "err", err)
		}
	}
	if r.ACP == nil {
		return
	}
	go func() {
		runCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Minute)
		defer cancel()
		if _, err := r.ACP.Prompt(runCtx, p.CoordinatorSessionID, notice); err != nil {
			r.log().Info("coordinator github_hook prompt", "project", p.Key, "err", err)
		}
	}()
}

func (r *Runner) log() *slog.Logger {
	if r.Log != nil {
		return r.Log
	}
	return slog.Default()
}
