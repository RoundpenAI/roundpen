package download

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Backend types.
const (
	TypeQBittorrent = "qbittorrent"
	TypeAria2       = "aria2"
	TypeWebhook     = "webhook"
)

// Backend is one configured download client.
type Backend struct {
	ID               string    `json:"id"`
	UserID           string    `json:"userId"`
	Name             string    `json:"name"`
	Type             string    `json:"type"`
	BaseURL          string    `json:"baseUrl"`
	Username         string    `json:"username,omitempty"`
	Password         string    `json:"-"`
	HasPassword      bool      `json:"hasPassword"`
	DefaultSavePath  string    `json:"defaultSavePath,omitempty"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// Store persists download backends.
type Store struct {
	DB *sql.DB
}

// List returns backends for a user (passwords omitted).
func (s *Store) List(ctx context.Context, userID string) ([]Backend, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT id, user_id, name, type, base_url, username, password, default_save_path, created_at, updated_at
		FROM download_backends WHERE user_id=$1 ORDER BY updated_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Backend
	for rows.Next() {
		b, err := scanBackend(rows)
		if err != nil {
			return nil, err
		}
		b.Password = ""
		out = append(out, b)
	}
	return out, rows.Err()
}

// Get returns a backend including password for RPC.
func (s *Store) Get(ctx context.Context, userID, id string) (*Backend, error) {
	if s == nil || s.DB == nil {
		return nil, sql.ErrNoRows
	}
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, user_id, name, type, base_url, username, password, default_save_path, created_at, updated_at
		FROM download_backends WHERE user_id=$1 AND id=$2`, userID, id)
	b, err := scanBackend(row)
	if err != nil {
		return nil, err
	}
	return &b, nil
}

// UpsertInput creates or updates a backend.
type UpsertInput struct {
	ID              string
	Name            string
	Type            string
	BaseURL         string
	Username        string
	Password        *string // nil = keep existing
	DefaultSavePath string
}

// Upsert inserts or updates.
func (s *Store) Upsert(ctx context.Context, userID string, in UpsertInput) (*Backend, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("download store not configured")
	}
	name := strings.TrimSpace(in.Name)
	base := strings.TrimRight(strings.TrimSpace(in.BaseURL), "/")
	typ := strings.TrimSpace(in.Type)
	if typ == "" {
		typ = TypeQBittorrent
	}
	switch typ {
	case TypeQBittorrent, TypeAria2, TypeWebhook:
	default:
		return nil, fmt.Errorf("type must be qbittorrent, aria2, or webhook")
	}
	if name == "" || base == "" {
		return nil, fmt.Errorf("name and baseUrl are required")
	}
	now := time.Now().UTC()
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = uuid.NewString()
		pass := ""
		if in.Password != nil {
			pass = *in.Password
		}
		_, err := s.DB.ExecContext(ctx, `
			INSERT INTO download_backends (
				id, user_id, name, type, base_url, username, password, default_save_path, created_at, updated_at
			) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`,
			id, userID, name, typ, base, strings.TrimSpace(in.Username), pass,
			strings.TrimSpace(in.DefaultSavePath), now, now)
		if err != nil {
			return nil, err
		}
	} else {
		cur, err := s.Get(ctx, userID, id)
		if err != nil {
			return nil, err
		}
		pass := cur.Password
		if in.Password != nil {
			pass = *in.Password
		}
		_, err = s.DB.ExecContext(ctx, `
			UPDATE download_backends SET
				name=$3, type=$4, base_url=$5, username=$6, password=$7,
				default_save_path=$8, updated_at=$9
			WHERE user_id=$1 AND id=$2`,
			userID, id, name, typ, base, strings.TrimSpace(in.Username), pass,
			strings.TrimSpace(in.DefaultSavePath), now)
		if err != nil {
			return nil, err
		}
	}
	b, err := s.Get(ctx, userID, id)
	if err != nil {
		return nil, err
	}
	b.Password = ""
	return b, nil
}

// Delete removes a backend.
func (s *Store) Delete(ctx context.Context, userID, id string) error {
	res, err := s.DB.ExecContext(ctx, `
		DELETE FROM download_backends WHERE user_id=$1 AND id=$2`, userID, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func scanBackend(row interface{ Scan(dest ...any) error }) (Backend, error) {
	var b Backend
	var pass string
	err := row.Scan(&b.ID, &b.UserID, &b.Name, &b.Type, &b.BaseURL, &b.Username, &pass,
		&b.DefaultSavePath, &b.CreatedAt, &b.UpdatedAt)
	if err != nil {
		return b, err
	}
	b.Password = pass
	b.HasPassword = pass != ""
	return b, nil
}

// ErrNotFound aliases sql.ErrNoRows for handlers.
var ErrNotFound = errors.New("download backend not found")
