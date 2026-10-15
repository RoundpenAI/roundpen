package connectors

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

// Store persists Google OAuth clients, grants and connect states.
type Store struct {
	DB *sql.DB
}

func (s *Store) GetClient(ctx context.Context, userID string) (*GoogleClient, error) {
	if s == nil || s.DB == nil {
		return nil, storage.ErrNotFound
	}
	row := s.DB.QueryRowContext(ctx, `
		SELECT user_id, client_id, client_secret, updated_at
		FROM user_google_oauth_clients WHERE user_id=$1`, userID)
	var c GoogleClient
	if err := row.Scan(&c.UserID, &c.ClientID, &c.ClientSecret, &c.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	return &c, nil
}

// SaveClient upserts the user's Google OAuth application credentials.
func (s *Store) SaveClient(ctx context.Context, userID, clientID, clientSecret string) (*GoogleClient, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("connectors store not configured")
	}
	userID = strings.TrimSpace(userID)
	clientID = strings.TrimSpace(clientID)
	if userID == "" || clientID == "" {
		return nil, fmt.Errorf("user id and client id are required")
	}
	now := time.Now().UTC()
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO user_google_oauth_clients (user_id, client_id, client_secret, updated_at)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (user_id) DO UPDATE SET
			client_id=EXCLUDED.client_id,
			client_secret=EXCLUDED.client_secret,
			updated_at=EXCLUDED.updated_at`,
		userID, clientID, clientSecret, now)
	if err != nil {
		return nil, err
	}
	return s.GetClient(ctx, userID)
}

// DeleteClient removes the OAuth client and cascades grants via explicit deletes.
func (s *Store) DeleteClient(ctx context.Context, userID string) error {
	if s == nil || s.DB == nil {
		return nil
	}
	if err := s.DeleteGrant(ctx, userID, KindGoogle); err != nil && !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM user_google_oauth_clients WHERE user_id=$1`, userID)
	return err
}

func (s *Store) GetGrant(ctx context.Context, userID, kind string) (*Grant, error) {
	if s == nil || s.DB == nil {
		return nil, storage.ErrNotFound
	}
	if kind == "" {
		kind = KindGoogle
	}
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, user_id, kind, subject, email, access_token, refresh_token,
		       token_expires_at, scopes, created_at, updated_at
		FROM user_connector_grants WHERE user_id=$1 AND kind=$2`, userID, kind)
	g, err := scanGrant(row)
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// UpsertGrant inserts or replaces the grant for (user, kind).
func (s *Store) UpsertGrant(ctx context.Context, g Grant) (*Grant, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("connectors store not configured")
	}
	if g.ID == "" {
		g.ID = uuid.NewString()
	}
	if g.Kind == "" {
		g.Kind = KindGoogle
	}
	now := time.Now().UTC()
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO user_connector_grants (
			id, user_id, kind, subject, email, access_token, refresh_token,
			token_expires_at, scopes, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$10)
		ON CONFLICT (user_id, kind) DO UPDATE SET
			subject=EXCLUDED.subject,
			email=EXCLUDED.email,
			access_token=EXCLUDED.access_token,
			refresh_token=COALESCE(NULLIF(EXCLUDED.refresh_token,''), user_connector_grants.refresh_token),
			token_expires_at=EXCLUDED.token_expires_at,
			scopes=EXCLUDED.scopes,
			updated_at=EXCLUDED.updated_at`,
		g.ID, g.UserID, g.Kind, g.Subject, g.Email, g.AccessToken, g.RefreshToken,
		nullTime(g.TokenExpiresAt), g.Scopes, now)
	if err != nil {
		return nil, err
	}
	return s.GetGrant(ctx, g.UserID, g.Kind)
}

// SaveGrantTokens updates only token fields after a refresh.
func (s *Store) SaveGrantTokens(ctx context.Context, id, access, refresh string, expires *time.Time) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("connectors store not configured")
	}
	_, err := s.DB.ExecContext(ctx, `
		UPDATE user_connector_grants SET
			access_token=$2,
			refresh_token=COALESCE(NULLIF($3,''), refresh_token),
			token_expires_at=$4,
			updated_at=now()
		WHERE id=$1`, id, access, refresh, nullTime(expires))
	return err
}

func (s *Store) DeleteGrant(ctx context.Context, userID, kind string) error {
	if s == nil || s.DB == nil {
		return nil
	}
	if kind == "" {
		kind = KindGoogle
	}
	res, err := s.DB.ExecContext(ctx, `
		DELETE FROM user_connector_grants WHERE user_id=$1 AND kind=$2`, userID, kind)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

func (s *Store) CreateState(ctx context.Context, st State) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("connectors store not configured")
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO connector_oauth_states (state, user_id, verifier, redirect_uri, redirect_to, expires_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7)`,
		st.State, st.UserID, st.Verifier, st.RedirectURI, st.RedirectTo, st.ExpiresAt, st.CreatedAt)
	return err
}

// TakeState deletes and returns a state (single use).
func (s *Store) TakeState(ctx context.Context, state string) (*State, error) {
	if s == nil || s.DB == nil {
		return nil, storage.ErrNotFound
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	row := tx.QueryRowContext(ctx, `
		SELECT state, user_id, verifier, redirect_uri, redirect_to, expires_at, created_at
		FROM connector_oauth_states WHERE state=$1 FOR UPDATE`, state)
	var st State
	if err := row.Scan(&st.State, &st.UserID, &st.Verifier, &st.RedirectURI, &st.RedirectTo, &st.ExpiresAt, &st.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM connector_oauth_states WHERE state=$1`, state); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &st, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func scanGrant(row scannable) (Grant, error) {
	var g Grant
	var exp sql.NullTime
	err := row.Scan(
		&g.ID, &g.UserID, &g.Kind, &g.Subject, &g.Email,
		&g.AccessToken, &g.RefreshToken, &exp, &g.Scopes,
		&g.CreatedAt, &g.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Grant{}, storage.ErrNotFound
		}
		return Grant{}, err
	}
	if exp.Valid {
		t := exp.Time.UTC()
		g.TokenExpiresAt = &t
	}
	return g, nil
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}
