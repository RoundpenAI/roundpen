// Package preview issues short-lived tokens and reverse-proxies sandbox ports.
package preview

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
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

// ClaimRegistry is the booked-name table this handler works with: the vhost
// router resolves labels through it, the claim API creates, lists and releases
// them. *ClaimStore is the database-backed implementation.
type ClaimRegistry interface {
	Resolve(ctx context.Context, name string) (sandboxID string, port int, ok bool)
	Get(ctx context.Context, name string) (*Claim, error)
	List(ctx context.Context, owner string) ([]Claim, error)
	Claim(ctx context.Context, c Claim) error
	Release(ctx context.Context, name, owner string) (bool, error)
}

// Handler serves preview-link minting and reverse-proxies sandbox ports, by
// path (/p/{id}/{port}/) and — when Domain is set — by host.
type Handler struct {
	Manager   sandbox.Manager
	Tokens    *Store
	PublicURL string // e.g. http://127.0.0.1:9527 — used to build absolute preview URLs
	Domain    string // preview zone, e.g. rp.mk: {id}-{port}.rp.mk serves that port; "" = path previews only
	Scheme    string // scheme for zone links; "" = follow the console request
	Claims    ClaimRegistry
}

// Mount registers preview routes.
func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/sandboxes/{id}/preview-link", h.previewLink)
	mux.HandleFunc("POST /v1/preview-domains", h.claimDomain)
	mux.HandleFunc("GET /v1/preview-domains", h.listDomains)
	mux.HandleFunc("DELETE /v1/preview-domains/{name}", h.releaseDomain)
	mux.HandleFunc("/p/{id}/{port}/", h.proxy)
	mux.HandleFunc("/p/{id}/{port}", h.proxy)
}

// VhostRouter serves {id}-{port}.{Domain} requests, and must sit outside the
// auth middleware and the console mux: the console session cookie is
// host-only, so nothing on those hosts is a console request, and their paths
// belong to the previewed app (no /p/ prefix to strip — its absolute URLs
// work as written). Returns next unchanged when no preview zone is set.
func (h *Handler) VhostRouter(next http.Handler) http.Handler {
	if h.Domain == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		label, inZone := h.zoneLabel(r.Host)
		if !inZone {
			next.ServeHTTP(w, r)
			return
		}
		// A name inside the zone that resolves to nothing is nobody's preview,
		// and never the console: the zone serves previews only.
		id, port, ok := h.resolveTarget(r.Context(), label)
		if !ok {
			http.NotFound(w, r)
			return
		}
		path := r.URL.Path
		if path == "" {
			path = "/"
		}
		h.serve(w, r, proxyTarget{
			id:         id,
			port:       port,
			path:       path,
			query:      r.URL.RawQuery,
			host:       r.Host,
			cookiePath: "/",
		})
	})
}

// zoneLabel returns the single label a host carries under the preview zone,
// and whether the host is below that zone at all. The zone host itself is
// deliberately excluded: an apex that resolves to this daemon still belongs to
// the console. A deeper label comes back with the dot in it, which no target
// matches — the wildcard certificate covers one level only.
func (h *Handler) zoneLabel(hostport string) (label string, inZone bool) {
	host := bareHost(hostport)
	if !strings.HasSuffix(host, "."+h.Domain) {
		return "", false
	}
	return strings.TrimSuffix(host, "."+h.Domain), true
}

// resolveTarget maps a zone label to the sandbox port it serves: a booked name
// first (users ask for those explicitly), then the automatic {id}-{port} form.
func (h *Handler) resolveTarget(ctx context.Context, label string) (string, int, bool) {
	if label == "" || strings.ContainsRune(label, '.') {
		return "", 0, false
	}
	if h.Claims != nil {
		if id, port, ok := h.Claims.Resolve(ctx, label); ok {
			return id, port, true
		}
	}
	return autoTarget(label)
}

// autoTarget decodes the automatic {id}-{port} label. The port is whatever
// follows the last hyphen, so ids that contain hyphens (UUIDs, slugs ending in
// digits) survive intact.
func autoTarget(label string) (string, int, bool) {
	i := strings.LastIndex(label, "-")
	if i <= 0 || i == len(label)-1 {
		return "", 0, false
	}
	port, err := strconv.Atoi(label[i+1:])
	if err != nil || port <= 0 || port > 65535 {
		return "", 0, false
	}
	return label[:i], port, true
}

