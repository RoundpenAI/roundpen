package issuescan

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// State is one user's board-scan bookkeeping (cooldown + last digest).
type State struct {
	UserID         string
	LastNotifiedAt *time.Time
	LastDigestHash string
	UpdatedAt      time.Time
}

// Store persists scan cooldown rows.
type Store struct {
	DB *sql.DB
}

// Get returns the user's scan state, or zero values when none exists.
func (s *Store) Get(ctx context.Context, userID string) (State, error) {
	var st State
	var last sql.NullTime
	err := s.DB.QueryRowContext(ctx, `
		SELECT user_id, last_notified_at, last_digest_hash, updated_at
		FROM issue_board_scan_state WHERE user_id = $1`, userID).Scan(
		&st.UserID, &last, &st.LastDigestHash, &st.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
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

// MarkNotified records a delivered board prompt.
func (s *Store) MarkNotified(ctx context.Context, userID, hash string, at time.Time) error {
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO issue_board_scan_state (user_id, last_notified_at, last_digest_hash, updated_at)
		VALUES ($1, $2, $3, $2)
		ON CONFLICT (user_id) DO UPDATE SET
			last_notified_at = EXCLUDED.last_notified_at,
			last_digest_hash = EXCLUDED.last_digest_hash,
			updated_at = EXCLUDED.updated_at`,
		userID, at.UTC(), hash)
	return err
}
