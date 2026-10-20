package oauth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// CloudGitHubProviderID is the official relay binding. It is not a row in
// oauth_providers and it is not a login method: a signed-in user authorizes
// it so the agent can operate that user's GitHub repos.
const CloudGitHubProviderID = "github-cloud"

// InstallSigner mints the rp1 credential the relay uses to recognize this install.
type InstallSigner interface {
	MintToken() (string, error)
	PublicKeyHex() string
}

// GitHubRelay is the relay origin (https://auth.example). Empty disables it.
// Install signs outbound calls. HTTP is optional.
func (s *Service) cloudOn() bool {
	return s != nil && strings.TrimSpace(s.GitHubRelay) != "" && s.Install != nil
}

func (s *Service) cloudGitHub() Provider {
	return Provider{
		ID:      CloudGitHubProviderID,
		Kind:    KindGitHub,
		Scheme:  "https",
		Host:    "github.com",
		Label:   "GitHub",
		Scopes:  DefaultScopes,
		Enabled: true,
	}
}

func (s *Service) hasOwnGitHub(ctx context.Context) bool {
	list, err := s.Store.ListProviders(ctx, true)
	if err != nil {
		return false
	}
	for _, p := range list {
		if p.Kind == KindGitHub && strings.EqualFold(p.Host, "github.com") {
			return true
		}
	}
	return false
}

type relayTicket struct {
	Login        string `json:"login"`
	Name         string `json:"name"`
	Email        string `json:"email"`
	AccessToken  string `json:"accessToken"`
	TokenType    string `json:"tokenType"`
	Scope        string `json:"scope"`
	RefreshToken string `json:"refreshToken"`
	ExpiresIn    int    `json:"expiresIn"`
}

func (s *Service) cloudAuthorize(ctx context.Context, in StartInput) (string, error) {
	if strings.TrimSpace(in.LinkUser) == "" {
		return "", ErrProviderUnavailable
	}
	if !s.cloudOn() {
		return "", ErrProviderUnavailable
	}
	if s.hasOwnGitHub(ctx) {
		return "", ErrProviderUnavailable
	}
	state, err := randomHex(32)
	if err != nil {
		return "", err
	}
	now := s.now()
	origin := in.ConsoleScheme + "://" + in.ConsoleHost
	if err := s.Store.CreateState(ctx, State{
		State:       state,
		ProviderID:  CloudGitHubProviderID,
		RedirectURI: origin,
		RedirectTo:  safeRedirect(in.RedirectTo),
		LinkUser:    in.LinkUser,
		ExpiresAt:   now.Add(s.stateTTL()),
		CreatedAt:   now,
	}); err != nil {
		return "", err
	}
	var resp struct {
		AuthorizeURL string `json:"authorizeUrl"`
	}
	if err := s.relayJSON(ctx, http.MethodPost, "/github/start", map[string]string{
		"return": origin,
		"state":  state,
	}, &resp); err != nil {
		return "", err
	}
	if strings.TrimSpace(resp.AuthorizeURL) == "" {
		return "", fmt.Errorf("github relay returned no authorize url")
	}
	return resp.AuthorizeURL, nil
}

// CloudCallbackInput is the browser landing the relay sends back.
type CloudCallbackInput struct {
	Ticket      string
	State       string
	Error       string
	SessionUser string
}

// HandleCloudCallback redeems a one-time ticket into the signed-in user's
// GitHub identity. The token is what the agent uses to operate GitHub.
func (s *Service) HandleCloudCallback(ctx context.Context, in CloudCallbackInput) (*CallbackResult, error) {
	if in.Error != "" {
		return nil, ErrProviderUnavailable
	}
	st, err := s.Store.TakeState(ctx, in.State)
	if err != nil {
		return nil, ErrStateInvalid
	}
	if st.ProviderID != CloudGitHubProviderID {
		return nil, ErrStateInvalid
	}
	if st.LinkUser != "" && st.LinkUser != in.SessionUser {
		return nil, ErrStateUserMismatch
	}
	ticket := strings.TrimSpace(in.Ticket)
	if ticket == "" {
		return nil, fmt.Errorf("github relay ticket is missing")
	}
	var body relayTicket
	ticketPath := "/tickets/" + url.PathEscape(ticket) + "?state=" + url.QueryEscape(in.State)
	if err := s.relayJSON(ctx, http.MethodGet, ticketPath, nil, &body); err != nil {
		return nil, err
	}
	if strings.TrimSpace(body.Login) == "" || strings.TrimSpace(body.AccessToken) == "" {
		return nil, fmt.Errorf("github relay ticket is incomplete")
	}
	p := s.cloudGitHub()
	var exp *time.Time
	if body.ExpiresIn > 0 {
		t := s.now().Add(time.Duration(body.ExpiresIn) * time.Second)
		exp = &t
	}
	prof := Profile{
		Subject:       body.Login,
		Login:         body.Login,
		Name:          body.Name,
		VerifiedEmail: strings.TrimSpace(body.Email),
	}
	user, linked, err := s.resolveUser(ctx, p, st, prof)
	if err != nil {
		return nil, err
	}
	if st.LinkUser != "" {
		linked = true
	}
	if _, err := s.Store.UpsertIdentity(ctx, IdentityUpsert{
		UserID:       user.Username,
		ProviderID:   p.ID,
		Subject:      prof.Subject,
		Login:        prof.Login,
		Name:         prof.Name,
		Email:        prof.VerifiedEmail,
		AccessToken:  body.AccessToken,
		RefreshToken: body.RefreshToken,
		ExpiresAt:    exp,
		Scopes:       body.Scope,
		MarkLogin:    !linked,
	}); err != nil {
		return nil, err
	}
	s.identityChanged(ctx, user.Username)
	return &CallbackResult{User: user, RedirectTo: safeRedirect(st.RedirectTo), Linked: linked}, nil
}

func (s *Service) relayJSON(ctx context.Context, method, path string, payload any, dest any) error {
	if !s.cloudOn() {
		return ErrProviderUnavailable
	}
	token, err := s.Install.MintToken()
	if err != nil {
		return err
	}
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(raw)
	}
	base := strings.TrimRight(strings.TrimSpace(s.GitHubRelay), "/")
	req, err := http.NewRequestWithContext(ctx, method, base+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Roundpen-Install-Pub", s.Install.PublicKeyHex())
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	client := s.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return err
	}
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("oauth relay %s: http %d", path, res.StatusCode)
	}
	if dest == nil {
		return nil
	}
	if err := json.Unmarshal(raw, dest); err != nil {
		return fmt.Errorf("oauth relay %s: %w", path, err)
	}
	return nil
}
