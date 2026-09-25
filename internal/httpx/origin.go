package httpx

import (
	"net/http"
	"net/url"
	"strings"
)

// CheckSameOrigin reports whether a WebSocket upgrade request comes from the
// same origin it is served from. Requests without an Origin header (non-browser
// clients) are allowed. publicURL, when set, is accepted as an alternative
// trusted origin (deployments behind a reverse proxy with a public hostname).
func CheckSameOrigin(r *http.Request, publicURL string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if strings.EqualFold(u.Host, r.Host) {
		return true
	}
	if publicURL != "" {
		if pu, err := url.Parse(publicURL); err == nil && strings.EqualFold(u.Host, pu.Host) {
			return true
		}
	}
	return false
}
