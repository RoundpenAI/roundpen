package mcp

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

	"github.com/RoundpenAI/roundpen/internal/httpx"
)

// ProtectedResource is the RFC 9728 metadata document served by the MCP
// endpoint (or linked via WWW-Authenticate).
type ProtectedResource struct {
	Resource             string   `json:"resource"`
	AuthorizationServers []string `json:"authorization_servers"`
	ScopesSupported      []string `json:"scopes_supported"`
}

// ASMetadata is the RFC 8414 authorization server metadata subset we use.
type ASMetadata struct {
	Issuer                   string   `json:"issuer"`
	AuthorizationEndpoint    string   `json:"authorization_endpoint"`
	TokenEndpoint            string   `json:"token_endpoint"`
	RegistrationEndpoint     string   `json:"registration_endpoint"`
	RevocationEndpoint       string   `json:"revocation_endpoint"`
	ScopesSupported          []string `json:"scopes_supported"`
	TokenEndpointAuthMethods []string `json:"token_endpoint_auth_methods_supported"`
}

// TokenError is an OAuth error response (RFC 6749 section 5.2).
type TokenError struct {
	Code        string
	Description string
	Status      int
}

func (e *TokenError) Error() string {
	msg := e.Code
	if e.Description != "" {
		msg += ": " + e.Description
	}
	return fmt.Sprintf("oauth token endpoint rejected the request (%s)", msg)
}

// IsInvalidGrant reports refreshes the server permanently rejected.
func IsInvalidGrant(err error) bool {
	var te *TokenError
	return errors.As(err, &te) && te.Code == "invalid_grant"
}

// Tokens is one token endpoint response.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	Scopes       string
	ExpiresAt    *time.Time
}

// Discover finds the protected resource metadata for an MCP endpoint:
// first via the WWW-Authenticate challenge, then the well-known path.
func Discover(ctx context.Context, hc *http.Client, mcpEndpoint string) (*ProtectedResource, error) {
	metadataURL := ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, mcpEndpoint, nil)
	if err == nil {
		req.Header.Set("Accept", "application/json, text/event-stream")
		if resp, err := hc.Do(req); err == nil {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
			resp.Body.Close()
			metadataURL = parseResourceMetadata(resp.Header.Get("WWW-Authenticate"))
		}
	}
	if metadataURL == "" {
		metadataURL, err = wellKnownURL(mcpEndpoint, "/.well-known/oauth-protected-resource")
		if err != nil {
			return nil, err
		}
	}
	var prm ProtectedResource
	if err := getJSON(ctx, hc, metadataURL, &prm); err != nil {
		return nil, fmt.Errorf("protected resource metadata: %w", err)
	}
	if len(prm.AuthorizationServers) == 0 {
		return nil, fmt.Errorf("protected resource metadata has no authorization server")
	}
	if prm.Resource == "" {
		prm.Resource = mcpEndpoint
	}
	return &prm, nil
}

// FetchASMetadata loads RFC 8414 metadata for an authorization server.
func FetchASMetadata(ctx context.Context, hc *http.Client, asURL string) (*ASMetadata, error) {
	candidates := []string{}
	if u, err := wellKnownURL(asURL, "/.well-known/oauth-authorization-server"); err == nil {
		candidates = append(candidates, u)
	}
	if u, err := wellKnownURL(asURL, ""); err == nil {
		candidates = append(candidates, u)
	}
	var lastErr error
	for _, cand := range candidates {
		var meta ASMetadata
		if err := getJSON(ctx, hc, cand, &meta); err != nil {
			lastErr = err
			continue
		}
		if meta.AuthorizationEndpoint == "" || meta.TokenEndpoint == "" {
			lastErr = fmt.Errorf("authorization server metadata is missing endpoints")
			continue
		}
		return &meta, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no authorization server metadata")
	}
	return nil, lastErr
}

// RegisterClient performs RFC 7591 dynamic client registration. It returns
// ErrDCRUnsupported when the server exposes no registration endpoint.
func RegisterClient(ctx context.Context, hc *http.Client, meta *ASMetadata, redirectURI string) (clientID, clientSecret string, err error) {
	if meta.RegistrationEndpoint == "" {
		return "", "", ErrDCRUnsupported
	}
	authMethod := pickTokenAuthMethod(meta.TokenEndpointAuthMethods)
	payload := map[string]any{
		"client_name":                "Roundpen",
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": authMethod,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, meta.RegistrationEndpoint, strings.NewReader(string(body)))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("client registration: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", fmt.Errorf("client registration failed: HTTP %d: %s", resp.StatusCode, truncateErr(string(raw)))
	}
	var out struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", fmt.Errorf("client registration response: %w", err)
	}
	if out.ClientID == "" {
		return "", "", fmt.Errorf("client registration response missing client_id")
	}
	if authMethod == "none" {
		out.ClientSecret = ""
	}
	return out.ClientID, out.ClientSecret, nil
}

