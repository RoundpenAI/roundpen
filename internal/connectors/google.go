// Package connectors implements per-user Google OAuth connectors
// (Gmail + Calendar read-only) for the personal agent.
package connectors

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

const (
	// KindGoogle is the grant kind for the combined Gmail+Calendar consent.
	KindGoogle = "google"

	googleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL = "https://oauth2.googleapis.com/token"
	googleUserInfo = "https://openidconnect.googleapis.com/v1/userinfo"

	gmailAPIBase    = "https://gmail.googleapis.com/gmail/v1"
	calendarAPIBase = "https://www.googleapis.com/calendar/v3"

	defaultStateTTL   = 10 * time.Minute
	refreshWindow     = 5 * time.Minute
	httpTimeout       = 20 * time.Second
	maxMessageBytes   = 64 << 10 // 64 KiB body cap for GmailGet
	defaultMaxResults = 10
	hardMaxResults    = 50
)

// DefaultScopes cover read-only Gmail and Calendar plus identity for display.
const DefaultScopes = "openid email https://www.googleapis.com/auth/gmail.readonly https://www.googleapis.com/auth/calendar.readonly"

var (
	// ErrNoClient means the user has not saved a Google OAuth client yet.
	ErrNoClient = errors.New("google OAuth client is not configured; add Client ID and Secret in Settings → Connectors")
	// ErrNotConnected means there is no grant (or it cannot be refreshed).
	ErrNotConnected = errors.New("google is not connected; open Settings → Connectors and connect")
	// ErrStateInvalid covers missing, expired and replayed states.
	ErrStateInvalid = errors.New("authorization state is invalid or expired")
	// ErrStateUserMismatch is a connect finishing for the wrong user (should not happen for our flow).
	ErrStateUserMismatch = errors.New("authorization was started by a different user")
	// ErrMissingCode is a callback without an authorization code.
	ErrMissingCode = errors.New("missing authorization code")
)

// GoogleClient is the user-owned OAuth application credentials.
type GoogleClient struct {
	UserID       string    `json:"userId"`
	ClientID     string    `json:"clientId"`
	ClientSecret string    `json:"clientSecret"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// Grant is a connected Google account with tokens.
type Grant struct {
	ID             string     `json:"id"`
	UserID         string     `json:"userId"`
	Kind           string     `json:"kind"`
	Subject        string     `json:"subject"`
	Email          string     `json:"email"`
	AccessToken    string     `json:"-"`
	RefreshToken   string     `json:"-"`
	TokenExpiresAt *time.Time `json:"-"`
	Scopes         string     `json:"scopes"`
	// Source is "relay" when the shared official app issued the grant.
	// Empty means the user's own Google client.
	Source    string    `json:"-"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// GrantSourceRelay marks a grant whose refresh token is bound to this install
// on the shared OAuth relay. The client secret stays on the relay.
const GrantSourceRelay = "relay"

// State is one in-flight Google connect flow.
type State struct {
	State       string
	UserID      string
	Verifier    string
	RedirectURI string
	RedirectTo  string
	ExpiresAt   time.Time
	CreatedAt   time.Time
}

// CallbackURL is the redirect URI users register in Google Cloud Console.
func CallbackURL(consoleScheme, consoleHost string) string {
	scheme := strings.TrimSpace(consoleScheme)
	host := strings.TrimSpace(consoleHost)
	if scheme == "" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/v1/connectors/google/callback", scheme, host)
}

func authorizeURL(clientID, state, challenge, redirectURI string) string {
	q := url.Values{}
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", DefaultScopes)
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("access_type", "offline")
	q.Set("prompt", "consent")
	return googleAuthURL + "?" + q.Encode()
}

func safeRedirect(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "/settings/connectors"
	}
	if !strings.HasPrefix(path, "/") || strings.HasPrefix(path, "//") {
		return "/settings/connectors"
	}
	u, err := url.Parse(path)
	if err != nil || u.Host != "" || u.Scheme != "" {
		return "/settings/connectors"
	}
	return u.RequestURI()
}
