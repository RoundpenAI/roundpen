package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/gitcred"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

var (
	// ErrStateInvalid covers missing, expired and replayed states alike.
	ErrStateInvalid = errors.New("authorization state is invalid or expired")
	// ErrStateUserMismatch is a link flow finishing in another session.
	ErrStateUserMismatch = errors.New("this authorization was started by a different session")
	// ErrRegistrationDisabled means the identity matched no local user.
	ErrRegistrationDisabled = errors.New("no local account matches; sign in with your password and link from settings")
	// ErrProviderUnavailable covers unknown and disabled providers.
	ErrProviderUnavailable = errors.New("provider is not available")
)

const (
	defaultStateTTL = 10 * time.Minute
	// refreshWindow is how long before expiry a token is refreshed.
	refreshWindow = 5 * time.Minute
)

// store is the persistence the service needs; *Store implements it.
type store interface {
	ListProviders(ctx context.Context, enabledOnly bool) ([]Provider, error)
	GetProvider(ctx context.Context, id string) (*Provider, error)
	SaveProvider(ctx context.Context, p Provider) (*Provider, error)
	DeleteProvider(ctx context.Context, id string) error

	ListIdentities(ctx context.Context, userID string) ([]Identity, error)
	GetIdentityBySubject(ctx context.Context, providerID, subject string) (*Identity, error)
	GetIdentityForProvider(ctx context.Context, userID, providerID string) (*Identity, error)
	UpsertIdentity(ctx context.Context, in IdentityUpsert) (*Identity, error)
	SaveIdentityTokens(ctx context.Context, id, access, refresh string, expires *time.Time) error
	DeleteIdentity(ctx context.Context, userID, id string) error
	ListExpiringIdentities(ctx context.Context, before time.Time) ([]Identity, error)

	CreateState(ctx context.Context, st State) error
	TakeState(ctx context.Context, state string) (*State, error)
}

// Service runs the authorization flow and projects identities into git creds.
type Service struct {
	Store             store
	Users             storage.UserStore
	AllowRegistration func() bool
	StateTTL          time.Duration
	// Now is overridable in tests.
	Now func() time.Time
}

// StartInput describes one authorization (login or link) request.
type StartInput struct {
	ProviderID    string
	LinkUser      string // non-empty → bind to that session user
	RedirectTo    string // SPA path to land on after the callback
	ConsoleScheme string
	ConsoleHost   string
}

// CallbackInput is everything the callback endpoint learned.
type CallbackInput struct {
	ProviderID  string
	Code        string
	State       string
	SessionUser string // current session user, empty when anonymous
}

// CallbackResult tells the HTTP layer who to sign in and where to send them.
type CallbackResult struct {
	User       *storage.User
	RedirectTo string
	// Linked is true when an existing signed-in user gained this identity.
	Linked bool
}

// ListEnabledProviders returns the providers shown on the login page.
func (s *Service) ListEnabledProviders(ctx context.Context) ([]Provider, error) {
	return s.Store.ListProviders(ctx, true)
}

// ListProviders returns every configured provider (admin view).
func (s *Service) ListProviders(ctx context.Context) ([]Provider, error) {
	return s.Store.ListProviders(ctx, false)
}

// Provider loads one provider regardless of its enabled flag.
func (s *Service) Provider(ctx context.Context, id string) (*Provider, error) {
	return s.Store.GetProvider(ctx, id)
}

// SaveProvider validates/normalizes then persists a provider.
func (s *Service) SaveProvider(ctx context.Context, p Provider) (*Provider, error) {
	norm, err := Normalize(p)
	if err != nil {
		return nil, err
	}
	return s.Store.SaveProvider(ctx, norm)
}

// DeleteProvider removes a provider and (by cascade) its identities.
func (s *Service) DeleteProvider(ctx context.Context, id string) error {
	return s.Store.DeleteProvider(ctx, id)
}

// Identities lists the user's linked remote accounts.
func (s *Service) Identities(ctx context.Context, userID string) ([]Identity, error) {
	return s.Store.ListIdentities(ctx, userID)
}

// Unlink removes one linked account; the next injection drops its token.
func (s *Service) Unlink(ctx context.Context, userID, id string) error {
	return s.Store.DeleteIdentity(ctx, userID, id)
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) stateTTL() time.Duration {
	if s.StateTTL > 0 {
		return s.StateTTL
	}
	return defaultStateTTL
}

