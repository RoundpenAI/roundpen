package envapi

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/runtime"
)

const (
	// liveRoutePrefix is the control-plane path that fronts the debugger.
	liveRoutePrefix = "/v1/me/environments/browser/live"
	// debuggerPrefix is the browserless debugger root inside the container.
	debuggerPrefix = "/debugger"
	// liveTargetTTL bounds how long a resolved sandbox/token pair is reused
	// before the next asset request re-runs EnsureBrowser.
	liveTargetTTL = 10 * time.Second
)

// liveTarget caches one resolved managed browser container for a user.
type liveTarget struct {
	sandboxID string
	token     string
	expires   time.Time
}

// liveLink reports how the browser slot can be viewed right now: the managed
// browserless debugger proxied through the control plane, a direct remote
// debugger URL, or nothing for host Chrome.
func (h *Handler) liveLink(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Envs == nil {
		writeErr(w, http.StatusServiceUnavailable, "environments not configured")
		return
	}
	target, err := h.Envs.EnsureBrowser(r.Context(), user.Username)
	if err != nil {
		if runtime.WriteNotReady(w, err) {
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	switch target.Provider {
	case config.CDPProviderDocker:
		writeJSON(w, http.StatusOK, map[string]any{"mode": "managed", "url": liveRoutePrefix + "/"})
	case config.CDPProviderRemote, config.CDPProviderCloud:
		endpoint, token := "", ""
		if h.Cfg != nil {
			endpoint, token = h.Cfg.CDP.Endpoint, h.Cfg.CDP.Token
		}
		u := strings.TrimRight(strings.TrimSpace(endpoint), "/")
		if u == "" {
			writeJSON(w, http.StatusOK, map[string]any{"mode": target.Provider, "url": ""})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"mode": target.Provider, "url": debuggerURL(u, token)})
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"mode": "host", "url": "",
			"hint": "host Chrome has no live view; use the screenshot takeover panel",
		})
	}
}

// debuggerURL turns a browserless origin into its debugger page URL.
func debuggerURL(endpoint, token string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	}
	u.Path = "/debugger/"
	u.RawQuery = ""
	if token != "" {
		q := u.Query()
		q.Set("token", token)
		u.RawQuery = q.Encode()
	}
	return u.String()
}

