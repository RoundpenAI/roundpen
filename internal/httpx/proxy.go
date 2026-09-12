// Package httpx holds shared HTTP helpers (trusted proxies, client IP, request host).
package httpx

import (
	"net"
	"net/http"
	"strings"
	"sync"
)

// Trust decides whether X-Forwarded-* headers from a request may be honored.
type Trust struct {
	mu   sync.RWMutex
	nets []*net.IPNet
}

// ParseTrust parses a comma-separated list of CIDRs or IPs.
// Empty input means no proxy is trusted (ignore forwarded headers).
func ParseTrust(raw string) (*Trust, error) {
	t := &Trust{}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return t, nil
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "/") {
			if strings.Contains(part, ":") {
				part += "/128"
			} else {
				part += "/32"
			}
		}
		_, n, err := net.ParseCIDR(part)
		if err != nil {
			return nil, err
		}
		t.nets = append(t.nets, n)
	}
	return t, nil
}

// SetNets replaces the trusted CIDR list (tests).
func (t *Trust) SetNets(nets []*net.IPNet) {
	t.mu.Lock()
	t.nets = nets
	t.mu.Unlock()
}

// TrustsRemote reports whether r.RemoteAddr is a configured trusted hop.
func (t *Trust) TrustsRemote(r *http.Request) bool {
	if t == nil {
		return false
	}
	t.mu.RLock()
	nets := t.nets
	t.mu.RUnlock()
	if len(nets) == 0 {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// ClientIP returns the client address. X-Forwarded-For is used only from a trusted hop.
func (t *Trust) ClientIP(r *http.Request) string {
	if t.TrustsRemote(r) {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			return strings.TrimSpace(strings.Split(xff, ",")[0])
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// Scheme is https when the request is TLS, or when a trusted proxy sent X-Forwarded-Proto.
func (t *Trust) Scheme(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if t.TrustsRemote(r) {
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			return strings.ToLower(strings.TrimSpace(strings.Split(proto, ",")[0]))
		}
	}
	return "http"
}

// Host is r.Host, or X-Forwarded-Host when the peer is a trusted proxy.
func (t *Trust) Host(r *http.Request) string {
	if t.TrustsRemote(r) {
		if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
			return strings.TrimSpace(strings.Split(fwd, ",")[0])
		}
	}
	return r.Host
}

// DefaultTrust is process-wide and empty until SetDefaultTrust.
var DefaultTrust = &Trust{}

// SetDefaultTrust replaces the process-wide proxy trust list.
func SetDefaultTrust(t *Trust) {
	if t == nil {
		t = &Trust{}
	}
	DefaultTrust = t
}
