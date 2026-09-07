package browsetask

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/RoundpenAI/roundpen/internal/browser"
)

const (
	KindExplore = "explore"
	KindVerify  = "verify"
)

// Task is one Browser explore/verify job, usually backed by an agent session.
type Task struct {
	ID        string          `json:"id"`
	UserID    string          `json:"userId"`
	Kind      string          `json:"kind"`
	URL       string          `json:"url"`
	Brief     string          `json:"brief"`
	SessionID string          `json:"sessionId,omitempty"`
	Status    string          `json:"status"`
	Prompt    string          `json:"prompt,omitempty"`
	Report    json.RawMessage `json:"report,omitempty"`
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// Store persists browser tasks.
type Store struct {
	DB *sql.DB
}

// Create inserts a queued task.
func (s *Store) Create(ctx context.Context, userID, kind, startURL, brief, sessionID, prompt string) (*Task, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("browser tasks not configured")
	}
	now := time.Now().UTC()
	t := &Task{
		ID:        uuid.NewString(),
		UserID:    userID,
		Kind:      kind,
		URL:       startURL,
		Brief:     brief,
		SessionID: sessionID,
		Status:    "open",
		Prompt:    prompt,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO browser_tasks (id, user_id, kind, url, brief, session_id, status, prompt, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
		t.ID, t.UserID, t.Kind, t.URL, t.Brief, nullStr(t.SessionID), t.Status, t.Prompt, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return t, nil
}

// ListByUser returns recent tasks (newest first).
func (s *Store) ListByUser(ctx context.Context, userID string, limit int) ([]*Task, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("browser tasks not configured")
	}
	if limit <= 0 {
		limit = 30
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, user_id, kind, url, brief, COALESCE(session_id, ''), status, COALESCE(prompt, ''),
		       report, created_at, updated_at
		FROM browser_tasks WHERE user_id=$1 ORDER BY created_at DESC LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Task
	for rows.Next() {
		var t Task
		var report []byte
		if err := rows.Scan(&t.ID, &t.UserID, &t.Kind, &t.URL, &t.Brief, &t.SessionID, &t.Status, &t.Prompt,
			&report, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		if len(report) > 0 {
			t.Report = report
		}
		out = append(out, &t)
	}
	return out, rows.Err()
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// NormalizeKind returns explore or verify.
func NormalizeKind(kind string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "", KindExplore:
		return KindExplore, nil
	case KindVerify:
		return KindVerify, nil
	default:
		return "", fmt.Errorf("kind must be explore or verify")
	}
}

// TitleFor builds a short session title.
func TitleFor(kind, startURL string) string {
	host := startURL
	if u, err := url.Parse(startURL); err == nil && u.Host != "" {
		host = u.Host
	}
	if kind == KindVerify {
		return "Verify " + host
	}
	return "Explore " + host
}

// PromptFor is the first user message the curious agent receives.
func PromptFor(kind, startURL, brief string) string {
	var b strings.Builder
	if kind == KindVerify {
		b.WriteString("You are running a Browser verify task on Roundpen.\n\n")
		fmt.Fprintf(&b, "Start URL: %s\n\n", startURL)
		b.WriteString("First call browser_explore with this url. That tool hovers to reveal hidden actions and clicks interactive controls, reporting dead clicks and page errors.\n")
		b.WriteString("Then check the user's criteria against what you saw. Use browser_snapshot / browser_click / browser_hover for anything the walk missed.\n")
		b.WriteString("Do not sign out. Prefer the user's own test data.\n")
	} else {
		b.WriteString("You are running a Browser explore task on Roundpen.\n\n")
		fmt.Fprintf(&b, "Start URL: %s\n\n", startURL)
		b.WriteString("First call browser_explore with this url. That is the structural walk — it hovers rows, clicks what a person would try, and lists dead clicks / hover-revealed controls / page errors.\n")
		b.WriteString("Then be curious: pick the most surprising findings and probe them. Do not sign out.\n")
	}
	if notes := strings.TrimSpace(brief); notes != "" {
		b.WriteString("\nNotes from the user:\n")
		b.WriteString(notes)
		b.WriteByte('\n')
	}
	b.WriteString("\nWrite a short report: what you covered, what looks broken, what you skipped.")
	return b.String()
}

// MustStartURL validates and returns a trimmed URL.
func MustStartURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if err := browser.ValidateStartURL(raw); err != nil {
		return "", err
	}
	return raw, nil
}
