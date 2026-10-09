package connectors

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/storage"
)

// Service runs Google connector OAuth and token access for tools.
type Service struct {
	Store *Store
	HTTP  *http.Client
	Now   func() time.Time
	TTL   time.Duration
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now().UTC()
}

func (s *Service) stateTTL() time.Duration {
	if s.TTL > 0 {
		return s.TTL
	}
	return defaultStateTTL
}

func (s *Service) httpClient() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: httpTimeout}
}

// GetClient returns the user's Google OAuth app credentials.
func (s *Service) GetClient(ctx context.Context, userID string) (*GoogleClient, error) {
	c, err := s.Store.GetClient(ctx, userID)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, ErrNoClient
		}
		return nil, err
	}
	if strings.TrimSpace(c.ClientID) == "" {
		return nil, ErrNoClient
	}
	return c, nil
}

// SaveClient stores Client ID/Secret. Empty secret with mask handling is done in HTTP.
func (s *Service) SaveClient(ctx context.Context, userID, clientID, clientSecret string) (*GoogleClient, error) {
	return s.Store.SaveClient(ctx, userID, clientID, clientSecret)
}

// DeleteClient removes credentials and the grant.
func (s *Service) DeleteClient(ctx context.Context, userID string) error {
	return s.Store.DeleteClient(ctx, userID)
}

// Status returns connector status for the settings UI.
func (s *Service) Status(ctx context.Context, userID string) (hasClient bool, grant *Grant, err error) {
	c, err := s.Store.GetClient(ctx, userID)
	if err != nil && !errors.Is(err, storage.ErrNotFound) {
		return false, nil, err
	}
	hasClient = err == nil && c != nil && strings.TrimSpace(c.ClientID) != ""
	g, gerr := s.Store.GetGrant(ctx, userID, KindGoogle)
	if gerr != nil {
		if errors.Is(gerr, storage.ErrNotFound) {
			return hasClient, nil, nil
		}
		return hasClient, nil, gerr
	}
	return hasClient, g, nil
}

// Disconnect removes the grant but keeps the OAuth client credentials.
func (s *Service) Disconnect(ctx context.Context, userID string) error {
	err := s.Store.DeleteGrant(ctx, userID, KindGoogle)
	if errors.Is(err, storage.ErrNotFound) {
		return nil
	}
	return err
}

// StartConnect begins PKCE authorization; returns the Google authorize URL.
func (s *Service) StartConnect(ctx context.Context, userID, consoleScheme, consoleHost, redirectTo string) (string, error) {
	c, err := s.GetClient(ctx, userID)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(c.ClientSecret) == "" {
		return "", ErrNoClient
	}
	state, err := randomHex(32)
	if err != nil {
		return "", err
	}
	verifier, err := randomBase64(32)
	if err != nil {
		return "", err
	}
	redirectURI := CallbackURL(consoleScheme, consoleHost)
	now := s.now()
	if err := s.Store.CreateState(ctx, State{
		State:       state,
		UserID:      userID,
		Verifier:    verifier,
		RedirectURI: redirectURI,
		RedirectTo:  safeRedirect(redirectTo),
		ExpiresAt:   now.Add(s.stateTTL()),
		CreatedAt:   now,
	}); err != nil {
		return "", err
	}
	return authorizeURL(c.ClientID, state, s256Challenge(verifier), redirectURI), nil
}