// AuthorizeURL starts a flow and returns the remote authorization URL.
func (s *Service) AuthorizeURL(ctx context.Context, in StartInput) (string, error) {
	p, err := s.provider(ctx, in.ProviderID)
	if err != nil {
		return "", err
	}
	state, err := randomHex(32)
	if err != nil {
		return "", err
	}
	verifier, err := randomBase64(32)
	if err != nil {
		return "", err
	}
	redirectURI := p.CallbackURL(in.ConsoleScheme, in.ConsoleHost)
	now := s.now()
	if err := s.Store.CreateState(ctx, State{
		State:       state,
		ProviderID:  p.ID,
		Verifier:    verifier,
		LinkUser:    in.LinkUser,
		RedirectURI: redirectURI,
		RedirectTo:  safeRedirect(in.RedirectTo),
		ExpiresAt:   now.Add(s.stateTTL()),
		CreatedAt:   now,
	}); err != nil {
		return "", err
	}
	return p.AuthorizeURL(state, s256Challenge(verifier), redirectURI), nil
}

// HandleCallback completes a flow: it consumes the state, exchanges the code,
// resolves (or creates) the local user and stores the identity.
func (s *Service) HandleCallback(ctx context.Context, in CallbackInput) (*CallbackResult, error) {
	st, err := s.Store.TakeState(ctx, in.State)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, ErrStateInvalid
		}
		return nil, err
	}
	if st.ProviderID != in.ProviderID {
		return nil, ErrStateInvalid
	}
	if st.LinkUser != "" && st.LinkUser != in.SessionUser {
		return nil, ErrStateUserMismatch
	}
	p, err := s.provider(ctx, st.ProviderID)
	if err != nil {
		return nil, err
	}

	client := &Client{Provider: *p}
	tok, err := client.Exchange(ctx, in.Code, st.Verifier, st.RedirectURI)
	if err != nil {
		return nil, err
	}
	prof, err := client.Profile(ctx, tok.AccessToken)
	if err != nil {
		return nil, err
	}

	user, linked, err := s.resolveUser(ctx, *p, st, prof)
	if err != nil {
		return nil, err
	}
	if _, err := s.Store.UpsertIdentity(ctx, IdentityUpsert{
		UserID:       user.Username,
		ProviderID:   p.ID,
		Subject:      prof.Subject,
		Login:        prof.Login,
		Name:         prof.Name,
		Email:        prof.VerifiedEmail,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    tok.ExpiresAt,
		Scopes:       tok.Scopes,
		MarkLogin:    !linked,
	}); err != nil {
		return nil, err
	}
	return &CallbackResult{User: user, RedirectTo: safeRedirect(st.RedirectTo), Linked: linked}, nil
}

// resolveUser applies the account-mapping rules: an existing binding wins,
// then an explicit link, then a verified-email match, then (only when public
// registration is on) a brand-new account.
func (s *Service) resolveUser(ctx context.Context, p Provider, st *State, prof Profile) (*storage.User, bool, error) {
	ident, err := s.Store.GetIdentityBySubject(ctx, p.ID, prof.Subject)
	switch {
	case err == nil:
		user, uerr := s.Users.GetByUsername(ctx, ident.UserID)
		if uerr != nil {
			return nil, false, uerr
		}
		return user, false, nil
	case !errors.Is(err, storage.ErrNotFound):
		return nil, false, err
	}

	if st.LinkUser != "" {
		user, uerr := s.Users.GetByUsername(ctx, st.LinkUser)
		if uerr != nil {
			return nil, false, uerr
		}
		existing, eerr := s.Store.GetIdentityForProvider(ctx, user.Username, p.ID)
		switch {
		case eerr == nil && existing.Subject != prof.Subject:
			return nil, false, ErrAlreadyLinked
		case eerr != nil && !errors.Is(eerr, storage.ErrNotFound):
			return nil, false, eerr
		}
		return user, true, nil
	}

	if prof.VerifiedEmail != "" {
		user, uerr := s.Users.GetByEmail(ctx, prof.VerifiedEmail)
		if uerr == nil {
			return user, false, nil
		}
		if !errors.Is(uerr, storage.ErrNotFound) {
			return nil, false, uerr
		}
	}

	if s.AllowRegistration == nil || !s.AllowRegistration() {
		return nil, false, ErrRegistrationDisabled
	}

	username, err := auth.UniqueUsername(ctx, s.Users, auth.SanitizeUsername(prof.Login))
	if err != nil {
		return nil, false, err
	}
	id, err := auth.NewID()
	if err != nil {
		return nil, false, err
	}
	user := &storage.User{
		Username:     username,
		Email:        strings.ToLower(strings.TrimSpace(prof.VerifiedEmail)),
		FullName:     strings.TrimSpace(prof.Name),
		APIKey:       auth.APIKeyPrefix + id,
		Role:         storage.RoleUser,
		AuthProvider: p.Kind,
	}
	if err := s.Users.Upsert(ctx, *user); err != nil {
		return nil, false, err
	}
	return user, false, nil
}

