package httpx

import (
	"strings"
	"sync/atomic"
)

// uiBase is the browser path prefix the SPA is built with (e.g. "/app/roundpen"
// on fnOS). Empty means the console is served at the host root.
var uiBase atomic.Value // string

// SetUIBase records the SPA mount prefix for ConsolePath. Call once at startup
// when the fnOS gateway (or an equivalent prefixed console) is active.
func SetUIBase(prefix string) {
	uiBase.Store(strings.TrimRight(strings.TrimSpace(prefix), "/"))
}

// UIBase returns the SPA mount prefix (e.g. "/app/roundpen"), or "" when the
// console is served at the host root.
func UIBase() string {
	base, _ := uiBase.Load().(string)
	return base
}

// ConsolePath prefixes a same-origin console path with the SPA base. Paths that
// already start with the base are returned unchanged. Non-fnOS builds leave the
// base empty so "/settings/apps" stays "/settings/apps".
func ConsolePath(path string) string {
	base := UIBase()
	path = strings.TrimSpace(path)
	if path == "" {
		path = "/"
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	if base == "" || path == base || strings.HasPrefix(path, base+"/") || strings.HasPrefix(path, base+"?") {
		return path
	}
	return base + path
}

// ConsoleReturnBase is the browser-facing base the OAuth relay redirects to:
// scheme://host[/uiBase]. On fnOS this must include /app/roundpen so the
// unified gateway routes /v1/... callbacks to Roundpen instead of another app.
func ConsoleReturnBase(scheme, host string) string {
	scheme = strings.TrimSpace(scheme)
	host = strings.TrimSpace(host)
	if scheme == "" || host == "" {
		return ""
	}
	base := scheme + "://" + host
	if ui := UIBase(); ui != "" {
		return base + ui
	}
	return base
}
