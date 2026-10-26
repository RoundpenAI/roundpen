package proactive

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Store persists per-user cooldown and digest fingerprint.
type Store struct {
	DB *sql.DB
}

// State is one user's proactive recall bookkeeping.
type State struct {
	UserID         string
	LastNotifiedAt *time.Time
	LastDigestHash string
	UpdatedAt      time.Time
}

// Get returns state for userID, or zero values when absent.
func (s *Store) Get(ctx context.Context, userID string) (State, error) {
	userID = strings.TrimSpace(userID)
	if s == nil || s.DB == nil || userID == "" {
		return State{}, nil
	}
	var st State
	var last sql.NullTime
	err := s.DB.QueryRowContext(ctx, `
		SELECT user_id, last_notified_at, COALESCE(last_digest_hash, ''), updated_at
		FROM proactive_state WHERE user_id = $1`, userID).Scan(
		&st.UserID, &last, &st.LastDigestHash, &st.UpdatedAt)
	if err == sql.ErrNoRows {
		return State{UserID: userID}, nil
	}
	if err != nil {
		return State{}, err
	}
	if last.Valid {
		t := last.Time.UTC()
		st.LastNotifiedAt = &t
	}
	return st, nil
}

// MarkNotified records a successful recall delivery.
func (s *Store) MarkNotified(ctx context.Context, userID, digestHash string, at time.Time) error {
	userID = strings.TrimSpace(userID)
	if s == nil || s.DB == nil || userID == "" {
		return nil
	}
	if at.IsZero() {
		at = time.Now().UTC()
	} else {
		at = at.UTC()
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO proactive_state (user_id, last_notified_at, last_digest_hash, updated_at)
		VALUES ($1, $2, $3, $2)
		ON CONFLICT (user_id) DO UPDATE SET
			last_notified_at = EXCLUDED.last_notified_at,
			last_digest_hash = EXCLUDED.last_digest_hash,
			updated_at = EXCLUDED.updated_at`,
		userID, at, strings.TrimSpace(digestHash))
	return err
}
