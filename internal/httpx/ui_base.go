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

// ConsolePath prefixes a same-origin console path with the SPA base. Paths that
// already start with the base are returned unchanged. Non-fnOS builds leave the
// base empty so "/settings/apps" stays "/settings/apps".
func ConsolePath(path string) string {
	base, _ := uiBase.Load().(string)
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