// live reverse-proxies the managed browserless debugger under
// /v1/me/environments/browser/live/ so the console can embed it in an iframe.
// The debugger's assets are relative, so a path-prefix proxy suffices.
func (h *Handler) live(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if h.Envs == nil || h.Dial == nil {
		http.Error(w, "live view not configured", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, liveRoutePrefix)
	if rest == "" {
		rest = "/"
	}
	upstreamPath := path.Clean(debuggerPrefix + rest)
	if strings.HasSuffix(rest, "/") && !strings.HasSuffix(upstreamPath, "/") {
		// browserless redirects the slashless root with an absolute Location,
		// which would push the browser off the proxy prefix; keep the slash.
		upstreamPath += "/"
	}
	if upstreamPath != debuggerPrefix && !strings.HasPrefix(upstreamPath, debuggerPrefix+"/") {
		http.Error(w, "invalid live view path", http.StatusForbidden)
		return
	}

	sandboxID, token, ok := h.cachedLiveTarget(user.Username)
	if !ok {
		target, err := h.Envs.EnsureBrowser(r.Context(), user.Username)
		if err != nil {
			if runtime.WriteNotReady(w, err) {
				return
			}
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		if target == nil || !target.Managed || target.Sandbox == nil {
			http.Error(w, "live view requires the managed browser container", http.StatusConflict)
			return
		}
		sandboxID, token = target.Sandbox.ID, ""
		if target.Sandbox.Metadata != nil {
			token = target.Sandbox.Metadata["browserToken"]
		}
		h.storeLiveTarget(user.Username, sandboxID, token)
	}

	port := config.DefaultCDPPort
	if h.Cfg != nil && h.Cfg.CDP.Port > 0 {
		port = h.Cfg.CDP.Port
	}
	conn, err := h.Dial.Dial(r.Context(), sandboxID, port)
	if err != nil {
		// A stale sandbox id must not stick for the whole TTL.
		h.forgetLiveTarget(user.Username)
		http.Error(w, "browser dial: "+err.Error(), http.StatusBadGateway)
		return
	}

	if isWebSocket(r) {
		h.proxyLiveWS(w, r, conn, upstreamPath, token)
		return
	}
	defer conn.Close()
	q := r.URL.Query()
	q.Del("token")
	if token != "" {
		q.Set("token", token)
	}
	director := func(req *http.Request) {
		req.URL = &url.URL{Scheme: "http", Host: "127.0.0.1", Path: upstreamPath, RawQuery: q.Encode()}
		req.Host = "127.0.0.1"
		req.RequestURI = ""
		// The container authenticates with the injected ?token=; the console
		// session credentials (cookie, bearer key, or X-API-Key) must not leak
		// into the sandbox.
		req.Header.Del("Cookie")
		req.Header.Del("Authorization")
		req.Header.Del("X-API-Key")
	}
	proxy := &httputil.ReverseProxy{
		Director: director,
		Transport: &http.Transport{
			DialContext:       func(ctx context.Context, network, addr string) (net.Conn, error) { return conn, nil },
			DisableKeepAlives: true,
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			http.Error(rw, "live proxy error: "+err.Error(), http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}

// cachedLiveTarget returns a fresh cached target for username, if any.
func (h *Handler) cachedLiveTarget(username string) (sandboxID, token string, ok bool) {
	h.liveMu.Lock()
	defer h.liveMu.Unlock()
	t, ok := h.liveCache[username]
	if !ok || !t.expires.After(time.Now()) {
		return "", "", false
	}
	return t.sandboxID, t.token, true
}

// storeLiveTarget caches a resolved target for liveTargetTTL.
func (h *Handler) storeLiveTarget(username, sandboxID, token string) {
	h.liveMu.Lock()
	defer h.liveMu.Unlock()
	if h.liveCache == nil {
		h.liveCache = make(map[string]liveTarget)
	}
	h.liveCache[username] = liveTarget{
		sandboxID: sandboxID,
		token:     token,
		expires:   time.Now().Add(liveTargetTTL),
	}
}

// forgetLiveTarget drops the cached target so the next request re-resolves.
func (h *Handler) forgetLiveTarget(username string) {
	h.liveMu.Lock()
	defer h.liveMu.Unlock()
	delete(h.liveCache, username)
}

func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func (h *Handler) proxyLiveWS(w http.ResponseWriter, r *http.Request, conn net.Conn, upstreamPath, token string) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		_ = conn.Close()
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}
	client, bufrw, err := hj.Hijack()
	if err != nil {
		_ = conn.Close()
		return
	}
	q := r.URL.Query()
	q.Del("token")
	if token != "" {
		q.Set("token", token)
	}
	req := r.Clone(r.Context())
	req.URL.Scheme = "http"
	req.URL.Host = "127.0.0.1"
	req.URL.Path = upstreamPath
	req.URL.RawQuery = q.Encode()
	req.RequestURI = ""
	// Request.Write ignores the Host header; the field is what goes on the wire.
	req.Host = "127.0.0.1"
	// The container authenticates with the injected ?token=; the console
	// session credentials (cookie, bearer key, or X-API-Key) must not leak
	// into the sandbox.
	req.Header.Del("Cookie")
	req.Header.Del("Authorization")
	req.Header.Del("X-API-Key")
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		_ = client.Close()
		return
	}
	_ = bufrw.Flush()

	errCh := make(chan struct{}, 2)
	go func() {
		// Read through the hijacked reader: bytes buffered past the request
		// line live there, not on the raw conn.
		_, _ = io.Copy(conn, bufrw)
		errCh <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, conn)
		errCh <- struct{}{}
	}()
	<-errCh
	_ = conn.Close()
	_ = client.Close()
}
