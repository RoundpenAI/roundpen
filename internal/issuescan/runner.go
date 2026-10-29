package issuescan

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/assistant"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// Defaults for 少打扰.
const (
	DefaultTick     = 30 * time.Minute
	DefaultQuietFor = 45 * time.Minute
	DefaultCooldown = 6 * time.Hour
)

// UserLister lists accounts to scan.
type UserLister interface {
	ListAll(ctx context.Context) ([]storage.User, error)
}

// Assistants resolves the system assistant.
type Assistants interface {
	EnsureSystem(ctx context.Context, userID string) (*assistant.Assistant, error)
}

// QuietClock reports when the user last spoke in any chat.
type QuietClock interface {
	LastUserMessageAt(ctx context.Context, userID string) (time.Time, bool, error)
}

// SessionStarter opens a chat session for the system assistant and returns its id.
type SessionStarter interface {
	StartBoardScanSession(ctx context.Context, user *storage.User, assistantID, title string) (sessionID string, err error)
}

// Prompter sends one ACP prompt to a session.
type Prompter interface {
	PromptBoard(ctx context.Context, sessionID, text string) error
}

// UsersByID loads a storage.User by username.
type UsersByID interface {
	GetByUsername(ctx context.Context, username string) (*storage.User, error)
}

// Runner periodically requeues stale claims and, when the user is quiet,
// prompts the system assistant with a board digest.
type Runner struct {
	Users      UserLister
	UserGet    UsersByID
	Assistants Assistants
	Quiet      QuietClock
	Issues     IssueStore
	Store      *Store
	Starter    SessionStarter
	ACP        Prompter
	Log        *slog.Logger

	Tick              time.Duration
	QuietFor          time.Duration
	Cooldown          time.Duration
	ClaimLease        time.Duration
	RunningLease      time.Duration
	ReviewIdle        time.Duration
	FailedIdle        time.Duration
	BacklogIdle       time.Duration
	IssueScanCooldown time.Duration
	Now               func() time.Time
}

// Loop polls until ctx is cancelled.
func (r *Runner) Loop(ctx context.Context) {
	tick := r.Tick
	if tick <= 0 {
		tick = DefaultTick
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

// TickOnce scans every user.
func (r *Runner) TickOnce(ctx context.Context) {
	if r == nil || r.Users == nil || r.Issues == nil {
		return
	}
	users, err := r.Users.ListAll(ctx)
	if err != nil {
		r.log().Error("issuescan list users", "err", err)
		return
	}
	now := r.now()
	for _, u := range users {
		userID := strings.TrimSpace(u.Username)
		if userID == "" {
			continue
		}
		if err := r.considerUser(ctx, userID, now); err != nil {
			r.log().Warn("issuescan user", "user", userID, "err", err)
		}
	}
}

func (r *Runner) considerUser(ctx context.Context, userID string, now time.Time) error {
	actions, err := ApplyStatusTimeouts(ctx, r.Issues, userID, now, TimeoutPolicy{
		ClaimLease:   r.ClaimLease,
		RunningLease: r.RunningLease,
		ReviewIdle:   r.ReviewIdle,
		FailedIdle:   r.FailedIdle,
		BacklogIdle:  r.BacklogIdle,
	})
	if err != nil {
		return err
	}
	for _, a := range actions {
		if a.Did {
			r.log().Info("issuescan status timeout", "user", userID, "key", a.Key,
				"from", a.From, "to", a.To, "reason", a.Reason)
		}
	}

	quietFor := r.QuietFor
	if quietFor <= 0 {
		quietFor = DefaultQuietFor
	}
	cooldown := r.Cooldown
	if cooldown <= 0 {
		cooldown = DefaultCooldown
	}
	if r.Quiet != nil {
		last, ok, err := r.Quiet.LastUserMessageAt(ctx, userID)
		if err != nil {
			return err
		}
		if ok && now.Sub(last) < quietFor {
			return nil
		}
	}
	if r.Store != nil {
		st, err := r.Store.Get(ctx, userID)
		if err != nil {
			return err
		}
		if st.LastNotifiedAt != nil && now.Sub(*st.LastNotifiedAt) < cooldown {
			return nil
		}
	}

	issueCD := r.IssueScanCooldown
	if issueCD <= 0 {
		issueCD = DefaultIssueScanCooldown
	}
	text, digestKey, mentioned, err := BoardDigest(ctx, r.Issues, userID, now, 8, issueCD)
	if err != nil || text == "" {
		return err
	}
	sum := sha256.Sum256([]byte(digestKey))
	hash := hex.EncodeToString(sum[:])
	if r.Store != nil {
		st, err := r.Store.Get(ctx, userID)
		if err != nil {
			return err
		}
		if st.LastDigestHash != "" && st.LastDigestHash == hash {
			return nil
		}
	}
	if err := r.promptBoard(ctx, userID, text); err != nil {
		return err
	}
	if err := r.Issues.MarkBoardScanned(ctx, userID, mentioned, now); err != nil {
		r.log().Warn("issuescan mark scanned", "user", userID, "err", err)
	}
	if r.Store != nil {
		return r.Store.MarkNotified(ctx, userID, hash, now)
	}
	return nil
}

func (r *Runner) promptBoard(ctx context.Context, userID, text string) error {
	if r.Assistants == nil || r.Starter == nil || r.ACP == nil || r.UserGet == nil {
		r.log().Info("issuescan skip prompt: wiring incomplete", "user", userID)
		return nil
	}
	a, err := r.Assistants.EnsureSystem(ctx, userID)
	if err != nil {
		return err
	}
	user, err := r.UserGet.GetByUsername(ctx, userID)
	if err != nil {
		return err
	}
	sid, err := r.Starter.StartBoardScanSession(ctx, user, a.ID, "事项看板巡检")
	if err != nil {
		return err
	}
	return r.ACP.PromptBoard(ctx, sid, text)
}

func (r *Runner) now() time.Time {
	if r != nil && r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

func (r *Runner) log() *slog.Logger {
	if r != nil && r.Log != nil {
		return r.Log
	}
	return slog.Default()
}