// BuildAuthorizeURL assembles the PKCE authorization request (RFC 8707
// resource included so the AS binds the token to the MCP endpoint).
func BuildAuthorizeURL(meta *ASMetadata, clientID, state, challenge, redirectURI, resource, scope string) string {
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	if scope != "" {
		q.Set("scope", scope)
	}
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	if resource != "" {
		q.Set("resource", resource)
	}
	return meta.AuthorizationEndpoint + "?" + q.Encode()
}

// ExchangeCode swaps an authorization code for tokens.
func ExchangeCode(ctx context.Context, hc *http.Client, tokenEndpoint, clientID, clientSecret, code, verifier, redirectURI, resource string) (*Tokens, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	form.Set("code_verifier", verifier)
	if resource != "" {
		form.Set("resource", resource)
	}
	return tokenRequest(ctx, hc, tokenEndpoint, form)
}

// RefreshToken exchanges a refresh token for a new access token.
func RefreshToken(ctx context.Context, hc *http.Client, tokenEndpoint, clientID, clientSecret, refreshToken, resource string) (*Tokens, error) {
	if strings.TrimSpace(tokenEndpoint) == "" {
		return nil, fmt.Errorf("connection has no token endpoint")
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	if resource != "" {
		form.Set("resource", resource)
	}
	return tokenRequest(ctx, hc, tokenEndpoint, form)
}

// RevokeToken asks the AS to invalidate a token (RFC 7009, best effort).
func RevokeToken(ctx context.Context, hc *http.Client, revocationEndpoint, clientID, clientSecret, token string) {
	if revocationEndpoint == "" || token == "" {
		return
	}
	form := url.Values{}
	form.Set("token", token)
	form.Set("client_id", clientID)
	if clientSecret != "" {
		form.Set("client_secret", clientSecret)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, revocationEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if resp, err := hc.Do(req); err == nil {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		resp.Body.Close()
	}
}

func tokenRequest(ctx context.Context, hc *http.Client, endpoint string, form url.Values) (*Tokens, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var oe struct {
			Error       string `json:"error"`
			Description string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &oe)
		if oe.Error == "" {
			oe.Error = truncateErr(string(body))
		}
		return nil, &TokenError{Code: oe.Error, Description: oe.Description, Status: resp.StatusCode}
	}
	var raw struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Scope        string `json:"scope"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}
	if raw.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}
	out := &Tokens{
		AccessToken:  raw.AccessToken,
		RefreshToken: raw.RefreshToken,
		Scopes:       raw.Scope,
	}
	if raw.ExpiresIn > 0 {
		t := time.Now().UTC().Add(time.Duration(raw.ExpiresIn) * time.Second)
		out.ExpiresAt = &t
	}
	return out, nil
}

// StartOAuth runs discovery and dynamic registration, then returns the
// authorization URL for the user to visit.
func (s *Service) StartOAuth(ctx context.Context, userID, serverID, consoleScheme, consoleHost, redirectTo string) (string, error) {
	sv, ok := Lookup(serverID)
	if !ok {
		return "", ErrServerUnknown
	}
	if sv.Auth == AuthAPIKey {
		return "", ErrNotSupported
	}
	prm, err := Discover(ctx, s.httpClient(), sv.Endpoint)
	if err != nil {
		return "", err
	}
	meta, err := FetchASMetadata(ctx, s.httpClient(), prm.AuthorizationServers[0])
	if err != nil {
		return "", err
	}
	redirectURI := CallbackURL(consoleScheme, consoleHost)
	clientID, clientSecret, err := RegisterClient(ctx, s.httpClient(), meta, redirectURI)
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
	scopes := strings.Join(pickScopes(prm, meta), " ")
	now := s.now()
	if err := s.Store.CreateState(ctx, State{
		State:                 state,
		UserID:                userID,
		ServerID:              sv.ID,
		Verifier:              verifier,
		RedirectURI:           redirectURI,
		RedirectTo:            safeRedirect(redirectTo),
		Resource:              prm.Resource,
		AuthorizationEndpoint: meta.AuthorizationEndpoint,
		TokenEndpoint:         meta.TokenEndpoint,
		ClientID:              clientID,
		ClientSecret:          clientSecret,
		Scopes:                scopes,
		ExpiresAt:             now.Add(s.stateTTL()),
		CreatedAt:             now,
	}); err != nil {
		return "", err
	}
	url := BuildAuthorizeURL(meta, clientID, state, s256Challenge(verifier), redirectURI, prm.Resource, scopes)
	return url, nil
}

// HandleCallback finishes the flow: exchange, persist the connection, and
// snapshot the tool list. A tool listing failure keeps the connection with a
// recorded error instead of dropping the grant. serverID is empty when the
// state could not be consumed.
func (s *Service) HandleCallback(ctx context.Context, code, state, errParam string) (redirectTo, serverID string, err error) {
	st, err := s.Store.TakeState(ctx, state)
	if err != nil {
		return "", "", ErrStateInvalid
	}
	if st.ExpiresAt.Before(s.now()) {
		return st.RedirectTo, st.ServerID, ErrStateInvalid
	}
	if errParam != "" {
		return st.RedirectTo, st.ServerID, ErrDenied
	}
	if strings.TrimSpace(code) == "" {
		return st.RedirectTo, st.ServerID, ErrMissingCode
	}
	sv, ok := Lookup(st.ServerID)
	if !ok {
		return st.RedirectTo, st.ServerID, ErrServerUnknown
	}
	tok, err := ExchangeCode(ctx, s.httpClient(), st.TokenEndpoint, st.ClientID, st.ClientSecret, code, st.Verifier, st.RedirectURI, st.Resource)
	if err != nil {
		return st.RedirectTo, st.ServerID, err
	}
	conn := Connection{
		UserID:         st.UserID,
		ServerID:       sv.ID,
		AuthKind:       AuthKindOAuth,
		Status:         StatusConnected,
		AccessToken:    tok.AccessToken,
		RefreshToken:   tok.RefreshToken,
		TokenExpiresAt: tok.ExpiresAt,
		Scopes:         tok.Scopes,
		TokenEndpoint:  st.TokenEndpoint,
		ClientID:       st.ClientID,
		ClientSecret:   st.ClientSecret,
		Resource:       st.Resource,
	}
	saved, err := s.Store.UpsertConnection(ctx, conn)
	if err != nil {
		return st.RedirectTo, st.ServerID, err
	}
	tools, err := s.probe(ctx, sv.Endpoint, s.bearer(ctx, saved))
	if err != nil {
		s.markError(ctx, saved, err)
		return st.RedirectTo, st.ServerID, nil
	}
	now := s.now()
	if err := s.Store.SaveTools(ctx, saved.ID, tools, now); err != nil {
		return st.RedirectTo, st.ServerID, err
	}
	return st.RedirectTo, st.ServerID, nil
}

// CallbackURL is the redirect URI users authorize against.
func CallbackURL(consoleScheme, consoleHost string) string {
	scheme := strings.TrimSpace(consoleScheme)
	host := strings.TrimSpace(consoleHost)
	if scheme == "" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/v1/connectors/mcp/callback", scheme, host)
}

func safeRedirect(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return httpx.ConsolePath("/settings/apps")
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return httpx.ConsolePath("/settings/apps")
	}
	u, err := url.Parse(path)
	if err != nil || u.Host != "" || u.Scheme != "" {
		return httpx.ConsolePath("/settings/apps")
	}
	return u.RequestURI()
}

// pickScopes prefers scopes advertised by the protected resource, then the
// authorization server. Empty means "let the server decide".
func pickScopes(prm *ProtectedResource, meta *ASMetadata) []string {
	scopes := prm.ScopesSupported
	if len(scopes) == 0 {
		scopes = meta.ScopesSupported
	}
	out := make([]string, 0, len(scopes))
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// pickTokenAuthMethod follows MCP's public-client convention: prefer "none"
// (PKCE-only), fall back to client_secret_post.
func pickTokenAuthMethod(methods []string) string {
	for _, m := range methods {
		if m == "none" {
			return "none"
		}
	}
	for _, m := range methods {
		if m == "client_secret_post" {
			return "client_secret_post"
		}
	}
	return "none"
}

func parseResourceMetadata(header string) string {
	const key = "resource_metadata="
	i := strings.Index(strings.ToLower(header), key)
	if i < 0 {
		return ""
	}
	rest := strings.TrimSpace(header[i+len(key):])
	if strings.HasPrefix(rest, "\"") {
		rest = rest[1:]
		if j := strings.Index(rest, "\""); j >= 0 {
			return rest[:j]
		}
		return ""
	}
	if j := strings.IndexAny(rest, " ,;"); j >= 0 {
		return rest[:j]
	}
	return rest
}

// wellKnownURL joins a /.well-known/... suffix: the resource path is appended
// after the well-known segment (RFC 9728 / 8414 path form).
func wellKnownURL(rawURL, wellKnownPath string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid URL %q", rawURL)
	}
	base := u.Scheme + "://" + u.Host
	if wellKnownPath == "" {
		if u.Path == "" || u.Path == "/" {
			return base + "/.well-known/oauth-authorization-server", nil
		}
		return base + "/.well-known/oauth-authorization-server" + strings.TrimSuffix(u.Path, "/"), nil
	}
	return base + wellKnownPath + strings.TrimSuffix(u.Path, "/"), nil
}

func getJSON(ctx context.Context, hc *http.Client, url string, dest any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncateErr(string(body)))
	}
	if err := json.Unmarshal(body, dest); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}
	return nil
}

// issuerFromTokenEndpoint approximates the authorization server URL from its
// token endpoint (scheme://host) so revocation metadata can be fetched on
// disconnect. Best-effort: an empty result skips revocation.
func issuerFromTokenEndpoint(tokenEndpoint string) string {
	u, err := url.Parse(strings.TrimSpace(tokenEndpoint))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
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