// zoneURL is the subdomain origin for sandboxID:port, or "" when no zone is
// configured or the id cannot be a DNS label (a custom id with uppercase
// letters, say) — that preview stays on the path form.
func (h *Handler) zoneURL(sandboxID string, port int, r *http.Request) string {
	label, ok := vhostLabel(sandboxID, port)
	if h.Domain == "" || !ok {
		return ""
	}
	return h.claimOrigin(label, r)
}

// vhostLabel renders the leftmost DNS label for a sandbox port. DNS caps a
// label at 63 bytes, and a wildcard certificate covers one label only, so a
// dotted id would be unreachable and is refused here.
func vhostLabel(sandboxID string, port int) (string, bool) {
	if sandboxID == "" || len(sandboxID) > 63-1-len(strconv.Itoa(port)) {
		return "", false
	}
	if !validLabel(sandboxID) {
		return "", false
	}
	return fmt.Sprintf("%s-%d", sandboxID, port), true
}

func bareHost(hostport string) string {
	host := strings.ToLower(hostport)
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
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
			httpx.WriteErr(w, http.StatusBadRequest, "invalid port")
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
		httpx.WriteErr(w, http.StatusNotFound, "sandbox not found")
		return
	}

	token, exp, err := h.Tokens.Issue(id, port, sb.Owner)
	if err != nil {
		httpx.WriteErr(w, http.StatusInternalServerError, "token issue failed")
		return
	}

	// A preview subdomain serves the app at its root — that root is also the
	// scope of its token cookie — while a path preview keeps the app (and the
	// cookie) under /p/{id}/{port} on the console origin.
	base := h.zoneURL(id, port, r)
	cookiePath := "/"
	u := base + pathSuffix
	if base == "" {
		if base = strings.TrimRight(h.PublicURL, "/"); base == "" {
			base = httpx.DefaultTrust.Scheme(r) + "://" + httpx.DefaultTrust.Host(r)
		}
		cookiePath = fmt.Sprintf("/p/%s/%d", id, port)
		u = base + cookiePath + pathSuffix
	}

	if sameOriginPreview(base, r) {
		// Default same-origin preview: deliver the token as a path-scoped
		// HttpOnly cookie so it never appears in the URL — no leakage via
		// browser history, access logs, or Referer — and sub-resource
		// requests of the previewed app carry it automatically. The proxy
		// refreshes the cookie on each request.
		http.SetCookie(w, &http.Cookie{
			Name:     "roundpen_preview",
			Value:    token,
			Path:     cookiePath,
			HttpOnly: true,
			Secure:   httpx.DefaultTrust.Scheme(r) == "https",
			SameSite: http.SameSiteLaxMode,
			MaxAge:   int(h.Tokens.ttl.Seconds()),
		})
	} else {
		// Separate preview origin: a cookie set on the console domain is
		// never sent to the preview domain, so the first navigation must
		// carry ?token=; the proxy then sets its own cookie on the preview
		// origin for subsequent requests.
		if strings.Contains(u, "?") {
			u += "&token=" + token
		} else {
			u += "?token=" + token
		}
	}

	httpx.WriteJSON(w, http.StatusOK, previewLinkResp{
		URL: u, Port: port, Token: token, ExpiresAt: exp,
	})
}

// sameOriginPreview reports whether preview links resolve to the same host as
// the console request, i.e. whether a cookie set here will reach the iframe.
func sameOriginPreview(base string, r *http.Request) bool {
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return true
	}
	return strings.EqualFold(u.Host, r.Host)
}

// proxyTarget is where one preview request goes: the sandbox port, the
// app-relative path and query to forward, the Host the app should see, and the
// cookie scope. A path preview keeps 127.0.0.1 as its upstream Host (it is one
// app among many on the console origin, so Host tells the app nothing useful),
// while a subdomain preview keeps the public host so the app builds absolute
// URLs and HMR sockets that point back at itself.
type proxyTarget struct {
	id         string
	port       int
	path       string
	query      string
	host       string
	cookiePath string
}