// CredsForUser projects the user's identities into git credentials, refreshing
// access tokens that are at (or near) expiry. Tokens the remote never expires
// are passed through untouched.
func (s *Service) CredsForUser(ctx context.Context, userID string) ([]gitcred.Cred, error) {
	idents, err := s.Store.ListIdentities(ctx, userID)
	if err != nil {
		return nil, err
	}
	providers := map[string]*Provider{}
	out := make([]gitcred.Cred, 0, len(idents))
	for i := range idents {
		id := idents[i]
		if id.AccessToken == "" {
			continue
		}
		if s.expiring(&id) {
			if err := s.refreshIdentity(ctx, &id); err != nil {
				slog.Warn("oauth token refresh", "user", userID, "provider", id.ProviderID, "err", err)
			}
		}
		if id.AccessToken == "" {
			continue
		}
		p, ok := providers[id.ProviderID]
		if !ok {
			p, err = s.Store.GetProvider(ctx, id.ProviderID)
			if err != nil {
				slog.Warn("oauth provider lookup", "user", userID, "provider", id.ProviderID, "err", err)
				continue
			}
			providers[id.ProviderID] = p
		}
		// A disabled provider is switched off everywhere: its tokens stop
		// being injected even though the stored binding survives.
		if !p.Enabled {
			continue
		}
		out = append(out, gitcred.TokenCred(p.Kind, p.Host, id.AccessToken))
	}
	return out, nil
}

// RefreshExpiring refreshes every token that expires before the given time and
// reports the affected users so callers can re-inject their sandboxes.
func (s *Service) RefreshExpiring(ctx context.Context, before time.Time) ([]string, error) {
	idents, err := s.Store.ListExpiringIdentities(ctx, before)
	if err != nil {
		return nil, err
	}
	users := map[string]bool{}
	for i := range idents {
		id := idents[i]
		if err := s.refreshIdentity(ctx, &id); err != nil {
			slog.Warn("oauth token refresh", "user", id.UserID, "provider", id.ProviderID, "err", err)
			continue
		}
		users[id.UserID] = true
	}
	out := make([]string, 0, len(users))
	for u := range users {
		out = append(out, u)
	}
	return out, nil
}

func (s *Service) expiring(id *Identity) bool {
	return id.TokenExpiresAt != nil && !id.TokenExpiresAt.After(s.now().Add(refreshWindow))
}

func (s *Service) refreshIdentity(ctx context.Context, id *Identity) error {
	if id.RefreshToken == "" {
		return errors.New("identity has no refresh token")
	}
	p, err := s.provider(ctx, id.ProviderID)
	if err != nil {
		return err
	}
	tok, err := (&Client{Provider: *p}).Refresh(ctx, id.RefreshToken)
	if err != nil {
		return err
	}
	if err := s.Store.SaveIdentityTokens(ctx, id.ID, tok.AccessToken, tok.RefreshToken, tok.ExpiresAt); err != nil {
		return err
	}
	id.AccessToken = tok.AccessToken
	if tok.RefreshToken != "" {
		id.RefreshToken = tok.RefreshToken
	}
	id.TokenExpiresAt = tok.ExpiresAt
	return nil
}

func (s *Service) provider(ctx context.Context, id string) (*Provider, error) {
	p, err := s.Store.GetProvider(ctx, id)
	if errors.Is(err, storage.ErrNotFound) {
		return nil, ErrProviderUnavailable
	}
	if err != nil {
		return nil, err
	}
	if !p.Enabled {
		return nil, ErrProviderUnavailable
	}
	return p, nil
}

// safeRedirect keeps post-callback navigation inside the console: absolute
// URLs, protocol-relative URLs and backslash tricks all fall back to "/".
func safeRedirect(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.Contains(raw, "\\") {
		return "/"
	}
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/") {
		return "/"
	}
	return u.RequestURI()
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func randomBase64(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func s256Challenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
