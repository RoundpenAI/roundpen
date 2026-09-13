package envapi

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/config"
	"github.com/RoundpenAI/roundpen/internal/runtime"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

type fakeEnvs struct {
	target *userenv.BrowserTarget
	err    error
	calls  int
}

func (f *fakeEnvs) List(context.Context, string) ([]userenv.EnvView, error) { return nil, nil }

func (f *fakeEnvs) EnsureBrowser(context.Context, string) (*userenv.BrowserTarget, error) {
	f.calls++
	return f.target, f.err
}

func (f *fakeEnvs) EnsureAgent(context.Context, string) (*sandbox.Sandbox, error) { return nil, nil }

type fakeDialer func(ctx context.Context, sandboxID string, destPort int) (net.Conn, error)

func (f fakeDialer) Dial(ctx context.Context, sandboxID string, destPort int) (net.Conn, error) {
	return f(ctx, sandboxID, destPort)
}

// liveUpstream is a stand-in browserless debugger that records what the proxy
// sends it.
type liveUpstream struct {
	*httptest.Server
	paths   []string
	query   url.Values
	headers http.Header
}

func newLiveUpstream() *liveUpstream {
	u := &liveUpstream{}
	u.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.paths = append(u.paths, r.URL.Path)
		if u.query == nil {
			u.query = r.URL.Query()
		}
		if u.headers == nil {
			u.headers = r.Header.Clone()
		}
		_, _ = io.WriteString(w, "ok")
	}))
	return u
}

// dialer connects the proxy to this stub instead of a sandbox.
func (u *liveUpstream) dialer() fakeDialer {
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(u.URL, "http://"))
	return fakeDialer(func(context.Context, string, int) (net.Conn, error) {
		return net.Dial("tcp", net.JoinHostPort(host, port))
	})
}

func (u *liveUpstream) sawPath(p string) bool {
	for _, got := range u.paths {
		if got == p {
			return true
		}
	}
	return false
}

// managedTarget is the happy-path managed browser container.
func managedTarget() *userenv.BrowserTarget {
	return &userenv.BrowserTarget{
		Key: "sb-browser", Provider: "docker", Managed: true,
		Sandbox: &sandbox.Sandbox{
			ID:       "sb-browser",
			Metadata: map[string]string{"browserToken": "browserless-token"},
		},
	}
}

func liveRequest(t *testing.T, username, target string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if username != "" {
		req = req.WithContext(authCtx(req.Context(), username))
	}
	return req
}

