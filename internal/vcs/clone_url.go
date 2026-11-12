package vcs

import (
	"net"
	"net/url"
	"strings"
	"time"
)

// NormalizeCloneURL ensures a usable clone URL for common shorthand.
// SCP-style and ssh:// remotes are rewritten to https:// so guest clones never
// invoke OpenSSH (agent UIDs often lack a passwd entry on fnOS).
func NormalizeCloneURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if u := sshToHTTPS(s); u != "" {
		return u
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") ||
		strings.HasPrefix(s, "git://") {
		return s
	}
	// owner/repo
	if !strings.Contains(s, "://") && strings.Count(s, "/") == 1 {
		return "https://github.com/" + s + ".git"
	}
	if u, err := url.Parse("https://" + s); err == nil && u.Host != "" {
		return "https://" + s
	}
	return s
}

// PreferReachableCloneURL rewrites SSH to HTTP(S), then prefers https when
// :443 accepts TCP and falls back to http when only :80 is open (common for
// internal GitLab that terminates TLS elsewhere).
func PreferReachableCloneURL(raw string) string {
	s := NormalizeCloneURL(raw)
	if s == "" {
		return ""
	}
	u, err := url.Parse(s)
	if err != nil || u.Host == "" {
		return s
	}
	host := u.Hostname()
	if host == "" {
		return s
	}
	scheme := preferredHTTPScheme(host)
	if scheme == u.Scheme {
		return s
	}
	u.Scheme = scheme
	return u.String()
}

// CloneHost returns the credential host key for a clone URL (lowercase, no port).
func CloneHost(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "git@") {
		rest := strings.TrimPrefix(s, "git@")
		if i := strings.Index(rest, ":"); i >= 0 {
			return strings.ToLower(rest[:i])
		}
	}
	if u, err := url.Parse(s); err == nil && u.Host != "" {
		return strings.ToLower(u.Hostname())
	}
	// Normalize first (ssh → https), then parse.
	if u, err := url.Parse(NormalizeCloneURL(s)); err == nil && u.Host != "" {
		return strings.ToLower(u.Hostname())
	}
	return ""
}

func sshToHTTPS(s string) string {
	if strings.HasPrefix(s, "git@") {
		rest := strings.TrimPrefix(s, "git@")
		i := strings.Index(rest, ":")
		if i <= 0 {
			return ""
		}
		host := rest[:i]
		path := strings.TrimPrefix(rest[i+1:], "/")
		if host == "" || path == "" {
			return ""
		}
		return "https://" + host + "/" + path
	}
	if strings.HasPrefix(s, "ssh://") {
		u, err := url.Parse(s)
		if err != nil || u.Host == "" {
			return ""
		}
		path := strings.TrimPrefix(u.Path, "/")
		if path == "" {
			return ""
		}
		return "https://" + u.Hostname() + "/" + path
	}
	return ""
}

func preferredHTTPScheme(host string) string {
	d := net.Dialer{Timeout: 2 * time.Second}
	if c, err := d.Dial("tcp", net.JoinHostPort(host, "443")); err == nil {
		_ = c.Close()
		return "https"
	}
	if c, err := d.Dial("tcp", net.JoinHostPort(host, "80")); err == nil {
		_ = c.Close()
		return "http"
	}
	return "https"
}
