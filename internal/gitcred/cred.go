// Package gitcred stores per-user personal tokens (PAT) and injects them into
// Agent workspaces. One token per host covers git + tea/gh/glab. Secrets never
// go into images: user provides, platform stores, EnsureAgent writes
// /workspace/.roundpen/git.
package gitcred

import (
	"net/url"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/settings"
)

const (
	ProviderGitHub  = "github"
	ProviderGitea   = "gitea"
	ProviderGitLab  = "gitlab"
	ProviderGeneric = "generic"
)

// Cred is one git host credential owned by a user.
type Cred struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	Provider  string    `json:"provider"`
	Host      string    `json:"host"`
	Username  string    `json:"username"`
	Label     string    `json:"label,omitempty"`
	Token     string    `json:"token,omitempty"`
	HasToken  bool      `json:"hasToken"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// SanitizeForResponse masks the token.
func (c Cred) SanitizeForResponse() Cred {
	out := c
	out.HasToken = strings.TrimSpace(c.Token) != ""
	out.Token = settings.MaskSecret(c.Token)
	return out
}

func normalizeProvider(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case ProviderGitHub, "gh":
		return ProviderGitHub
	case ProviderGitLab, "glab", "gitlab.com":
		return ProviderGitLab
	case ProviderGitea, "tea", "gogs":
		return ProviderGitea
	case ProviderGeneric, "git":
		return ProviderGeneric
	default:
		return ProviderGitea
	}
}

func normalizeHost(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "https://")
	s = strings.TrimPrefix(s, "http://")
	s = strings.TrimPrefix(s, "ssh://")
	s = strings.TrimPrefix(s, "git@")
	if i := strings.Index(s, ":"); i >= 0 {
		// scp-style git@host:owner/repo or host:port
		rest := s[i+1:]
		if !strings.Contains(rest, "/") && looksPort(rest) {
			s = s[:i] // drop :port
		} else {
			s = s[:i] // drop :owner/repo
		}
	}
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(strings.TrimSuffix(s, "/"))
}

func looksPort(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func inferProvider(host string) string {
	h := normalizeHost(host)
	switch {
	case h == "github.com" || strings.HasSuffix(h, ".github.com"):
		return ProviderGitHub
	case h == "gitlab.com" || strings.Contains(h, "gitlab"):
		return ProviderGitLab
	default:
		return ProviderGitea
	}
}

func authUsername(provider, username string) string {
	if u := strings.TrimSpace(username); u != "" {
		return u
	}
	switch provider {
	case ProviderGitHub:
		return "x-access-token"
	case ProviderGitLab:
		return "oauth2"
	default:
		return "git"
	}
}

func credentialURL(c Cred) string {
	u := url.URL{
		Scheme: "https",
		Host:   c.Host,
		User:   url.UserPassword(authUsername(c.Provider, c.Username), c.Token),
	}
	return u.String()
}

func cliEnv(c Cred) map[string]string {
	if strings.TrimSpace(c.Token) == "" {
		return nil
	}
	env := map[string]string{}
	switch c.Provider {
	case ProviderGitHub:
		env["GH_TOKEN"] = c.Token
		env["GITHUB_TOKEN"] = c.Token
	case ProviderGitLab:
		env["GITLAB_TOKEN"] = c.Token
	case ProviderGitea:
		env["GITEA_TOKEN"] = c.Token
		env["TEA_TOKEN"] = c.Token
	}
	return env
}
