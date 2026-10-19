package project

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrSubNotFound = errors.New("subscription not found")
var ErrSubKindTaken = errors.New("subscription kind already exists for project")

const subCols = `id, key, project_id, user_id, kind, repo, secret, enabled,
	last_event_at, COALESCE(last_status, ''), COALESCE(last_summary, ''),
	created_at, updated_at`

// CreateSubscription inserts a listening row; secret is generated when empty.
func (s *Store) CreateSubscription(ctx context.Context, userID, projectID string, kind, repo, secret string) (*Subscription, error) {
	kind, err := NormalizeSubKind(kind)
	if err != nil {
		return nil, err
	}
	repo, err = NormalizeRepo(repo)
	if err != nil {
		return nil, err
	}
	secret = strings.TrimSpace(secret)
	if secret == "" {
		secret, err = NewHookSecret()
		if err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	sub := &Subscription{
		ID:         uuid.NewString(),
		ProjectID:  projectID,
		UserID:     userID,
		Kind:       kind,
		Repo:       repo,
		Secret:     secret,
		Enabled:    true,
		CreatedAt:  now,
		UpdatedAt:  now,
		SecretOnce: secret,
	}
	err = s.DB.QueryRowContext(ctx, `
		INSERT INTO project_subscriptions (
			id, key, project_id, user_id, kind, repo, secret, enabled,
			last_status, last_summary, created_at, updated_at
		) VALUES (
			$1, 'PSUB-' || nextval('project_subscription_key_seq'), $2, $3, $4, $5, $6, TRUE,
			'', '', $7, $7
		)
		RETURNING key`,
		sub.ID, projectID, userID, kind, repo, secret, now,
	).Scan(&sub.Key)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrSubKindTaken
		}
		return nil, err
	}
	sub.HookPath = HookPathFor(sub.Key)
	return sub, nil
}

func (s *Store) ListSubscriptions(ctx context.Context, userID, projectID string) ([]*Subscription, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT `+subCols+` FROM project_subscriptions
		WHERE user_id = $1 AND project_id = $2
		ORDER BY kind`, userID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Subscription
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sub)
	}
	if out == nil {
		out = []*Subscription{}
	}
	return out, rows.Err()
}

func (s *Store) GetSubscription(ctx context.Context, userID, keyOrID string) (*Subscription, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT `+subCols+` FROM project_subscriptions
		WHERE user_id = $1 AND (id = $2 OR key = $2)`, userID, keyOrID)
	return scanSubscription(row)
}

// GetSubscriptionByKey looks up any user's subscription by public key (webhook).
func (s *Store) GetSubscriptionByKey(ctx context.Context, key string) (*Subscription, error) {
	row := s.DB.QueryRowContext(ctx, `
		SELECT `+subCols+` FROM project_subscriptions WHERE key = $1`, strings.TrimSpace(key))
	return scanSubscription(row)
}

func (s *Store) DeleteSubscription(ctx context.Context, userID, keyOrID string) error {
	res, err := s.DB.ExecContext(ctx, `
		DELETE FROM project_subscriptions
		WHERE user_id = $1 AND (id = $2 OR key = $2)`, userID, keyOrID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrSubNotFound
	}
	return nil
}

func (s *Store) SetSubscriptionEnabled(ctx context.Context, userID, keyOrID string, enabled bool) (*Subscription, error) {
	sub, err := s.GetSubscription(ctx, userID, keyOrID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	_, err = s.DB.ExecContext(ctx, `
		UPDATE project_subscriptions SET enabled = $3, updated_at = $4
		WHERE id = $1 AND user_id = $2`, sub.ID, userID, enabled, now)
	if err != nil {
		return nil, err
	}
	sub.Enabled = enabled
	sub.UpdatedAt = now
	return sub, nil
}

// TouchSubscriptionEvent records last delivery + optional CI status cache.
func (s *Store) TouchSubscriptionEvent(ctx context.Context, id, status, summary string) error {
	now := time.Now().UTC()
	_, err := s.DB.ExecContext(ctx, `
		UPDATE project_subscriptions SET
			last_event_at = $2,
			last_status = CASE WHEN $3 = '' THEN last_status ELSE $3 END,
			last_summary = CASE WHEN $4 = '' THEN last_summary ELSE $4 END,
			updated_at = $2
		WHERE id = $1`, id, now, strings.TrimSpace(status), strings.TrimSpace(summary))
	return err
}

func scanSubscription(row rowScanner) (*Subscription, error) {
	var sub Subscription
	var lastAt sql.NullTime
	err := row.Scan(
		&sub.ID, &sub.Key, &sub.ProjectID, &sub.UserID, &sub.Kind, &sub.Repo, &sub.Secret, &sub.Enabled,
		&lastAt, &sub.LastStatus, &sub.LastSummary, &sub.CreatedAt, &sub.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrSubNotFound
	}
	if err != nil {
		return nil, err
	}
	if lastAt.Valid {
		t := lastAt.Time.UTC()
		sub.LastEventAt = &t
	}
	sub.HookPath = HookPathFor(sub.Key)
	return &sub, nil
}
