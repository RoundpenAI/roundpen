package policy

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

type Denial struct {
	ID          string    `json:"id"`
	UserID      string    `json:"userId"`
	AssistantID string    `json:"assistantId"`
	SessionID   string    `json:"sessionId"`
	Dimension   string    `json:"dimension"`
	Target      string    `json:"target"`
	Reason      string    `json:"reason"`
	Appliable   bool      `json:"appliable"`
	CreatedAt   time.Time `json:"createdAt"`
}

type DenialStore struct {
	DB *sql.DB
}

func (s *DenialStore) Record(ctx context.Context, userID, assistantID, sessionID string, d Decision) (*Denial, error) {
	rec := &Denial{
		ID:          uuid.NewString(),
		UserID:      userID,
		AssistantID: assistantID,
		SessionID:   sessionID,
		Dimension:   d.Dimension,
		Target:      d.Target,
		Reason:      d.Reason,
		Appliable:   d.Appliable,
		CreatedAt:   time.Now().UTC(),
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO policy_denials (
			id, user_id, assistant_id, session_id, dimension, target, reason, appliable, created_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)`,
		rec.ID, rec.UserID, rec.AssistantID, rec.SessionID, rec.Dimension, rec.Target, rec.Reason, rec.Appliable, rec.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return rec, nil
}

func (s *DenialStore) ListByAssistant(ctx context.Context, assistantID string, limit int) ([]*Denial, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, user_id, assistant_id, session_id, dimension, target, reason, appliable, created_at
		FROM policy_denials WHERE assistant_id=$1
		ORDER BY created_at DESC LIMIT $2`, assistantID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Denial
	for rows.Next() {
		var d Denial
		if err := rows.Scan(&d.ID, &d.UserID, &d.AssistantID, &d.SessionID, &d.Dimension, &d.Target, &d.Reason, &d.Appliable, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}
