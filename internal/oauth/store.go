package oauth

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

// ErrLinkedElsewhere means the remote account is already bound to another
// local user; identities are never reassigned implicitly.
var ErrLinkedElsewhere = errors.New("remote account is already linked to another user")

// ErrAlreadyLinked means the local user already has an identity for this
// provider and would have to disconnect it first.
var ErrAlreadyLinked = errors.New("provider already linked; disconnect it first")

// Identity is a remote account bound to a local user, with its tokens.
type Identity struct {
	ID             string     `json:"id"`
	UserID         string     `json:"userId"`
	ProviderID     string     `json:"providerId"`
	ProviderKind   string     `json:"providerKind"`
	ProviderHost   string     `json:"providerHost"`
	ProviderLabel  string     `json:"providerLabel"`
	Subject        string     `json:"subject"`
	Login          string     `json:"login"`
	Name           string     `json:"name"`
	Email          string     `json:"email"`
	AccessToken    string     `json:"-"`
	RefreshToken   string     `json:"-"`
	TokenExpiresAt *time.Time `json:"-"`
	Scopes         string     `json:"scopes"`
	LastLoginAt    *time.Time `json:"-"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

// IdentityUpsert binds a remote account to a local user (or refreshes it).
type IdentityUpsert struct {
	UserID       string
	ProviderID   string
	Subject      string
	Login        string
	Name         string
	Email        string
	AccessToken  string
	RefreshToken string
	ExpiresAt    *time.Time
	Scopes       string
	MarkLogin    bool
}

// State is one in-flight authorization flow.
type State struct {
	State       string
	ProviderID  string
	Verifier    string
	LinkUser    string
	RedirectURI string
	RedirectTo  string
	ExpiresAt   time.Time
	CreatedAt   time.Time
}

// Store persists providers, identities and authorization states in PostgreSQL.
type Store struct {
	DB *sql.DB
}

func (s *Store) ListProviders(ctx context.Context, enabledOnly bool) ([]Provider, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	q := `
		SELECT id, kind, scheme, host, label, client_id, client_secret, scopes,
		       auth_url, token_url, api_url, enabled
		FROM oauth_providers`
	if enabledOnly {
		q += ` WHERE enabled`
	}
	q += ` ORDER BY kind, host`
	rows, err := s.DB.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Provider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProvider(ctx context.Context, id string) (*Provider, error) {
	if s == nil || s.DB == nil {
		return nil, storage.ErrNotFound
	}
	row := s.DB.QueryRowContext(ctx, `
		SELECT id, kind, scheme, host, label, client_id, client_secret, scopes,
		       auth_url, token_url, api_url, enabled
		FROM oauth_providers WHERE id=$1`, id)
	p, err := scanProvider(row)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// SaveProvider upserts by id. The provider must already be normalized.
func (s *Store) SaveProvider(ctx context.Context, p Provider) (*Provider, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("oauth store not configured")
	}
	now := time.Now().UTC()
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO oauth_providers (id, kind, scheme, host, label, client_id, client_secret,
		                             scopes, auth_url, token_url, api_url, enabled, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13)
		ON CONFLICT (id) DO UPDATE SET
			kind=EXCLUDED.kind,
			scheme=EXCLUDED.scheme,
			host=EXCLUDED.host,
			label=EXCLUDED.label,
			client_id=EXCLUDED.client_id,
			client_secret=EXCLUDED.client_secret,
			scopes=EXCLUDED.scopes,
			auth_url=EXCLUDED.auth_url,
			token_url=EXCLUDED.token_url,
			api_url=EXCLUDED.api_url,
			enabled=EXCLUDED.enabled,
			updated_at=EXCLUDED.updated_at`,
		p.ID, p.Kind, p.Scheme, p.Host, p.Label, p.ClientID, p.ClientSecret,
		p.Scopes, p.AuthURL, p.TokenURL, p.APIURL, p.Enabled, now)
	if err != nil {
		return nil, err
	}
	return s.GetProvider(ctx, p.ID)
}