// HandleCallback exchanges the code and upserts the grant.
func (s *Service) HandleCallback(ctx context.Context, code, state string) (redirectTo string, err error) {
	st, err := s.Store.TakeState(ctx, state)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return "", ErrStateInvalid
		}
		return "", err
	}
	if st.ExpiresAt.Before(s.now()) {
		return "", ErrStateInvalid
	}
	if strings.TrimSpace(code) == "" {
		return st.RedirectTo, ErrMissingCode
	}
	c, err := s.GetClient(ctx, st.UserID)
	if err != nil {
		return st.RedirectTo, err
	}
	tok, err := s.exchange(ctx, c, code, st.Verifier, st.RedirectURI)
	if err != nil {
		return st.RedirectTo, err
	}
	prof, err := s.userInfo(ctx, tok.AccessToken)
	if err != nil {
		return st.RedirectTo, err
	}
	if _, err := s.Store.UpsertGrant(ctx, Grant{
		UserID:         st.UserID,
		Kind:           KindGoogle,
		Subject:        prof.Subject,
		Email:          prof.Email,
		AccessToken:    tok.AccessToken,
		RefreshToken:   tok.RefreshToken,
		TokenExpiresAt: tok.ExpiresAt,
		Scopes:         tok.Scopes,
	}); err != nil {
		return st.RedirectTo, err
	}
	return st.RedirectTo, nil
}

type tokenResult struct {
	AccessToken  string
	RefreshToken string
	Scopes       string
	ExpiresAt    *time.Time
}

type userProfile struct {
	Subject string
	Email   string
}

func (s *Service) exchange(ctx context.Context, c *GoogleClient, code, verifier, redirectURI string) (*tokenResult, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	return s.tokenRequest(ctx, form)
}

func (s *Service) refresh(ctx context.Context, c *GoogleClient, refreshToken string) (*tokenResult, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", c.ClientID)
	form.Set("client_secret", c.ClientSecret)
	return s.tokenRequest(ctx, form)
}

func (s *Service) tokenRequest(ctx context.Context, form url.Values) (*tokenResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("google token exchange failed: %s", truncateErr(body))
	}
	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
		TokenType    string `json:"token_type"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if raw.AccessToken == "" {
		return nil, fmt.Errorf("google token response missing access_token")
	}
	out := &tokenResult{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		Scopes:       raw.Scope,
	}
	if raw.ExpiresIn > 0 {
		t := s.now().Add(time.Duration(raw.ExpiresIn) * time.Second)
		out.ExpiresAt = &t
	}
	return out, nil
}

func (s *Service) userInfo(ctx context.Context, accessToken string) (*userProfile, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleUserInfo, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("google userinfo failed: %s", truncateErr(body))
	}
	var raw struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if raw.Sub == "" {
		return nil, fmt.Errorf("google userinfo missing sub")
	}
	return &userProfile{Subject: raw.Sub, Email: raw.Email}, nil
}

// AccessToken returns a valid access token for the user's Google grant, refreshing if needed.
func (s *Service) AccessToken(ctx context.Context, userID string) (string, error) {
	g, err := s.Store.GetGrant(ctx, userID, KindGoogle)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return "", ErrNotConnected
		}
		return "", err
	}
	if g.AccessToken != "" && !s.needsRefresh(g) {
		return g.AccessToken, nil
	}
	if strings.TrimSpace(g.RefreshToken) == "" {
		if g.AccessToken != "" {
			return g.AccessToken, nil
		}
		return "", ErrNotConnected
	}
	c, err := s.GetClient(ctx, userID)
	if err != nil {
		return "", err
	}
	tok, err := s.refresh(ctx, c, g.RefreshToken)
	if err != nil {
		if g.AccessToken != "" {
			return g.AccessToken, nil
		}
		return "", fmt.Errorf("refresh google token: %w", err)
	}
	if err := s.Store.SaveGrantTokens(ctx, g.ID, tok.AccessToken, tok.RefreshToken, tok.ExpiresAt); err != nil {
		return "", err
	}
	return tok.AccessToken, nil
}

func (s *Service) needsRefresh(g *Grant) bool {
	if g.TokenExpiresAt == nil {
		return false
	}
	return !g.TokenExpiresAt.After(s.now().Add(refreshWindow))
}

func truncateErr(b []byte) string {
	const max = 240
	s := strings.TrimSpace(string(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	if s == "" {
		return "empty body"
	}
	return s
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
