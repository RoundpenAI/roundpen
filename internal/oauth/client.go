package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Token is an access token plus what is needed to refresh it.
type Token struct {
	AccessToken  string
	RefreshToken string
	Scopes       string
	// ExpiresAt is nil when the remote does not expire the token (GitHub's
	// default) — refresh is then never attempted.
	ExpiresAt *time.Time
}

// Profile is the remote account behind a token.
type Profile struct {
	Subject string
	Login   string
	Name    string
	// Email is the profile's public email (unverified, display only).
	Email string
	// VerifiedEmail comes from the provider's emails API and is the only value
	// allowed to match an existing local account.
	VerifiedEmail string
}

// Client performs the remote OAuth2 HTTP calls for one provider.
type Client struct {
	Provider Provider
	HTTP     *http.Client
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Exchange trades an authorization code (with its PKCE verifier) for tokens.
func (c *Client) Exchange(ctx context.Context, code, verifier, redirectURI string) (*Token, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	return c.tokenRequest(ctx, form)
}

// Refresh trades a refresh token for a fresh access token. Providers that
// rotate refresh tokens return a new one; it is persisted when present.
func (c *Client) Refresh(ctx context.Context, refreshToken string) (*Token, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	return c.tokenRequest(ctx, form)
}

func (c *Client) tokenRequest(ctx context.Context, form url.Values) (*Token, error) {
	form.Set("client_id", c.Provider.ClientID)
	form.Set("client_secret", c.Provider.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Provider.TokenURL,
		strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}

	var payload struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Scopes       string `json:"scope"`
		ExpiresIn    int64  `json:"expires_in"`
		Error        string `json:"error"`
		Description  string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("oauth token endpoint: %s (status %d)", http.StatusText(resp.StatusCode), resp.StatusCode)
	}
	if payload.Error != "" {
		return nil, fmt.Errorf("oauth token endpoint: %s: %s", payload.Error, payload.Description)
	}
	if resp.StatusCode/100 != 2 || payload.AccessToken == "" {
		return nil, fmt.Errorf("oauth token endpoint: no access token (status %d)", resp.StatusCode)
	}

	tok := &Token{
		AccessToken:  payload.AccessToken,
		RefreshToken: payload.RefreshToken,
		Scopes:       strings.ReplaceAll(payload.Scopes, ",", " "),
	}
	if payload.ExpiresIn > 0 {
		exp := time.Now().UTC().Add(time.Duration(payload.ExpiresIn) * time.Second)
		tok.ExpiresAt = &exp
	}
	return tok, nil
}

const userAgent = "roundpen"

// Profile fetches the remote account. A failing emails lookup only costs the
// verified email; it never fails the login.
func (c *Client) Profile(ctx context.Context, accessToken string) (Profile, error) {
	var me struct {
		ID       any    `json:"id"`
		Login    string `json:"login"`
		Name     string `json:"name"`
		FullName string `json:"full_name"`
		Email    string `json:"email"`
	}
	if err := c.apiGet(ctx, c.Provider.APIURL+"/user", accessToken, &me); err != nil {
		return Profile{}, err
	}
	name := me.Name
	if name == "" {
		name = me.FullName
	}
	p := Profile{
		Subject: subjectOf(me.ID),
		Login:   me.Login,
		Name:    name,
		Email:   me.Email,
	}
	if p.Subject == "" || p.Login == "" {
		return Profile{}, fmt.Errorf("oauth profile: missing id or login")
	}
	p.VerifiedEmail = c.primaryVerifiedEmail(ctx, accessToken)
	return p, nil
}

// primaryVerifiedEmail asks the provider's emails API; failures are non-fatal.
func (c *Client) primaryVerifiedEmail(ctx context.Context, accessToken string) string {
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := c.apiGet(ctx, c.Provider.APIURL+"/user/emails", accessToken, &emails); err != nil {
		return ""
	}
	fallback := ""
	for _, e := range emails {
		if !e.Verified || e.Email == "" {
			continue
		}
		if e.Primary {
			return e.Email
		}
		if fallback == "" {
			fallback = e.Email
		}
	}
	return fallback
}

func (c *Client) apiGet(ctx context.Context, rawURL, accessToken string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	if c.Provider.Kind == KindGitHub {
		req.Header.Set("Accept", "application/vnd.github+json")
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("oauth api %s: status %d", rawURL, resp.StatusCode)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

// subjectOf renders the remote user id, which GitHub/Gitea send as a number.
func subjectOf(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case json.Number:
		return t.String()
	default:
		return ""
	}
}
