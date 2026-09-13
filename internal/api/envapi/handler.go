// Package envapi exposes fixed per-user environments (agent / browser / mobile).
package envapi

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/runtime"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

// SandboxDialer opens a TCP connection to a port inside a sandbox.
type SandboxDialer interface {
	Dial(ctx context.Context, sandboxID string, destPort int) (net.Conn, error)
}

// Environments is the userenv surface used by environment HTTP handlers.
type Environments interface {
	List(ctx context.Context, userID string) ([]userenv.EnvView, error)
	EnsureBrowser(ctx context.Context, userID string) (*userenv.BrowserTarget, error)
	EnsureAgent(ctx context.Context, userID string) (*sandbox.Sandbox, error)
}

// Handler serves /v1/me/environments*.
type Handler struct {
	Envs Environments
	Cfg  *config.Config
	Dial SandboxDialer
}

// Mount registers environment routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/me/environments", h.list)
	mux.HandleFunc("POST /v1/me/environments/browser/ensure", h.ensureBrowser)
	mux.HandleFunc("POST /v1/me/environments/agent/ensure", h.ensureAgent)
	mux.HandleFunc("GET /v1/me/environments/browser/live-link", h.liveLink)
	mux.HandleFunc("GET /v1/me/environments/browser/live", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/v1/me/environments/browser/live/", http.StatusTemporaryRedirect)
	})
	mux.HandleFunc("/v1/me/environments/browser/live/", h.live)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Envs == nil {
		writeErr(w, http.StatusServiceUnavailable, "environments not configured")
		return
	}
	list, err := h.Envs.List(r.Context(), user.Username)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"environments": list})
}

func (h *Handler) ensureBrowser(w http.ResponseWriter, r *http.Request) {
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
	resp := map[string]any{
		"slot":     userenv.SlotBrowser,
		"provider": target.Provider,
		"managed":  target.Managed,
	}
	if target.Sandbox != nil {
		resp["sandboxId"] = target.Sandbox.ID
		resp["status"] = string(target.Sandbox.Status)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) ensureAgent(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Envs == nil {
		writeErr(w, http.StatusServiceUnavailable, "environments not configured")
		return
	}
	sb, err := h.Envs.EnsureAgent(r.Context(), user.Username)
	if err != nil {
		if runtime.WriteNotReady(w, err) {
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"slot":      userenv.SlotAgent,
		"sandboxId": sb.ID,
		"status":    sb.Status,
		"name":      sb.Name,
	})
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
		writeJSON(w, http.StatusOK, map[string]any{"mode": "managed", "url": "/v1/me/environments/browser/live/"})
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
	target, err := h.Envs.EnsureBrowser(r.Context(), user.Username)
	if err != nil || target == nil || !target.Managed || target.Sandbox == nil {
		http.Error(w, "live view requires the managed browser container", http.StatusConflict)
		return
	}
	sandboxID := target.Sandbox.ID
	token := ""
	if target.Sandbox.Metadata != nil {
		token = target.Sandbox.Metadata["browserToken"]
	}
	conn, err := h.Dial.Dial(r.Context(), sandboxID, config.DefaultCDPPort)
	if err != nil {
		http.Error(w, "browser dial: "+err.Error(), http.StatusBadGateway)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/v1/me/environments/browser/live")
	if rest == "" {
		rest = "/"
	}
	upstreamPath := "/debugger" + rest

	if isWebSocket(r) {
		h.proxyLiveWS(w, r, conn, upstreamPath, token)
		return
	}
	defer conn.Close()
	q := r.URL.Query()
	if token != "" {
		q.Set("token", token)
	}
	director := func(req *http.Request) {
		req.URL = &url.URL{Scheme: "http", Host: "127.0.0.1", Path: upstreamPath, RawQuery: q.Encode()}
		req.Host = "127.0.0.1"
		req.RequestURI = ""
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
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		_ = client.Close()
		return
	}
	_ = bufrw.Flush()

	errCh := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(conn, client)
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
