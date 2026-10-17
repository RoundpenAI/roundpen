package httpx

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// CheckSameOrigin reports whether a WebSocket upgrade request comes from the
// same origin it is served from. Requests without an Origin header (non-browser
// clients) are allowed. publicURL, when set, is accepted as an alternative
// trusted origin (deployments behind a reverse proxy with a public hostname).
//
// When the request carries fnOS gateway identity (X-Trim-Userid), any Origin is
// accepted: the unified gateway already authenticated the NAS session before
// forwarding to the Unix socket, and the TCP listener strips those headers so
// they cannot be forged from outside. Matching Host / X-Forwarded-Host alone is
// brittle — desktop embeds often omit the port on one side (e.g. Origin
// host:5666 vs forwarded Host without a port).
func CheckSameOrigin(r *http.Request, publicURL string) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	// Gateway-authenticated hop: allow sandboxed (Origin: null) and any browser
	// Origin the desktop / iframe may send.
	if strings.TrimSpace(r.Header.Get("X-Trim-Userid")) != "" {
		return true
	}
	if strings.EqualFold(origin, "null") {
		return false
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
	oh, _ := splitHostPortLoose(originHost)
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if strings.EqualFold(originHost, c) {
			return true
		}
		ch, _ := splitHostPortLoose(c)
		// Compare hostname only. NAS UIs often expose a mapped port (e.g. :5666)
		// while the app or proxy rewrites Host to an internal port (:9527).
		if strings.EqualFold(oh, ch) {
			return true
		}
	}
	return false
}

func splitHostPortLoose(hostport string) (host, port string) {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return "", ""
	}
	h, p, err := net.SplitHostPort(hostport)
	if err != nil {
		// Bare hostname / IPv4 without port.
		return hostport, ""
	}
	return h, p
}