func TestLiveProxyRewritesPathAndInjectsToken(t *testing.T) {
	up := newLiveUpstream()
	defer up.Close()
	h := &Handler{Envs: &fakeEnvs{target: managedTarget()}, Dial: up.dialer()}

	rec := httptest.NewRecorder()
	h.live(rec, liveRequest(t, "alice", "/v1/me/environments/browser/live/app.bundle.js?token=attacker&x=1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if !up.sawPath("/debugger/app.bundle.js") {
		t.Fatalf("upstream paths = %v, want /debugger/app.bundle.js", up.paths)
	}
	if got := up.query["token"]; len(got) != 1 || got[0] != "browserless-token" {
		t.Fatalf("upstream token = %v, want exactly the container token", got)
	}
	if strings.Contains(up.query.Encode(), "attacker") {
		t.Fatalf("client token leaked upstream: %q", up.query.Encode())
	}
	if up.query.Get("x") != "1" {
		t.Fatalf("upstream query = %q, want preserved x=1", up.query.Encode())
	}
}

func TestLiveProxyStripsClientCredentials(t *testing.T) {
	up := newLiveUpstream()
	defer up.Close()
	h := &Handler{Envs: &fakeEnvs{target: managedTarget()}, Dial: up.dialer()}

	req := liveRequest(t, "alice", "/v1/me/environments/browser/live/app.bundle.js")
	req.Header.Set("Cookie", "roundpen_session=console-secret")
	req.Header.Set("Authorization", "Bearer rp-api-key")
	rec := httptest.NewRecorder()
	h.live(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if got := up.headers.Get("Cookie"); got != "" {
		t.Fatalf("Cookie leaked upstream: %q", got)
	}
	if got := up.headers.Get("Authorization"); got != "" {
		t.Fatalf("Authorization leaked upstream: %q", got)
	}
}

func TestLiveUnauthorized(t *testing.T) {
	h := &Handler{Envs: &fakeEnvs{target: managedTarget()}, Dial: fakeDialer(func(context.Context, string, int) (net.Conn, error) {
		t.Fatal("dial must not run for an unauthenticated request")
		return nil, nil
	})}
	rec := httptest.NewRecorder()
	h.live(rec, liveRequest(t, "", "/v1/me/environments/browser/live/"))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestLiveNonManagedIsConflict(t *testing.T) {
	cases := map[string]*userenv.BrowserTarget{
		"host target":         {Key: "host", Provider: "host"},
		"managed without box": {Key: "x", Provider: "docker", Managed: true},
	}
	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			h := &Handler{Envs: &fakeEnvs{target: target}, Dial: fakeDialer(func(context.Context, string, int) (net.Conn, error) {
				t.Fatal("dial must not run without a managed container")
				return nil, nil
			})}
			rec := httptest.NewRecorder()
			h.live(rec, liveRequest(t, "alice", "/v1/me/environments/browser/live/"))
			if rec.Code != http.StatusConflict {
				t.Fatalf("status = %d body=%s, want 409", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestLiveEnsureErrors(t *testing.T) {
	t.Run("not ready", func(t *testing.T) {
		h := &Handler{
			Envs: &fakeEnvs{err: &runtime.NotReady{Engine: "docker", Setup: []runtime.SetupStep{{Title: "install docker"}}}},
			Dial: fakeDialer(func(context.Context, string, int) (net.Conn, error) { return nil, nil }),
		}
		rec := httptest.NewRecorder()
		h.live(rec, liveRequest(t, "alice", "/v1/me/environments/browser/live/"))
		if rec.Code != http.StatusConflict {
			t.Fatalf("status = %d body=%s, want 409", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if body["code"] != "engine_not_ready" {
			t.Fatalf("code = %v, want engine_not_ready", body["code"])
		}
	})
	t.Run("backend error", func(t *testing.T) {
		h := &Handler{
			Envs: &fakeEnvs{err: errors.New("sandbox manager down")},
			Dial: fakeDialer(func(context.Context, string, int) (net.Conn, error) { return nil, nil }),
		}
		rec := httptest.NewRecorder()
		h.live(rec, liveRequest(t, "alice", "/v1/me/environments/browser/live/"))
		if rec.Code != http.StatusBadGateway {
			t.Fatalf("status = %d body=%s, want 502", rec.Code, rec.Body.String())
		}
	})
}

func TestLiveUsesConfiguredCDPPort(t *testing.T) {
	cases := []struct {
		name string
		cfg  *config.Config
		want int
	}{
		{"configured", &config.Config{CDP: config.CDPConfig{Port: 9222}}, 9222},
		{"zero falls back", &config.Config{}, config.DefaultCDPPort},
		{"no config", nil, config.DefaultCDPPort},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up := newLiveUpstream()
			defer up.Close()
			gotPort := 0
			h := &Handler{Envs: &fakeEnvs{target: managedTarget()}, Cfg: tc.cfg, Dial: fakeDialer(func(_ context.Context, _ string, port int) (net.Conn, error) {
				gotPort = port
				host, p, _ := net.SplitHostPort(strings.TrimPrefix(up.URL, "http://"))
				return net.Dial("tcp", net.JoinHostPort(host, p))
			})}
			rec := httptest.NewRecorder()
			h.live(rec, liveRequest(t, "alice", "/v1/me/environments/browser/live/app.bundle.js"))
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
			}
			if gotPort != tc.want {
				t.Fatalf("dial port = %d, want %d", gotPort, tc.want)
			}
		})
	}
}

func TestLiveMuxRoutes(t *testing.T) {
	up := newLiveUpstream()
	defer up.Close()
	envs := &fakeEnvs{target: managedTarget()}
	h := &Handler{Envs: envs, Dial: up.dialer()}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r.WithContext(authCtx(r.Context(), "alice")))
	}))
	defer srv.Close()
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	follow := srv.Client()

	t.Run("plain live redirects to live-slash", func(t *testing.T) {
		resp, err := noFollow.Get(srv.URL + "/v1/me/environments/browser/live?token=x")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusTemporaryRedirect {
			t.Fatalf("status = %d, want 307", resp.StatusCode)
		}
		if got := resp.Header.Get("Location"); got != "/v1/me/environments/browser/live/?token=x" {
			t.Fatalf("location = %q, want the slash form with the query preserved", got)
		}
	})

	t.Run("encoded traversal is rejected", func(t *testing.T) {
		resp, err := noFollow.Get(srv.URL + "/v1/me/environments/browser/live/%2e%2e%2fjson%2fversion")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d body=%s, want 403", resp.StatusCode, resp.Body)
		}
		for _, p := range up.paths {
			if strings.HasPrefix(p, "/json") || strings.Contains(p, "/json/") {
				t.Fatalf("traversal reached upstream: %v", up.paths)
			}
		}
	})

	t.Run("double slash asset resolves", func(t *testing.T) {
		resp, err := follow.Get(srv.URL + "/v1/me/environments/browser/live//app.bundle.js")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d body=%s, want 200", resp.StatusCode, resp.Body)
		}
		if !up.sawPath("/debugger/app.bundle.js") {
			t.Fatalf("upstream paths = %v, want /debugger/app.bundle.js", up.paths)
		}
	})
}