// proxy serves the path form: /p/{id}/{port}/ plus the app's own path.
func (h *Handler) proxy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	port, err := strconv.Atoi(r.PathValue("port"))
	if err != nil || port <= 0 || port > 65535 {
		http.Error(w, "invalid port", http.StatusBadRequest)
		return
	}
	prefix := fmt.Sprintf("/p/%s/%d", id, port)
	targetPath := strings.TrimPrefix(r.URL.Path, prefix)
	if targetPath == "" {
		targetPath = "/"
	}
	h.serve(w, r, proxyTarget{
		id:         id,
		port:       port,
		path:       targetPath,
		query:      r.URL.RawQuery,
		host:       "127.0.0.1",
		cookiePath: prefix,
	})
}

// serve authorizes one preview request and forwards it to the sandbox port.
func (h *Handler) serve(w http.ResponseWriter, r *http.Request, target proxyTarget) {
	ctx := r.Context()
	id, port := target.id, target.port
	token, owner, ok := h.authenticate(r, id, port)
	switch {
	case ok:
		if owner != "" {
			ctx = authz.WithActor(ctx, authz.Actor{Username: owner})
		}
		// When the console embeds the preview from a separate domain the
		// request is cross-site: Lax cookies are not sent inside cross-site
		// iframes, so fall back to SameSite=None (browsers require Secure).
		sameSite := http.SameSiteLaxMode
		if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
			sameSite = http.SameSiteNoneMode
		}
		http.SetCookie(w, &http.Cookie{
			Name:     "roundpen_preview",
			Value:    token,
			Path:     target.cookiePath,
			HttpOnly: true,
			Secure:   sameSite == http.SameSiteNoneMode || httpx.DefaultTrust.Scheme(r) == "https",
			SameSite: sameSite,
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

	// Only the token this request authenticated with is dropped from the query
	// — a `token` parameter the previewed app uses itself must reach it.
	if q := r.URL.Query().Get("token"); q != "" && q == token {
		target.query = stripToken(target.query, token)
	}

	conn, err := h.Manager.Dial(ctx, id, port)
	if err != nil {
		slog.Warn("preview dial failed", "sandbox", id, "port", port, "err", err)
		http.Error(w, "dial failed", http.StatusBadGateway)
		return
	}

	_ = h.Manager.Touch(ctx, id)

	if isWebSocket(r) {
		h.proxyWebSocket(w, r, conn, target)
		return
	}

	h.proxyHTTP(w, r, conn, target)
}

// authenticate resolves the preview token: the ?token= of a preview link
// first, then the cookie the proxy planted for later requests. A parameter
// that validates as neither is skipped rather than treated as a failed
// attempt, so a previewed app's own ?token= never locks its users out.
func (h *Handler) authenticate(r *http.Request, id string, port int) (token, owner string, ok bool) {
	candidates := []string{r.URL.Query().Get("token")}
	if c, err := r.Cookie("roundpen_preview"); err == nil {
		candidates = append(candidates, c.Value)
	}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		if sid, tokPort, tokOwner, tokOK := h.Tokens.Lookup(candidate); tokOK && sid == id && tokPort == port {
			return candidate, tokOwner, true
		}
	}
	return "", "", false
}

func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func (h *Handler) proxyHTTP(w http.ResponseWriter, r *http.Request, conn net.Conn, target proxyTarget) {
	defer conn.Close()

	director := func(req *http.Request) {
		req.URL = &url.URL{
			Scheme:   "http",
			Host:     target.host,
			Path:     target.path,
			RawQuery: target.query,
		}
		req.Host = target.host
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
			slog.Warn("preview proxy error", "path", r.URL.Path, "err", err)
			http.Error(rw, "preview proxy error", http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}

func (h *Handler) proxyWebSocket(w http.ResponseWriter, r *http.Request, conn net.Conn, target proxyTarget) {
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
	req.URL.Host = target.host
	req.URL.Path = target.path
	req.URL.RawQuery = target.query
	req.RequestURI = ""
	req.Header.Set("Host", target.host)
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

// stripToken removes the token this proxy consumed, leaving any same-named
// parameter the previewed app owns untouched.
func stripToken(raw, token string) string {
	if raw == "" || token == "" {
		return raw
	}
	vals, err := url.ParseQuery(raw)
	if err != nil {
		return raw
	}
	if vals.Get("token") != token {
		return raw
	}
	vals.Del("token")
	return vals.Encode()
}
