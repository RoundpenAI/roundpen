package mcp

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/RoundpenAI/roundpen/internal/secretbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

// Connection statuses.
const (
	StatusConnected   = "connected"
	StatusNeedsReauth = "needs_reauth"
	StatusError       = "error"
)

// Auth kinds for connections.
const (
	AuthKindOAuth  = "oauth"
	AuthKindAPIKey = "api_key"
)

// Connection is one user's linked MCP server.
type Connection struct {
	ID             string
	UserID         string
	ServerID       string
	AuthKind       string
	Status         string
	AccountLabel   string
	APIKey         string
	AccessToken    string
	RefreshToken   string
	TokenExpiresAt *time.Time
	Scopes         string
	TokenEndpoint  string
	ClientID       string
	ClientSecret   string
	Resource       string
	Tools          []Tool
	ToolsSyncedAt  *time.Time
	LastError      string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// State is one in-flight MCP OAuth flow. It carries the discovery result and
// DCR client so the callback does not re-run discovery.
type State struct {
	State                 string
	UserID                string
	ServerID              string
	Verifier              string
	RedirectURI           string
	RedirectTo            string
	Resource              string
	AuthorizationEndpoint string
	TokenEndpoint         string
	ClientID              string
	ClientSecret          string
	Scopes                string
	ExpiresAt             time.Time
	CreatedAt             time.Time
}

// Store persists MCP connections and OAuth states. Secrets (API keys, tokens,
// DCR client secrets) are sealed with the secretbox when one is configured.
type Store struct {
	DB  *sql.DB
	Box *secretbox.Box
}

func (s *Store) seal(v string) (string, error) {
	if s == nil || s.Box == nil {
		return v, nil
	}
	return s.Box.Seal(v)
}

func (s *Store) open(v string) (string, error) {
	if s == nil || s.Box == nil {
		return v, nil
	}
	return s.Box.Open(v)
}

const connectionCols = `id, user_id, server_id, auth_kind, status, account_label,
	api_key, access_token, refresh_token, token_expires_at, scopes, token_endpoint,
	client_id, client_secret, resource, tools, tools_synced_at, last_error,
	created_at, updated_at`

func (s *Store) ListConnections(ctx context.Context, userID string) ([]Connection, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT `+connectionCols+`
		FROM user_mcp_connections WHERE user_id=$1 ORDER BY server_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Connection
	for rows.Next() {
		c, err := s.scanConnection(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

func (s *Store) GetConnection(ctx context.Context, userID, serverID string) (*Connection, error) {
	if s == nil || s.DB == nil {
		return nil, storage.ErrNotFound
	}
	row := s.DB.QueryRowContext(ctx, `
		SELECT `+connectionCols+`
		FROM user_mcp_connections WHERE user_id=$1 AND server_id=$2`, userID, serverID)
	return s.scanConnection(row)
}

// UpsertConnection inserts or replaces the connection for (user, server).
func (s *Store) UpsertConnection(ctx context.Context, c Connection) (*Connection, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("mcp store not configured")
	}
	if c.ID == "" {
		c.ID = uuid.NewString()
	}
	if c.AuthKind == "" {
		c.AuthKind = AuthKindOAuth
	}
	if c.Status == "" {
		c.Status = StatusConnected
	}
	apiKey, err := s.seal(c.APIKey)
	if err != nil {
		return nil, err
	}
	access, err := s.seal(c.AccessToken)
	if err != nil {
		return nil, err
	}
	refresh, err := s.seal(c.RefreshToken)
	if err != nil {
		return nil, err
	}
	clientSecret, err := s.seal(c.ClientSecret)
	if err != nil {
		return nil, err
	}
	tools := c.Tools
	if tools == nil {
		tools = []Tool{}
	}
	toolsRaw, err := json.Marshal(tools)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO user_mcp_connections (
			id, user_id, server_id, auth_kind, status, account_label,
			api_key, access_token, refresh_token, token_expires_at, scopes, token_endpoint,
			client_id, client_secret, resource, tools, tools_synced_at, last_error,
			created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$19)
		ON CONFLICT (user_id, server_id) DO UPDATE SET
			auth_kind=EXCLUDED.auth_kind,
			status=EXCLUDED.status,
			account_label=EXCLUDED.account_label,
			api_key=EXCLUDED.api_key,
			access_token=EXCLUDED.access_token,
			refresh_token=EXCLUDED.refresh_token,
			token_expires_at=EXCLUDED.token_expires_at,
			scopes=EXCLUDED.scopes,
			token_endpoint=EXCLUDED.token_endpoint,
			client_id=EXCLUDED.client_id,
			client_secret=EXCLUDED.client_secret,
			resource=EXCLUDED.resource,
			tools=EXCLUDED.tools,
			tools_synced_at=EXCLUDED.tools_synced_at,
			last_error=EXCLUDED.last_error,
			updated_at=EXCLUDED.updated_at`,
		c.ID, c.UserID, c.ServerID, c.AuthKind, c.Status, c.AccountLabel,
		apiKey, access, refresh, nullTime(c.TokenExpiresAt), c.Scopes, c.TokenEndpoint,
		c.ClientID, clientSecret, c.Resource, toolsRaw, nullTime(c.ToolsSyncedAt), c.LastError,
		now)
	if err != nil {
		return nil, err
	}
	return s.GetConnection(ctx, c.UserID, c.ServerID)
}

// SaveTokens updates only the OAuth token fields after a refresh.
func (s *Store) SaveTokens(ctx context.Context, id, access, refresh string, expires *time.Time) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("mcp store not configured")
	}
	sealedAccess, err := s.seal(access)
	if err != nil {
		return err
	}
	sealedRefresh, err := s.seal(refresh)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `
		UPDATE user_mcp_connections SET
			access_token=$2,
			refresh_token=COALESCE(NULLIF($3,''), refresh_token),
			token_expires_at=$4,
			updated_at=now()
		WHERE id=$1`, id, sealedAccess, sealedRefresh, nullTime(expires))
	return err
}

// SaveTools replaces the tool snapshot for a connection.
func (s *Store) SaveTools(ctx context.Context, id string, tools []Tool, syncedAt time.Time) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("mcp store not configured")
	}
	if tools == nil {
		tools = []Tool{}
	}
	raw, err := json.Marshal(tools)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `
		UPDATE user_mcp_connections SET tools=$2, tools_synced_at=$3, updated_at=now()
		WHERE id=$1`, id, raw, syncedAt.UTC())
	return err
}

// SetStatus updates connection health after a call or refresh.
func (s *Store) SetStatus(ctx context.Context, id, status, lastErr string) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("mcp store not configured")
	}
	_, err := s.DB.ExecContext(ctx, `
		UPDATE user_mcp_connections SET status=$2, last_error=$3, updated_at=now()
		WHERE id=$1`, id, status, lastErr)
	return err
}

func (s *Store) DeleteConnection(ctx context.Context, userID, serverID string) error {
	if s == nil || s.DB == nil {
		return nil
	}
	res, err := s.DB.ExecContext(ctx, `
		DELETE FROM user_mcp_connections WHERE user_id=$1 AND server_id=$2`, userID, serverID)
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
		return fmt.Errorf("mcp store not configured")
	}
	clientSecret, err := s.seal(st.ClientSecret)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO mcp_oauth_states (
			state, user_id, server_id, verifier, redirect_uri, redirect_to, resource,
			authorization_endpoint, token_endpoint, client_id, client_secret, scopes,
			expires_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
		st.State, st.UserID, st.ServerID, st.Verifier, st.RedirectURI, st.RedirectTo, st.Resource,
		st.AuthorizationEndpoint, st.TokenEndpoint, st.ClientID, clientSecret, st.Scopes,
		st.ExpiresAt, st.CreatedAt)
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
		SELECT state, user_id, server_id, verifier, redirect_uri, redirect_to, resource,
			authorization_endpoint, token_endpoint, client_id, client_secret, scopes,
			expires_at, created_at
		FROM mcp_oauth_states WHERE state=$1 FOR UPDATE`, state)
	var st State
	if err := row.Scan(
		&st.State, &st.UserID, &st.ServerID, &st.Verifier, &st.RedirectURI, &st.RedirectTo, &st.Resource,
		&st.AuthorizationEndpoint, &st.TokenEndpoint, &st.ClientID, &st.ClientSecret, &st.Scopes,
		&st.ExpiresAt, &st.CreatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM mcp_oauth_states WHERE state=$1`, state); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	st.ClientSecret, err = s.open(st.ClientSecret)
	if err != nil {
		return nil, err
	}
	return &st, nil
}

type scannable interface {
	Scan(dest ...any) error
}

func (s *Store) scanConnection(row scannable) (*Connection, error) {
	var c Connection
	var toolsRaw []byte
	var exp, synced sql.NullTime
	err := row.Scan(
		&c.ID, &c.UserID, &c.ServerID, &c.AuthKind, &c.Status, &c.AccountLabel,
		&c.APIKey, &c.AccessToken, &c.RefreshToken, &exp, &c.Scopes, &c.TokenEndpoint,
		&c.ClientID, &c.ClientSecret, &c.Resource, &toolsRaw, &synced, &c.LastError,
		&c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, storage.ErrNotFound
		}
		return nil, err
	}
	if c.APIKey, err = s.open(c.APIKey); err != nil {
		return nil, err
	}
	if c.AccessToken, err = s.open(c.AccessToken); err != nil {
		return nil, err
	}
	if c.RefreshToken, err = s.open(c.RefreshToken); err != nil {
		return nil, err
	}
	if c.ClientSecret, err = s.open(c.ClientSecret); err != nil {
		return nil, err
	}
	if exp.Valid {
		t := exp.Time.UTC()
		c.TokenExpiresAt = &t
	}
	if synced.Valid {
		t := synced.Time.UTC()
		c.ToolsSyncedAt = &t
	}
	if len(toolsRaw) > 0 {
		if err := json.Unmarshal(toolsRaw, &c.Tools); err != nil {
			return nil, fmt.Errorf("decode tools snapshot: %w", err)
		}
	}
	return &c, nil
}

func nullTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC()
}