// TestLiveWebSocketForwardsBufferedBytes sends the upgrade request and a
// payload in one write; the payload lands in the server's buffered reader past
// the request line and must still reach the upstream.
func TestLiveWebSocketForwardsBufferedBytes(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	got := make(chan string, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		br := bufio.NewReader(c)
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				got <- "upstream read: " + err.Error()
				return
			}
			if line == "\r\n" {
				break
			}
		}
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 4)
		if _, err := io.ReadFull(br, buf); err != nil {
			got <- "upstream payload: " + err.Error()
			return
		}
		got <- string(buf)
	}()

	h := &Handler{
		Envs: &fakeEnvs{target: managedTarget()},
		Dial: fakeDialer(func(context.Context, string, int) (net.Conn, error) {
			return net.Dial("tcp", ln.Addr().String())
		}),
	}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mux.ServeHTTP(w, r.WithContext(authCtx(r.Context(), "alice")))
	}))
	defer srv.Close()

	client, err := net.Dial("tcp", strings.TrimPrefix(srv.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	upgrade := "GET /v1/me/environments/browser/live/ws HTTP/1.1\r\n" +
		"Host: control-plane\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n"
	if _, err := client.Write([]byte(upgrade + "PING")); err != nil {
		t.Fatal(err)
	}
	select {
	case gotBytes := <-got:
		if gotBytes != "PING" {
			t.Fatalf("upstream got %q, want the bytes buffered past the request line", gotBytes)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("upstream never received the bytes buffered past the request line")
	}
}

func TestLiveCacheReusesEnsure(t *testing.T) {
	up := newLiveUpstream()
	defer up.Close()
	envs := &fakeEnvs{target: managedTarget()}
	h := &Handler{Envs: envs, Dial: up.dialer()}

	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		h.live(rec, liveRequest(t, "alice", "/v1/me/environments/browser/live/app.bundle.js"))
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: status = %d body=%s", i, rec.Code, rec.Body.String())
		}
	}
	if envs.calls != 1 {
		t.Fatalf("EnsureBrowser calls = %d, want 1 (second request must hit the cache)", envs.calls)
	}
	if len(up.paths) != 2 {
		t.Fatalf("upstream requests = %d, want 2", len(up.paths))
	}
}

