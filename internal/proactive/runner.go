package proactive

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/agentsession"
	"github.com/RoundpenAI/roundpen/internal/assistant"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/webpush"
)

// Defaults for 少打扰.
const (
	DefaultTick       = 30 * time.Minute
	DefaultQuietFor   = 2 * time.Hour
	DefaultCooldown    = 12 * time.Hour
	DefaultMaxDigests = 8
)

// UserLister lists accounts to scan.
type UserLister interface {
	ListAll(ctx context.Context) ([]storage.User, error)
}

// Assistants resolves the system assistant for delivery.
type Assistants interface {
	EnsureSystem(ctx context.Context, userID string) (*assistant.Assistant, error)
}

// Sessions loads primary chat sessions and appends messages.
type Sessions interface {
	ListByAssistant(ctx context.Context, userID, assistantID string, limit int) ([]*agentsession.Session, error)
	AddMessage(ctx context.Context, sessionID, role, content string, meta any) (*agentsession.Message, error)
}

// QuietClock reports when the user last spoke in any chat.
type QuietClock interface {
	LastUserMessageAt(ctx context.Context, userID string) (time.Time, bool, error)
}

// PushNotifier is the optional Web Push fan-out.
type PushNotifier interface {
	Notify(ctx context.Context, userID string, payload webpush.Payload)
}

// Runner periodically scans quiet users and delivers one digest.
type Runner struct {
	Users      UserLister
	Assistants Assistants
	Sessions   Sessions
	Quiet      QuietClock
	Store      *Store
	Scanner    *Scanner
	Push       PushNotifier
	Log        *slog.Logger

	Tick     time.Duration
	QuietFor time.Duration
	Cooldown  time.Duration
	Now      func() time.Time
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

// TickOnce scans every user that looks quiet and is past cooldown.
func (r *Runner) TickOnce(ctx context.Context) {
	if r == nil || r.Users == nil || r.Scanner == nil {
		return
	}
	users, err := r.Users.ListAll(ctx)
	if err != nil {
		r.log().Error("proactive list users", "err", err)
		return
	}
	now := r.now()
	quietFor := r.QuietFor
	if quietFor <= 0 {
		quietFor = DefaultQuietFor
	}
	cooldown := r.Cooldown
	if cooldown <= 0 {
		cooldown = DefaultCooldown
	}
	for _, u := range users {
		userID := strings.TrimSpace(u.Username)
		if userID == "" {
			continue
		}
		if err := r.considerUser(ctx, userID, now, quietFor, cooldown); err != nil {
			r.log().Warn("proactive user", "user", userID, "err", err)
		}
	}
}

func (r *Runner) considerUser(ctx context.Context, userID string, now time.Time, quietFor, cooldown time.Duration) error {
	if r.Quiet != nil {
		last, ok, err := r.Quiet.LastUserMessageAt(ctx, userID)
		if err != nil {
			return err
		}
		if ok && now.Sub(last) < quietFor {
			return nil // still chatting
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

	digest, err := r.Scanner.ScanUser(ctx, userID, now)
	if err != nil {
		return err
	}
	actionable := Actionable(digest.Findings)
	if len(actionable) == 0 {
		return nil
	}
	hash := DigestHash(actionable)
	if r.Store != nil {
		st, err := r.Store.Get(ctx, userID)
		if err != nil {
			return err
		}
		if st.LastDigestHash != "" && st.LastDigestHash == hash {
			return nil // same digest already delivered
		}
	}

	text := digest.FormatText(DefaultMaxDigests)
	if text == "" {
		return nil
	}
	if err := r.deliver(ctx, userID, text); err != nil {
		return err
	}
	if r.Store != nil {
		return r.Store.MarkNotified(ctx, userID, hash, now)
	}
	return nil
}

func (r *Runner) deliver(ctx context.Context, userID, text string) error {
	if r.Assistants == nil || r.Sessions == nil {
		return nil
	}
	a, err := r.Assistants.EnsureSystem(ctx, userID)
	if err != nil {
		return err
	}
	list, err := r.Sessions.ListByAssistant(ctx, userID, a.ID, 1)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		r.log().Info("proactive skip deliver: no primary session", "user", userID)
		return nil
	}
	sess := list[0]
	meta := map[string]any{"type": "proactive", "kind": "digest"}
	if _, err := r.Sessions.AddMessage(ctx, sess.ID, agentsession.RoleEvent, text, meta); err != nil {
		return err
	}
	if r.Push != nil {
		r.Push.Notify(ctx, userID, webpush.Payload{
			Type:  "proactive",
			Title: "有几件值得看一眼的事",
			Body:  webpush.TruncateTitle(text, 120),
			URL:   webpush.SessionURL(a.ID, sess.ID),
		})
	}
	return nil
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

// SQLQuietClock reads the latest user message timestamp from agent_messages.
type SQLQuietClock struct {
	DB *sql.DB
}

// LastUserMessageAt implements QuietClock.
func (c SQLQuietClock) LastUserMessageAt(ctx context.Context, userID string) (time.Time, bool, error) {
	if c.DB == nil {
		return time.Time{}, false, nil
	}
	var t sql.NullTime
	err := c.DB.QueryRowContext(ctx, `
		SELECT MAX(m.created_at)
		FROM agent_messages m
		INNER JOIN agent_sessions s ON s.id = m.session_id
		WHERE s.user_id = $1 AND m.role = 'user'`, userID).Scan(&t)
	if err != nil {
		return time.Time{}, false, err
	}
	if !t.Valid {
		return time.Time{}, false, nil
	}
	return t.Time.UTC(), true, nil
}

// GitHubTokenFunc adapts a function to GitHubToken.
type GitHubTokenFunc func(ctx context.Context, userID string) (string, error)

// Token implements GitHubToken.
func (f GitHubTokenFunc) Token(ctx context.Context, userID string) (string, error) {
	if f == nil {
		return "", nil
	}
	return f(ctx, userID)
}

// GmailSearchFunc adapts a function to GmailSearcher.
type GmailSearchFunc func(ctx context.Context, userID, q string, maxResults int) ([]GmailHit, error)

// SearchGmail implements GmailSearcher.
func (f GmailSearchFunc) SearchGmail(ctx context.Context, userID, q string, maxResults int) ([]GmailHit, error) {
	if f == nil {
		return nil, nil
	}
	return f(ctx, userID, q, maxResults)
}
