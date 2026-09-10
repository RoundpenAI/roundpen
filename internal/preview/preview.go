// Package preview issues short-lived tokens and reverse-proxies sandbox ports.
package preview

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/authz"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

const defaultTokenTTL = 15 * time.Minute

// Store holds short-lived preview tokens.
type Store struct {
	mu     sync.Mutex
	tokens map[string]entry
	ttl    time.Duration
}

type entry struct {
	SandboxID string
	Port      int
	Owner     string
	Expires   time.Time
}

// NewStore returns an in-memory token store.
func NewStore(ttl time.Duration) *Store {
	if ttl <= 0 {
		ttl = defaultTokenTTL
	}
	return &Store{tokens: make(map[string]entry), ttl: ttl}
}

// SetTTL updates the token lifetime for newly issued tokens.
func (s *Store) SetTTL(ttl time.Duration) {
	if ttl <= 0 {
		ttl = defaultTokenTTL
	}
	s.mu.Lock()
	s.ttl = ttl
	s.mu.Unlock()
}

// Issue creates a token for sandboxID:port.
func (s *Store) Issue(sandboxID string, port int, owner string) (token string, expires time.Time, err error) {
	var b [16]byte
	if _, err = rand.Read(b[:]); err != nil {
		return "", time.Time{}, err
	}
	token = hex.EncodeToString(b[:])
	expires = time.Now().UTC().Add(s.ttl)
	s.mu.Lock()
	s.gcLocked()
	s.tokens[token] = entry{SandboxID: sandboxID, Port: port, Owner: owner, Expires: expires}
	s.mu.Unlock()
	return token, expires, nil
}

// Lookup validates a token and returns sandbox id, port, and owner.
func (s *Store) Lookup(token string) (sandboxID string, port int, owner string, ok bool) {
	if token == "" {
		return "", 0, "", false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gcLocked()
	e, found := s.tokens[token]
	if !found || time.Now().UTC().After(e.Expires) {
		delete(s.tokens, token)
		return "", 0, "", false
	}
	return e.SandboxID, e.Port, e.Owner, true
}

func (s *Store) gcLocked() {
	now := time.Now().UTC()
	for k, v := range s.tokens {
		if now.After(v.Expires) {
			delete(s.tokens, k)
		}
	}
}

// Handler serves preview-link minting and /p/{id}/{port}/ reverse proxy.
type Handler struct {
	Manager   sandbox.Manager
	Tokens    *Store
	PublicURL string // e.g. http://127.0.0.1:9527 — used to build absolute preview URLs
}

// Mount registers preview routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/sandboxes/{id}/preview-link", h.previewLink)
	mux.HandleFunc("/p/{id}/{port}/", h.proxy)
	mux.HandleFunc("/p/{id}/{port}", h.proxy)
}

type previewLinkResp struct {
	URL       string    `json:"url"`
	Port      int       `json:"port"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (h *Handler) previewLink(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	port := 3000
	if v := r.URL.Query().Get("port"); v != "" {
		p, err := strconv.Atoi(v)
		if err != nil || p <= 0 || p > 65535 {
			writeErr(w, http.StatusBadRequest, "invalid port")
			return
		}
		port = p
	}
	pathSuffix := r.URL.Query().Get("path")
	if pathSuffix == "" {
		pathSuffix = "/"
	}
	if !strings.HasPrefix(pathSuffix, "/") {
		pathSuffix = "/" + pathSuffix
	}

	sb, err := h.Manager.Get(r.Context(), id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "sandbox not found")
		return
	}

	token, exp, err := h.Tokens.Issue(id, port, sb.Owner)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "token issue failed")
		return
	}

	base := strings.TrimRight(h.PublicURL, "/")
	if base == "" {
		base = httpx.DefaultTrust.Scheme(r) + "://" + httpx.DefaultTrust.Host(r)
	}
	u := fmt.Sprintf("%s/p/%s/%d%s", base, id, port, pathSuffix)
	if strings.Contains(u, "?") {
		u += "&token=" + token
	} else {
		u += "?token=" + token
	}

	writeJSON(w, http.StatusOK, previewLinkResp{
		URL: u, Port: port, Token: token, ExpiresAt: exp,
	})
}

func (h *Handler) proxy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	portStr := r.PathValue("port")
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		http.Error(w, "invalid port", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	token := r.URL.Query().Get("token")
	if token == "" {
		if c, err := r.Cookie("roundpen_preview"); err == nil {
			token = c.Value
		}
	}
	sid, tokPort, owner, tokOK := h.Tokens.Lookup(token)
	switch {
	case tokOK && sid == id && tokPort == port:
		if owner != "" {
			ctx = authz.WithActor(ctx, authz.Actor{Username: owner})
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "roundpen_preview",
			Value:    token,
			Path:     fmt.Sprintf("/p/%s/%d", id, port),
			HttpOnly: true,
			Secure:   httpx.DefaultTrust.Scheme(r) == "https",
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int(h.Tokens.ttl.Seconds()),
		})
	case auth.GetUser(r.Context()) != nil:
		if _, err := h.Manager.Get(r.Context(), id); err != nil {
			http.Error(w, "unauthorized preview", http.StatusUnauthorized)
			return
		}
	default:
		http.Error(w, "unauthorized preview", http.StatusUnauthorized)
		return
	}

	prefix := fmt.Sprintf("/p/%s/%d", id, port)
	targetPath := strings.TrimPrefix(r.URL.Path, prefix)
	if targetPath == "" {
		targetPath = "/"
	}

	conn, err := h.Manager.Dial(ctx, id, port)
	if err != nil {
		http.Error(w, "dial failed", http.StatusBadGateway)
		return
	}

	_ = h.Manager.Touch(ctx, id)

	if isWebSocket(r) {
		h.proxyWebSocket(w, r, conn, targetPath)
		return
	}

	h.proxyHTTP(w, r, conn, targetPath)
}

func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func (h *Handler) proxyHTTP(w http.ResponseWriter, r *http.Request, conn net.Conn, targetPath string) {
	defer conn.Close()

	director := func(req *http.Request) {
		req.URL = &url.URL{
			Scheme:   "http",
			Host:     "127.0.0.1",
			Path:     targetPath,
			RawQuery: stripToken(r.URL.RawQuery),
		}
		req.Host = fmt.Sprintf("127.0.0.1")
		req.RequestURI = ""
	}
	proxy := &httputil.ReverseProxy{
		Director: director,
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return conn, nil
			},
			DisableKeepAlives: true,
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			http.Error(rw, "preview proxy error: "+err.Error(), http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}

func (h *Handler) proxyWebSocket(w http.ResponseWriter, r *http.Request, conn net.Conn, targetPath string) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		conn.Close()
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}
	client, bufrw, err := hj.Hijack()
	if err != nil {
		conn.Close()
		return
	}

	req := r.Clone(r.Context())
	req.URL.Scheme = "http"
	req.URL.Host = "127.0.0.1"
	req.URL.Path = targetPath
	req.URL.RawQuery = stripToken(r.URL.RawQuery)
	req.RequestURI = ""
	req.Header.Set("Host", "127.0.0.1")
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

func stripToken(raw string) string {
	if raw == "" {
		return ""
	}
	vals, err := url.ParseQuery(raw)
	if err != nil {
		return raw
	}
	vals.Del("token")
	return vals.Encode()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"message": msg})
}
