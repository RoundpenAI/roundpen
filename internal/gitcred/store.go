package gitcred

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/RoundpenAI/roundpen/internal/settings"
)

// Store persists per-user git tokens in PostgreSQL.
type Store struct {
	DB *sql.DB
}

func (s *Store) List(ctx context.Context, userID string) ([]Cred, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, user_id, provider, host, username, label, token, created_at, updated_at
		FROM user_git_credentials WHERE user_id=$1 ORDER BY host`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Cred
	for rows.Next() {
		c, err := scanCred(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *Store) Get(ctx context.Context, userID, id string) (*Cred, error) {
	if s == nil || s.DB == nil {
		return nil, sql.ErrNoRows
	}
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, user_id, provider, host, username, label, token, created_at, updated_at
		FROM user_git_credentials WHERE user_id=$1 AND id=$2`, userID, id)
	c, err := scanCred(row)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

type UpsertInput struct {
	ID       string
	Provider string
	Host     string
	Username string
	Label    string
	Token    string
}

func (s *Store) Upsert(ctx context.Context, userID string, in UpsertInput) (*Cred, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("git credentials store not configured")
	}
	host := normalizeHost(in.Host)
	if host == "" {
		return nil, fmt.Errorf("host is required")
	}
	provider := normalizeProvider(in.Provider)
	if strings.TrimSpace(in.Provider) == "" {
		provider = inferProvider(host)
	}
	username := strings.TrimSpace(in.Username)
	label := strings.TrimSpace(in.Label)
	now := time.Now().UTC()

	if id := strings.TrimSpace(in.ID); id != "" {
		prev, err := s.Get(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		token := settings.ResolveSecret(in.Token, prev.Token)
		if token == "" {
			return nil, fmt.Errorf("token is required")
		}
		_, err = s.DB.ExecContext(ctx, `
			UPDATE user_git_credentials
			SET provider=$3, host=$4, username=$5, label=$6, token=$7, updated_at=$8
			WHERE user_id=$1 AND id=$2`,
			userID, id, provider, host, username, label, token, now)
		if err != nil {
			return nil, err
		}
		return s.Get(ctx, userID, id)
	}

	token := strings.TrimSpace(in.Token)
	if token == "" || settings.IsSecretMask(token) {
		return nil, fmt.Errorf("token is required")
	}
	id := uuid.NewString()
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO user_git_credentials (id, user_id, provider, host, username, label, token, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$8)
		ON CONFLICT (user_id, host) DO UPDATE SET
			provider=EXCLUDED.provider,
			username=EXCLUDED.username,
			label=EXCLUDED.label,
			token=EXCLUDED.token,
			updated_at=EXCLUDED.updated_at
		RETURNING id`,
		id, userID, provider, host, username, label, token, now)
	if err != nil {
		return nil, err
	}
	// Conflict path assigned a different id; load by host.
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, user_id, provider, host, username, label, token, created_at, updated_at
		FROM user_git_credentials WHERE user_id=$1 AND host=$2`, userID, host)
	c, err := scanCred(row)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

func (s *Store) Delete(ctx context.Context, userID, id string) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("git credentials store not configured")
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM user_git_credentials WHERE user_id=$1 AND id=$2`, userID, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCred(row rowScanner) (Cred, error) {
	var c Cred
	err := row.Scan(&c.ID, &c.UserID, &c.Provider, &c.Host, &c.Username, &c.Label, &c.Token, &c.CreatedAt, &c.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Cred{}, err
	}
	if err != nil {
		return Cred{}, err
	}
	c.HasToken = c.Token != ""
	return c, nil
}
