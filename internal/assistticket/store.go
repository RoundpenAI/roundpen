package assistticket

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("assist ticket not found")

const (
	KindPermission  = "permission"
	KindPolicyApply = "policy_apply"
	KindCaptcha     = "captcha"
	KindOther       = "other"

	StatusPending    = "pending"
	StatusResolved   = "resolved"
	StatusRejected   = "rejected"
	StatusCancelled  = "cancelled"

	ResAllowOnce  = "allow_once"
	ResPermanent  = "permanent"
	ResReject     = "reject"
)

type Ticket struct {
	ID              string          `json:"id"`
	UserID          string          `json:"userId"`
	AssistantID     string          `json:"assistantId"`
	SessionID       string          `json:"sessionId"`
	Kind            string          `json:"kind"`
	Status          string          `json:"status"`
	Title           string          `json:"title"`
	Reason          string          `json:"reason"`
	ContextSummary  string          `json:"contextSummary"`
	AskHuman        string          `json:"askHuman"`
	Payload         json.RawMessage `json:"payload,omitempty"`
	Resolution      string          `json:"resolution,omitempty"`
	ResolutionNote  string          `json:"resolutionNote,omitempty"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	ResolvedAt      *time.Time      `json:"resolvedAt,omitempty"`
}

type CreateInput struct {
	AssistantID    string
	SessionID      string
	Kind           string
	Title          string
	Reason         string
	ContextSummary string
	AskHuman       string
	Payload        any
}

type Store struct {
	DB *sql.DB
}

func (s *Store) Create(ctx context.Context, userID string, in CreateInput) (*Ticket, error) {
	kind := in.Kind
	if kind == "" {
		kind = KindOther
	}
	now := time.Now().UTC()
	var raw json.RawMessage
	if in.Payload != nil {
		b, err := json.Marshal(in.Payload)
		if err != nil {
			return nil, err
		}
		raw = b
	} else {
		raw = json.RawMessage(`{}`)
	}
	t := &Ticket{
		ID:             uuid.NewString(),
		UserID:         userID,
		AssistantID:    in.AssistantID,
		SessionID:      in.SessionID,
		Kind:           kind,
		Status:         StatusPending,
		Title:          in.Title,
		Reason:         in.Reason,
		ContextSummary: in.ContextSummary,
		AskHuman:       in.AskHuman,
		Payload:        raw,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO assist_tickets (
			id, user_id, assistant_id, session_id, kind, status,
			title, reason, context_summary, ask_human, payload, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		t.ID, t.UserID, t.AssistantID, t.SessionID, t.Kind, t.Status,
		t.Title, t.Reason, t.ContextSummary, t.AskHuman, t.Payload, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Store) Get(ctx context.Context, id string) (*Ticket, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, user_id, assistant_id, session_id, kind, status,
			title, reason, context_summary, ask_human, payload,
			resolution, resolution_note, created_at, updated_at, resolved_at
		FROM assist_tickets WHERE id=$1`, id)
	return scanTicket(row)
}

func (s *Store) ListPendingByUser(ctx context.Context, userID string) ([]*Ticket, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, user_id, assistant_id, session_id, kind, status,
			title, reason, context_summary, ask_human, payload,
			resolution, resolution_note, created_at, updated_at, resolved_at
		FROM assist_tickets
		WHERE user_id=$1 AND status=$2
		ORDER BY updated_at DESC LIMIT 100`, userID, StatusPending)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTickets(rows)
}

func (s *Store) ListByAssistant(ctx context.Context, userID, assistantID string, pendingOnly bool) ([]*Ticket, error) {
	q := `
		SELECT id, user_id, assistant_id, session_id, kind, status,
			title, reason, context_summary, ask_human, payload,
			resolution, resolution_note, created_at, updated_at, resolved_at
		FROM assist_tickets
		WHERE user_id=$1 AND assistant_id=$2`
	args := []any{userID, assistantID}
	if pendingOnly {
		q += ` AND status=$3`
		args = append(args, StatusPending)
	}
	q += ` ORDER BY updated_at DESC LIMIT 100`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanTickets(rows)
}

func (s *Store) CountPending(ctx context.Context, userID string) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM assist_tickets WHERE user_id=$1 AND status=$2`,
		userID, StatusPending).Scan(&n)
	return n, err
}

func (s *Store) Resolve(ctx context.Context, id, resolution, note string) (*Ticket, error) {
	t, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if t.Status != StatusPending {
		return t, nil
	}
	now := time.Now().UTC()
	status := StatusResolved
	if resolution == ResReject {
		status = StatusRejected
	}
	_, err = s.DB.ExecContext(ctx, `
		UPDATE assist_tickets SET status=$2, resolution=$3, resolution_note=$4,
			updated_at=$5, resolved_at=$5 WHERE id=$1`,
		id, status, resolution, note, now)
	if err != nil {
		return nil, err
	}
	t.Status = status
	t.Resolution = resolution
	t.ResolutionNote = note
	t.UpdatedAt = now
	t.ResolvedAt = &now
	return t, nil
}

func scanTickets(rows *sql.Rows) ([]*Ticket, error) {
	var out []*Ticket
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func scanTicket(row interface{ Scan(dest ...any) error }) (*Ticket, error) {
	var t Ticket
	var resolved sql.NullTime
	var payload []byte
	if err := row.Scan(
		&t.ID, &t.UserID, &t.AssistantID, &t.SessionID, &t.Kind, &t.Status,
		&t.Title, &t.Reason, &t.ContextSummary, &t.AskHuman, &payload,
		&t.Resolution, &t.ResolutionNote, &t.CreatedAt, &t.UpdatedAt, &resolved,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	if len(payload) > 0 {
		t.Payload = payload
	}
	if resolved.Valid {
		t.ResolvedAt = &resolved.Time
	}
	return &t, nil
}