func (s *Store) DeleteProvider(ctx context.Context, id string) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("oauth store not configured")
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM oauth_providers WHERE id=$1`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

const identityCols = `
	i.id, i.user_id, i.provider_id, p.kind, p.host, p.label, i.subject, i.login, i.name, i.email,
	i.access_token, i.refresh_token, i.token_expires_at, i.scopes, i.last_login_at, i.created_at, i.updated_at`

func (s *Store) ListIdentities(ctx context.Context, userID string) ([]Identity, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT `+identityCols+`
		FROM user_identities i JOIN oauth_providers p ON p.id = i.provider_id
		WHERE i.user_id=$1 ORDER BY p.kind, p.host`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Identity
	for rows.Next() {
		id, err := scanIdentity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) GetIdentityBySubject(ctx context.Context, providerID, subject string) (*Identity, error) {
	if s == nil || s.DB == nil {
		return nil, storage.ErrNotFound
	}
	row := s.DB.QueryRowContext(ctx, `
		SELECT `+identityCols+`
		FROM user_identities i JOIN oauth_providers p ON p.id = i.provider_id
		WHERE i.provider_id=$1 AND i.subject=$2`, providerID, subject)
	id, err := scanIdentity(row)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (s *Store) GetIdentityForProvider(ctx context.Context, userID, providerID string) (*Identity, error) {
	if s == nil || s.DB == nil {
		return nil, storage.ErrNotFound
	}
	row := s.DB.QueryRowContext(ctx, `
		SELECT `+identityCols+`
		FROM user_identities i JOIN oauth_providers p ON p.id = i.provider_id
		WHERE i.user_id=$1 AND i.provider_id=$2`, userID, providerID)
	id, err := scanIdentity(row)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

// UpsertIdentity binds the remote account to in.UserID. It refuses to steal an
// identity that belongs to another user (ErrLinkedElsewhere).
func (s *Store) UpsertIdentity(ctx context.Context, in IdentityUpsert) (*Identity, error) {
	if s == nil || s.DB == nil {
		return nil, fmt.Errorf("oauth store not configured")
	}
	now := time.Now().UTC()
	var lastLogin any
	if in.MarkLogin {
		lastLogin = now
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO user_identities (id, user_id, provider_id, subject, login, name, email,
		                             access_token, refresh_token, token_expires_at, scopes,
		                             last_login_at, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$13)
		ON CONFLICT (provider_id, subject) DO UPDATE SET
			login=EXCLUDED.login,
			name=EXCLUDED.name,
			email=EXCLUDED.email,
			access_token=EXCLUDED.access_token,
			refresh_token=COALESCE(NULLIF(EXCLUDED.refresh_token, ''), user_identities.refresh_token),
			token_expires_at=EXCLUDED.token_expires_at,
			scopes=EXCLUDED.scopes,
			last_login_at=COALESCE(EXCLUDED.last_login_at, user_identities.last_login_at),
			updated_at=EXCLUDED.updated_at
		WHERE user_identities.user_id = EXCLUDED.user_id`,
		uuid.NewString(), in.UserID, in.ProviderID, in.Subject, in.Login, in.Name, in.Email,
		in.AccessToken, in.RefreshToken, in.ExpiresAt, in.Scopes, lastLogin, now)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAlreadyLinked
		}
		return nil, err
	}
	got, err := s.GetIdentityBySubject(ctx, in.ProviderID, in.Subject)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			// The ON CONFLICT ... WHERE guard rejected a cross-user write.
			return nil, ErrLinkedElsewhere
		}
		return nil, err
	}
	if got.UserID != in.UserID {
		return nil, ErrLinkedElsewhere
	}
	return got, nil
}

// SaveIdentityTokens persists refreshed tokens.
func (s *Store) SaveIdentityTokens(ctx context.Context, id, access, refresh string, expires *time.Time) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("oauth store not configured")
	}
	_, err := s.DB.ExecContext(ctx, `
		UPDATE user_identities SET
			access_token=$2,
			refresh_token=COALESCE(NULLIF($3, ''), refresh_token),
			token_expires_at=$4,
			updated_at=$5
		WHERE id=$1`, id, access, refresh, expires, time.Now().UTC())
	return err
}

func (s *Store) DeleteIdentity(ctx context.Context, userID, id string) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("oauth store not configured")
	}
	res, err := s.DB.ExecContext(ctx, `DELETE FROM user_identities WHERE user_id=$1 AND id=$2`, userID, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return storage.ErrNotFound
	}
	return nil
}

// ListExpiringIdentities returns identities whose access token expires before
// the given time (or already has). Tokens without expiry never show up.
func (s *Store) ListExpiringIdentities(ctx context.Context, before time.Time) ([]Identity, error) {
	if s == nil || s.DB == nil {
		return nil, nil
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT `+identityCols+`
		FROM user_identities i JOIN oauth_providers p ON p.id = i.provider_id
		WHERE i.token_expires_at IS NOT NULL
		  AND i.token_expires_at < $1
		  AND i.refresh_token <> ''
		ORDER BY i.token_expires_at`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Identity
	for rows.Next() {
		id, err := scanIdentity(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Store) CreateState(ctx context.Context, st State) error {
	if s == nil || s.DB == nil {
		return fmt.Errorf("oauth store not configured")
	}
	// Opportunistic GC: the table only ever holds in-flight flows.
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM oauth_states WHERE expires_at < now()`); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `
		INSERT INTO oauth_states (state, provider_id, verifier, link_user, redirect_uri, redirect_to, expires_at, created_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
		st.State, st.ProviderID, st.Verifier, st.LinkUser, st.RedirectURI, st.RedirectTo, st.ExpiresAt, st.CreatedAt)
	return err
}

// TakeState consumes a state. Missing, expired and already-used states are all
// sql.ErrNoRows: the caller must not distinguish them.
func (s *Store) TakeState(ctx context.Context, state string) (*State, error) {
	if s == nil || s.DB == nil {
		return nil, storage.ErrNotFound
	}
	row := s.DB.QueryRowContext(ctx, `
		DELETE FROM oauth_states WHERE state=$1 AND expires_at > now()
		RETURNING state, provider_id, verifier, link_user, redirect_uri, redirect_to, expires_at, created_at`, state)
	var st State
	err := row.Scan(&st.State, &st.ProviderID, &st.Verifier, &st.LinkUser, &st.RedirectURI,
		&st.RedirectTo, &st.ExpiresAt, &st.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, storage.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &st, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanProvider(row rowScanner) (Provider, error) {
	var p Provider
	err := row.Scan(&p.ID, &p.Kind, &p.Scheme, &p.Host, &p.Label, &p.ClientID, &p.ClientSecret,
		&p.Scopes, &p.AuthURL, &p.TokenURL, &p.APIURL, &p.Enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return Provider{}, storage.ErrNotFound
	}
	if err != nil {
		return Provider{}, err
	}
	return p, nil
}

func scanIdentity(row rowScanner) (Identity, error) {
	var id Identity
	err := row.Scan(&id.ID, &id.UserID, &id.ProviderID, &id.ProviderKind, &id.ProviderHost, &id.ProviderLabel,
		&id.Subject, &id.Login, &id.Name, &id.Email, &id.AccessToken, &id.RefreshToken,
		&id.TokenExpiresAt, &id.Scopes, &id.LastLoginAt, &id.CreatedAt, &id.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Identity{}, storage.ErrNotFound
	}
	if err != nil {
		return Identity{}, err
	}
	return id, nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "23505")
}
