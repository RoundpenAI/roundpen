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
//
// When the request carries fnOS gateway identity headers (X-Trim-Userid), the
// browser Origin is also matched against X-Forwarded-Host. Those headers are
// only present on the Unix-socket gateway chain (the TCP listener strips them),
// so a cross-site page cannot forge them to bypass this check.
func CheckSameOrigin(r *http.Request, publicURL string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	// Sandboxed / opaque iframe documents send Origin: null. The fnOS desktop
	// embeds the console that way; gateway identity (X-Trim-*) is only present
	// on the Unix-socket chain, so this is not forgeable from the TCP listener.
	if strings.EqualFold(origin, "null") {
		return r.Header.Get("X-Trim-Userid") != ""
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if hostAllowed(u.Host, requestHosts(r)...) {
		return true
	}
	if publicURL != "" {
		if pu, err := url.Parse(publicURL); err == nil && hostAllowed(u.Host, pu.Host) {
			return true
		}
	}
	return false
}

// requestHosts lists Host values that may legitimately match a browser Origin
// for this request: the effective (possibly forwarded) host, the raw Host
// header, and — on fnOS gateway requests — X-Forwarded-Host even when the
// Unix peer is not yet in ROUNDPEN_TRUSTED_PROXIES.
func requestHosts(r *http.Request) []string {
	out := make([]string, 0, 3)
	if h := strings.TrimSpace(DefaultTrust.Host(r)); h != "" {
		out = append(out, h)
	}
	if r.Host != "" && (len(out) == 0 || !strings.EqualFold(r.Host, out[0])) {
		out = append(out, r.Host)
	}
	if r.Header.Get("X-Trim-Userid") != "" {
		if fwd := firstForwardedHost(r); fwd != "" {
			out = append(out, fwd)
		}
	}
	return out
}

func firstForwardedHost(r *http.Request) string {
	fwd := strings.TrimSpace(r.Header.Get("X-Forwarded-Host"))
	if fwd == "" {
		return ""
	}
	return strings.TrimSpace(strings.Split(fwd, ",")[0])
}

func hostAllowed(originHost string, candidates ...string) bool {
	for _, c := range candidates {
		if c != "" && strings.EqualFold(originHost, c) {
			return true
		}
	}
	return false
}
