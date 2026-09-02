package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

const sessionTTL = 7 * 24 * time.Hour

// UserRole is a permission level.
type UserRole string

const (
	RoleUser  UserRole = "user"
	RoleAdmin UserRole = "admin"
)

// Valid reports whether r is a known role.
func (r UserRole) Valid() bool {
	return r == RoleUser || r == RoleAdmin
}

// User is an authenticated account with password and/or API key.
type User struct {
	Username     string    `json:"username"`
	Email        string    `json:"email"`
	FullName     string    `json:"fullname"`
	OrgName      string    `json:"orgName"`
	APIKey       string    `json:"apiKey"`
	Role         UserRole  `json:"role"`
	PasswordHash string    `json:"-"`
	AuthProvider string    `json:"authProvider,omitempty"`
	CreatedAt    time.Time `json:"createdAt,omitempty"`
	UpdatedAt    time.Time `json:"updatedAt,omitempty"`
}

// Session is a server-side login session (Cookie: roundpen_session).
type Session struct {
	ID         string    `json:"id"`
	UserID     string    `json:"userId"`
	TokenHash  string    `json:"-"`
	ExpiresAt  time.Time `json:"expiresAt"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	UserAgent  string    `json:"userAgent,omitempty"`
	IP         string    `json:"ip,omitempty"`
}

// UserStore persists users and their API keys.
type UserStore interface {
	GetByAPIKey(ctx context.Context, apiKey string) (*User, error)
	GetByUsername(ctx context.Context, username string) (*User, error)
	GetByEmail(ctx context.Context, email string) (*User, error)
	Upsert(ctx context.Context, user User) error
	ListAll(ctx context.Context) ([]User, error)
	Delete(ctx context.Context, username string) error
}

// SessionStore persists cookie-backed login sessions.
type SessionStore interface {
	Create(ctx context.Context, s Session) error
	GetByTokenHash(ctx context.Context, hash string) (*Session, error)
	Touch(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
	DeleteByUser(ctx context.Context, userID string) error
}

// PgUserStore is the PostgreSQL UserStore.
type PgUserStore struct {
	db *DB
}

// NewUserStore returns a PostgreSQL user store.
func NewUserStore(db *DB) *PgUserStore {
	return &PgUserStore{db: db}
}

const userCols = `username, email, fullname, org_name, api_key, COALESCE(role, 'user'), COALESCE(password_hash, ''), COALESCE(auth_provider, 'local'), created_at, updated_at`

func scanUser(row scannable) (*User, error) {
	var u User
	err := row.Scan(
		&u.Username, &u.Email, &u.FullName, &u.OrgName, &u.APIKey,
		&u.Role, &u.PasswordHash, &u.AuthProvider, &u.CreatedAt, &u.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *PgUserStore) GetByAPIKey(ctx context.Context, apiKey string) (*User, error) {
	row := s.db.SQL.QueryRowContext(ctx, "SELECT "+userCols+" FROM users WHERE api_key = $1", apiKey)
	return scanUser(row)
}

func (s *PgUserStore) GetByUsername(ctx context.Context, username string) (*User, error) {
	row := s.db.SQL.QueryRowContext(ctx, "SELECT "+userCols+" FROM users WHERE username = $1", username)
	return scanUser(row)
}

func (s *PgUserStore) GetByEmail(ctx context.Context, email string) (*User, error) {
	row := s.db.SQL.QueryRowContext(ctx,
		"SELECT "+userCols+" FROM users WHERE lower(email) = lower($1) AND email <> ''", email)
	return scanUser(row)
}

func (s *PgUserStore) Upsert(ctx context.Context, u User) error {
	if u.AuthProvider == "" {
		u.AuthProvider = "local"
	}
	if u.Role == "" {
		u.Role = RoleUser
	}
	now := time.Now().UTC()
	if u.CreatedAt.IsZero() {
		u.CreatedAt = now
	}
	u.UpdatedAt = now
	_, err := s.db.SQL.ExecContext(ctx, `
		INSERT INTO users (username, email, fullname, org_name, api_key, role, password_hash, auth_provider, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, ''), $8, $9, $10)
		ON CONFLICT (username) DO UPDATE SET
			email          = EXCLUDED.email,
			fullname       = EXCLUDED.fullname,
			org_name       = EXCLUDED.org_name,
			api_key        = EXCLUDED.api_key,
			role           = EXCLUDED.role,
			password_hash  = COALESCE(EXCLUDED.password_hash, users.password_hash),
			auth_provider  = COALESCE(NULLIF(EXCLUDED.auth_provider, ''), users.auth_provider),
			updated_at     = EXCLUDED.updated_at
	`, u.Username, u.Email, u.FullName, u.OrgName, u.APIKey, u.Role, u.PasswordHash, u.AuthProvider, u.CreatedAt, u.UpdatedAt)
	return err
}

func (s *PgUserStore) ListAll(ctx context.Context) ([]User, error) {
	rows, err := s.db.SQL.QueryContext(ctx, "SELECT "+userCols+" FROM users ORDER BY username")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var users []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		users = append(users, *u)
	}
	return users, rows.Err()
}

func (s *PgUserStore) Delete(ctx context.Context, username string) error {
	res, err := s.db.SQL.ExecContext(ctx, "DELETE FROM users WHERE username = $1", username)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// PgSessionStore is the PostgreSQL SessionStore.
type PgSessionStore struct {
	db *DB
}

// NewSessionStore returns a PostgreSQL session store.
func NewSessionStore(db *DB) *PgSessionStore {
	return &PgSessionStore{db: db}
}

func (s *PgSessionStore) Create(ctx context.Context, sess Session) error {
	_, err := s.db.SQL.ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at, last_seen_at, user_agent, ip)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`, sess.ID, sess.UserID, sess.TokenHash, sess.ExpiresAt, sess.CreatedAt, sess.LastSeenAt, sess.UserAgent, sess.IP)
	return err
}

func (s *PgSessionStore) GetByTokenHash(ctx context.Context, hash string) (*Session, error) {
	var sess Session
	err := s.db.SQL.QueryRowContext(ctx, `
		SELECT id, user_id, token_hash, expires_at, created_at, last_seen_at, COALESCE(user_agent, ''), COALESCE(ip, '')
		FROM sessions
		WHERE token_hash = $1 AND expires_at > now()
	`, hash).Scan(&sess.ID, &sess.UserID, &sess.TokenHash, &sess.ExpiresAt, &sess.CreatedAt, &sess.LastSeenAt, &sess.UserAgent, &sess.IP)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

func (s *PgSessionStore) Touch(ctx context.Context, id string) error {
	now := time.Now().UTC()
	_, err := s.db.SQL.ExecContext(ctx, `
		UPDATE sessions SET last_seen_at = $2, expires_at = $3 WHERE id = $1
	`, id, now, now.Add(sessionTTL))
	return err
}

func (s *PgSessionStore) Delete(ctx context.Context, id string) error {
	_, err := s.db.SQL.ExecContext(ctx, `DELETE FROM sessions WHERE id = $1`, id)
	return err
}

func (s *PgSessionStore) DeleteByUser(ctx context.Context, userID string) error {
	_, err := s.db.SQL.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = $1`, userID)
	return err
}

var _ UserStore = (*PgUserStore)(nil)
var _ SessionStore = (*PgSessionStore)(nil)

