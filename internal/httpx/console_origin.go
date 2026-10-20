package httpx

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// ParseConsoleOrigin validates an absolute console URL and returns its origin
// (scheme://host[:port]). Public hosts must use https; http is only allowed
// for localhost / LAN / .local-style names — same rules as the OAuth relay.
func ParseConsoleOrigin(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("return origin is required")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("return must be an absolute URL")
	}
	if u.User != nil {
		return "", fmt.Errorf("return URL must not include credentials")
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", fmt.Errorf("return URL must be http or https")
	}
	host := u.Hostname()
	if host == "" {
		return "", fmt.Errorf("return URL needs a host")
	}
	if scheme == "http" && !IsLocalOrPrivateHost(host) {
		return "", fmt.Errorf("http return is only allowed for localhost / LAN / .local hosts")
	}
	return u.Scheme + "://" + u.Host, nil
}

// IsLocalOrPrivateHost reports whether hostname is loopback, RFC1918, link-local,
// ULA, or a common LAN suffix (.local / .lan / …).
func IsLocalOrPrivateHost(hostname string) bool {
	host := strings.ToLower(strings.Trim(hostname, "[]"))
	switch {
	case host == "localhost", host == "localhost.", strings.HasSuffix(host, ".localhost"):
		return true
	case strings.HasSuffix(host, ".local"), strings.HasSuffix(host, ".lan"),
		strings.HasSuffix(host, ".home"), strings.HasSuffix(host, ".internal"):
		return true
	case host == "::1", host == "0:0:0:0:0:0:0:1":
		return true
	case strings.HasPrefix(host, "fe80:"):
		return true
	case strings.HasPrefix(host, "fc"), strings.HasPrefix(host, "fd"):
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() {
		return true
	}
	return false
}

// ConsoleOrigin prefers the browser-facing origin the client reports, then the
// Origin header. It does not invent a host from ROUNDPEN_* env or guess from a
// proxy upstream address.
//
// Prefer: explicit return (body) → Origin → Referer origin → Trust Host/Scheme.
func ConsoleOrigin(r *http.Request, explicit string) (scheme, host string, err error) {
	candidates := []string{explicit}
	if o := strings.TrimSpace(r.Header.Get("Origin")); o != "" && o != "null" {
		candidates = append(candidates, o)
	}
	if ref := strings.TrimSpace(r.Header.Get("Referer")); ref != "" {
		if u, e := url.Parse(ref); e == nil && u.Scheme != "" && u.Host != "" {
			candidates = append(candidates, u.Scheme+"://"+u.Host)
		}
	}
	for _, c := range candidates {
		if strings.TrimSpace(c) == "" {
			continue
		}
		origin, e := ParseConsoleOrigin(c)
		if e != nil {
			continue
		}
		u, _ := url.Parse(origin)
		return u.Scheme, u.Host, nil
	}
	// Last resort for non-browser API clients (no Origin / return).
	scheme = DefaultTrust.Scheme(r)
	host = DefaultTrust.Host(r)
	if strings.TrimSpace(host) == "" {
		return "", "", fmt.Errorf("return origin is required")
	}
	origin, err := ParseConsoleOrigin(scheme + "://" + host)
	if err != nil {
		return "", "", err
	}
	u, _ := url.Parse(origin)
	return u.Scheme, u.Host, nil
}