func TestLiveDialFailureInvalidatesCache(t *testing.T) {
	up := newLiveUpstream()
	defer up.Close()
	envs := &fakeEnvs{target: managedTarget()}
	fail := false
	h := &Handler{Envs: envs, Dial: fakeDialer(func(ctx context.Context, sandboxID string, port int) (net.Conn, error) {
		if fail {
			return nil, errors.New("connection refused")
		}
		return up.dialer()(ctx, sandboxID, port)
	})}

	rec := httptest.NewRecorder()
	h.live(rec, liveRequest(t, "alice", "/v1/me/environments/browser/live/app.bundle.js"))
	if rec.Code != http.StatusOK {
		t.Fatalf("first call: status = %d body=%s", rec.Code, rec.Body.String())
	}

	fail = true
	rec = httptest.NewRecorder()
	h.live(rec, liveRequest(t, "alice", "/v1/me/environments/browser/live/app.bundle.js"))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("failing call: status = %d body=%s, want 502", rec.Code, rec.Body.String())
	}
	if envs.calls != 1 {
		t.Fatalf("EnsureBrowser calls = %d after a cache hit, want 1", envs.calls)
	}

	fail = false
	rec = httptest.NewRecorder()
	h.live(rec, liveRequest(t, "alice", "/v1/me/environments/browser/live/app.bundle.js"))
	if rec.Code != http.StatusOK {
		t.Fatalf("recovery call: status = %d body=%s", rec.Code, rec.Body.String())
	}
	if envs.calls != 2 {
		t.Fatalf("EnsureBrowser calls = %d, want 2 (dial failure must invalidate the cache)", envs.calls)
	}
}

func TestLiveLinkManaged(t *testing.T) {
	h := &Handler{Envs: &fakeEnvs{target: &userenv.BrowserTarget{
		Key: "sb-browser", Provider: "docker", Managed: true,
		Sandbox: &sandbox.Sandbox{ID: "sb-browser"},
	}}}
	req := httptest.NewRequest(http.MethodGet, "/v1/me/environments/browser/live-link", nil)
	req = req.WithContext(authCtx(req.Context(), "alice"))
	rec := httptest.NewRecorder()
	h.liveLink(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["mode"] != "managed" {
		t.Fatalf("mode = %v, want managed", got["mode"])
	}
	if got["url"] != "/v1/me/environments/browser/live/" {
		t.Fatalf("url = %v, want /v1/me/environments/browser/live/", got["url"])
	}
}

func TestLiveLinkHost(t *testing.T) {
	h := &Handler{Envs: &fakeEnvs{target: &userenv.BrowserTarget{
		Key: "host", Provider: "host", Managed: false,
	}}}
	req := httptest.NewRequest(http.MethodGet, "/v1/me/environments/browser/live-link", nil)
	req = req.WithContext(authCtx(req.Context(), "alice"))
	rec := httptest.NewRecorder()
	h.liveLink(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["url"] != "" {
		t.Fatalf("url = %v, want empty for host Chrome", got["url"])
	}
	if hint, _ := got["hint"].(string); hint == "" {
		t.Fatalf("expected a hint for host Chrome, got %v", got)
	}
}

func TestDebuggerURL(t *testing.T) {
	cases := []struct {
		in, token, want string
	}{
		{"ws://127.0.0.1:3000", "tok", "http://127.0.0.1:3000/debugger/?token=tok"},
		{"wss://cdp.example.com/base", "", "https://cdp.example.com/debugger/"},
		{"", "tok", ""},
		{"not a url", "tok", ""},
	}
	for _, tc := range cases {
		if got := debuggerURL(tc.in, tc.token); got != tc.want {
			t.Errorf("debuggerURL(%q, %q) = %q, want %q", tc.in, tc.token, got, tc.want)
		}
	}
}

// authCtx mirrors auth.Middleware: the authenticated user is stored under the
// package-private user context key via auth.WithUser.
func authCtx(ctx context.Context, username string) context.Context {
	return auth.WithUser(ctx, &storage.User{Username: username, Role: storage.RoleUser})
}
