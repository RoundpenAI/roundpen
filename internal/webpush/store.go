// Package webpush stores browser Push API subscriptions and delivers
// notifications with VAPID-authenticated Web Push.
package webpush

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Subscription is one browser endpoint for a user.
type Subscription struct {
	Endpoint  string    `json:"endpoint"`
	UserID    string    `json:"userId"`
	P256dh    string    `json:"p256dh"`
	Auth      string    `json:"auth"`
	UserAgent string    `json:"userAgent,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Store persists push subscriptions in PostgreSQL.
type Store struct {
	DB *sql.DB
}

// UpsertInput is the browser PushSubscription JSON shape we accept.
type UpsertInput struct {
	Endpoint  string
	P256dh    string
	Auth      string
	UserAgent string
}

// Upsert creates or replaces a subscription for the endpoint.
func (s *Store) Upsert(ctx context.Context, userID string, in UpsertInput) (*Subscription, error) {
	if s == nil || s.DB == nil {
		return nil, sql.ErrConnDone
	}
	endpoint := strings.TrimSpace(in.Endpoint)
	p256dh := strings.TrimSpace(in.P256dh)
	auth := strings.TrimSpace(in.Auth)
	if endpoint == "" || p256dh == "" || auth == "" {
		return nil, errInvalidSubscription
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return nil, errInvalidSubscription
	}
	now := time.Now().UTC()
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO push_subscriptions (endpoint, user_id, p256dh, auth, user_agent, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $6)
		ON CONFLICT (endpoint) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			p256dh = EXCLUDED.p256dh,
			auth = EXCLUDED.auth,
			user_agent = EXCLUDED.user_agent,
			updated_at = EXCLUDED.updated_at`,
		endpoint, userID, p256dh, auth, strings.TrimSpace(in.UserAgent), now)
	if err != nil {
		return nil, err
	}
	return &Subscription{
		Endpoint:  endpoint,
		UserID:    userID,
		P256dh:    p256dh,
		Auth:      auth,
		UserAgent: strings.TrimSpace(in.UserAgent),
		CreatedAt: now,
		UpdatedAt: now,
	}, nil
}

// Delete removes one endpoint for the user (no-op if missing / wrong owner).
func (s *Store) Delete(ctx context.Context, userID, endpoint string) error {
	if s == nil || s.DB == nil {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `
		DELETE FROM push_subscriptions WHERE user_id=$1 AND endpoint=$2`,
		userID, strings.TrimSpace(endpoint))
	return err
}

// DeleteEndpoint removes a subscription by endpoint regardless of user
// (used when the push service returns gone).
func (s *Store) DeleteEndpoint(ctx context.Context, endpoint string) error {
	if s == nil || s.DB == nil {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `
		DELETE FROM push_subscriptions WHERE endpoint=$1`, strings.TrimSpace(endpoint))
	return err
}

// ListByUser returns all subscriptions for a user.
func (s *Store) ListByUser(ctx context.Context, userID string) ([]Subscription, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT endpoint, user_id, p256dh, auth, user_agent, created_at, updated_at
		FROM push_subscriptions WHERE user_id=$1 ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		var sub Subscription
		if err := rows.Scan(
			&sub.Endpoint, &sub.UserID, &sub.P256dh, &sub.Auth,
			&sub.UserAgent, &sub.CreatedAt, &sub.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	return out, rows.Err()
}

// HasAny reports whether the user has at least one subscription.
func (s *Store) HasAny(ctx context.Context, userID string) (bool, error) {
	if s == nil || s.DB == nil {
		return false, nil
	}
	var n int
	err := s.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM push_subscriptions WHERE user_id=$1`, userID).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}
