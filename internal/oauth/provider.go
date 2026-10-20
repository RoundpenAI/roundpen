// Package oauth implements OAuth2 federated login (GitHub, self-hosted Gitea)
// and projects the resulting tokens into the per-user git credentials.
package oauth

import (
	"fmt"
	"net/url"
	"strings"
)

const (
	KindGitHub = "github"
	KindGitea  = "gitea"
	KindGitLab = "gitlab"

	// DefaultScopes covers login (read:user), a verified email (user:email) and
	// repository read/write for git over HTTPS. GitLab uses its own default.
	DefaultScopes       = "read:user user:email repo"
	DefaultGitLabScopes = "read_user api"
)

// Provider is one remote OAuth application. A Gitea instance is one provider.
type Provider struct {
	ID           string `json:"id"`
	Kind         string `json:"kind"`
	Scheme       string `json:"scheme"`
	Host         string `json:"host"`
	Label        string `json:"label"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	Scopes       string `json:"scopes"`
	AuthURL      string `json:"authUrl"`
	TokenURL     string `json:"tokenUrl"`
	APIURL       string `json:"apiUrl"`
	Enabled      bool   `json:"enabled"`
}

// Normalize validates a provider and fills derived defaults (id, label, URLs).
// Explicit auth/token/api URLs win over the kind + host derivation.
func Normalize(p Provider) (Provider, error) {
	p.Kind = strings.ToLower(strings.TrimSpace(p.Kind))
	switch p.Kind {
	case KindGitHub, KindGitea, KindGitLab:
	default:
		return Provider{}, fmt.Errorf("unsupported provider kind %q", p.Kind)
	}

	if strings.TrimSpace(p.Host) == "" && p.Kind == KindGitHub {
		p.Host = "github.com"
	}
	host, embedded, err := splitHost(p.Host)
	if err != nil {
		return Provider{}, err
	}
	p.Host = host

	p.Scheme = strings.ToLower(strings.TrimSpace(p.Scheme))
	if p.Scheme == "" {
		p.Scheme = embedded
	}
	if p.Scheme == "" {
		p.Scheme = "https"
	}
	if p.Scheme != "https" && p.Scheme != "http" {
		return Provider{}, fmt.Errorf("unsupported scheme %q", p.Scheme)
	}

	if p.ID == "" {
		p.ID = SlugID(p.Kind, p.Host)
	}
	if p.Label == "" {
		p.Label = defaultLabel(p.Kind)
	}
	if p.Scopes == "" {
		p.Scopes = defaultScopes(p.Kind)
	}

	base := p.Scheme + "://" + p.Host
	if p.AuthURL == "" {
		p.AuthURL = defaultAuthURL(p.Kind, base)
	}
	if p.TokenURL == "" {
		p.TokenURL = defaultTokenURL(p.Kind, base)
	}
	if p.APIURL == "" {
		p.APIURL = defaultAPIURL(p.Kind, p.Scheme, p.Host)
	}

	if p.Enabled && strings.TrimSpace(p.ClientID) == "" {
		return Provider{}, fmt.Errorf("client id is required for an enabled provider")
	}
	if p.Enabled && strings.TrimSpace(p.ClientSecret) == "" {
		return Provider{}, fmt.Errorf("client secret is required for an enabled provider")
	}
	return p, nil
}

// CallbackURL is the redirect URI to register on the remote platform, derived
// from the console's own address rather than stored (reverse proxies move).
func (p Provider) CallbackURL(consoleScheme, consoleHost string) string {
	return fmt.Sprintf("%s://%s/v1/auth/oauth/%s/callback", consoleScheme, consoleHost, p.ID)
}

// AuthorizeURL builds the remote authorization-code request with PKCE.
func (p Provider) AuthorizeURL(state, challenge, redirectURI string) string {
	q := url.Values{}
	q.Set("client_id", p.ClientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", p.Scopes)
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	sep := "?"
	if strings.Contains(p.AuthURL, "?") {
		sep = "&"
	}
	return p.AuthURL + sep + q.Encode()
}

// DisplayLabel is the human name used by the login page and settings lists.
func (p Provider) DisplayLabel() string {
	if p.Label != "" {
		return p.Label
	}
	return defaultLabel(p.Kind)
}

func defaultLabel(kind string) string {
	switch kind {
	case KindGitHub:
		return "GitHub"
	case KindGitLab:
		return "GitLab"
	default:
		return "Gitea"
	}
}

func defaultScopes(kind string) string {
	if kind == KindGitLab {
		return DefaultGitLabScopes
	}
	return DefaultScopes
}

func defaultAuthURL(kind, base string) string {
	if kind == KindGitLab {
		return base + "/oauth/authorize"
	}
	return base + "/login/oauth/authorize"
}

func defaultTokenURL(kind, base string) string {
	if kind == KindGitLab {
		return base + "/oauth/token"
	}
	return base + "/login/oauth/access_token"
}

func defaultAPIURL(kind, scheme, host string) string {
	switch kind {
	case KindGitHub:
		if host == "github.com" {
			return "https://api.github.com"
		}
		return scheme + "://" + host + "/api/v3"
	case KindGitLab:
		return scheme + "://" + host + "/api/v4"
	default:
		return scheme + "://" + host + "/api/v1"
	}
}

// RequiresSelfHostedPlugin is true for Gitea, GitLab and GitHub Enterprise.
// github.com is the built-in path and does not need the plugin.
func RequiresSelfHostedPlugin(p Provider) bool {
	switch p.Kind {
	case KindGitea, KindGitLab:
		return true
	case KindGitHub:
		return p.Host != "" && p.Host != "github.com"
	default:
		return false
	}
}

// SlugID derives a stable provider id from kind + host.
func SlugID(kind, host string) string {
	base := kind
	if host != "" && !(kind == KindGitHub && host == "github.com") {
		base += "-" + host
	}
	var b strings.Builder
	lastDash := false
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// splitHost accepts "git.example.com", "git.example.com:3000" or a pasted URL
// and returns the bare lowercased host plus any scheme it carried.
func splitHost(raw string) (host, scheme string, err error) {
	s := strings.TrimSpace(raw)
	if u, perr := url.Parse(s); perr == nil && u.Host != "" {
		scheme = strings.ToLower(u.Scheme)
		s = u.Host
	}
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return "", scheme, fmt.Errorf("host is required")
	}
	return s, scheme, nil
}
