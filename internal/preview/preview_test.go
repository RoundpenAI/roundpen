package preview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

type fakeManager struct {
	sandbox.Manager
	sb   *sandbox.Sandbox
	addr string // upstream the preview proxy dials instead of a sandbox
}

func (f fakeManager) Get(_ context.Context, id string) (*sandbox.Sandbox, error) {
	if f.sb != nil && f.sb.ID == id {
		return f.sb, nil
	}
	return nil, errors.New("sandbox not found")
}

func (f fakeManager) Dial(ctx context.Context, _ string, _ int) (net.Conn, error) {
	if f.addr == "" {
		return nil, errors.New("no upstream")
	}
	return (&net.Dialer{}).DialContext(ctx, "tcp", f.addr)
}

func (f fakeManager) Touch(context.Context, string) error { return nil }

func newLinkTestServer(t *testing.T, publicURL string) (*httptest.Server, *Handler) {
	t.Helper()
	h := &Handler{
		Manager:   fakeManager{sb: &sandbox.Sandbox{ID: "sb-1", Owner: "alice"}},
		Tokens:    NewStore(time.Minute),
		PublicURL: publicURL,
	}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, h
}

func fetchLink(t *testing.T, srv *httptest.Server) (*http.Response, previewLinkResp) {
	t.Helper()
	return fetchLinkFor(t, srv, "sb-1", 3000)
}

func fetchLinkFor(t *testing.T, srv *httptest.Server, id string, port int) (*http.Response, previewLinkResp) {
	t.Helper()
	resp, err := srv.Client().Get(fmt.Sprintf("%s/v1/sandboxes/%s/preview-link?port=%d", srv.URL, id, port))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var link previewLinkResp
	if err := json.NewDecoder(resp.Body).Decode(&link); err != nil {
		t.Fatal(err)
	}
	return resp, link
}

func TestPreviewLinkSameOriginUsesCookie(t *testing.T) {
	srv, h := newLinkTestServer(t, "")
	h.PublicURL = srv.URL // same host as the request

	resp, link := fetchLink(t, srv)
	if strings.Contains(link.URL, "token=") {
		t.Fatalf("same-origin URL must not carry the token: %s", link.URL)
	}
	if link.Token == "" {
		t.Fatal("token missing from response body")
	}

	var cookie *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == "roundpen_preview" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("roundpen_preview cookie not set")
	}
	if cookie.Value != link.Token {
		t.Fatalf("cookie value != token")
	}
	if !cookie.HttpOnly {
		t.Fatal("cookie must be HttpOnly")
	}
	if cookie.Path != "/p/sb-1/3000" {
		t.Fatalf("cookie path = %q", cookie.Path)
	}
}

func TestPreviewLinkCrossOriginKeepsURLToken(t *testing.T) {
	srv, _ := newLinkTestServer(t, "https://preview.example.com")

	resp, link := fetchLink(t, srv)
	if !strings.Contains(link.URL, "token="+link.Token) {
		t.Fatalf("cross-origin URL must carry the token: %s", link.URL)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "roundpen_preview" {
			t.Fatal("cookie on the console domain is useless for a separate preview origin")
		}
	}
}

func TestPreviewLinkUsesPreviewSubdomain(t *testing.T) {
	srv, h := newLinkTestServer(t, "")
	h.Domain = "preview.test"

	resp, link := fetchLink(t, srv)
	want := "http://sb-1-3000.preview.test/?token=" + link.Token
	if link.URL != want {
		t.Fatalf("url = %q, want %q", link.URL, want)
	}
	for _, c := range resp.Cookies() {
		if c.Name == "roundpen_preview" {
			t.Fatal("a cookie on the console origin never reaches a preview subdomain")
		}
	}

	h.Scheme = "https" // e.g. the reverse proxy terminates TLS for the zone
	if _, link := fetchLink(t, srv); !strings.HasPrefix(link.URL, "https://sb-1-3000.preview.test/?token=") {
		t.Fatalf("configured scheme ignored: %s", link.URL)
	}
}

func TestPreviewLinkKeepsPathFormForIdsThatCannotBeLabels(t *testing.T) {
	srv, h := newLinkTestServer(t, "http://127.0.0.1:9527")
	h.Domain = "preview.test"
	h.Manager = fakeManager{sb: &sandbox.Sandbox{ID: "Web_1", Owner: "alice"}}

	_, link := fetchLinkFor(t, srv, "Web_1", 8080)
	if !strings.HasPrefix(link.URL, "http://127.0.0.1:9527/p/Web_1/8080/?token=") {
		t.Fatalf("url = %q, want the path form", link.URL)
	}
}

func TestVhostRouterProxiesPreviewSubdomain(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "host=%s path=%s query=%q", r.Host, r.URL.Path, r.URL.RawQuery)
	}))
	defer upstream.Close()

	h := &Handler{
		Manager: fakeManager{sb: &sandbox.Sandbox{ID: "sb-1", Owner: "alice"}, addr: upstream.Listener.Addr().String()},
		Tokens:  NewStore(time.Minute),
		Domain:  "preview.test",
	}
	token, _, err := h.Tokens.Issue("sb-1", 3000, "alice")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h.VhostRouter(consoleStub()))
	defer srv.Close()

	// First navigation carries the token; the app's own paths are served as
	// written and it sees its public host.
	resp := vhostGet(t, srv, "sb-1-3000.preview.test", "/assets/app.js?token="+token+"&v=2")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if want := `host=sb-1-3000.preview.test path=/assets/app.js query="v=2"`; string(body) != want {
		t.Fatalf("upstream saw %s, want %s", body, want)
	}
	cookie := cookieNamed(resp, "roundpen_preview")
	if cookie == nil || cookie.Path != "/" || cookie.Value != token {
		t.Fatalf("preview cookie = %+v, want the token host-scoped at /", cookie)
	}

	// Later requests ride the cookie the proxy just planted.
	resp = vhostGet(t, srv, "sb-1-3000.preview.test", "/", cookie)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("cookie navigation: status = %d", resp.StatusCode)
	}
}

func TestVhostRouterRejectsUnauthorizedAndUnknownHosts(t *testing.T) {
	h := &Handler{
		Manager: fakeManager{sb: &sandbox.Sandbox{ID: "sb-1", Owner: "alice"}},
		Tokens:  NewStore(time.Minute),
		Domain:  "preview.test",
	}
	srv := httptest.NewServer(h.VhostRouter(consoleStub()))
	defer srv.Close()

	if resp := vhostGet(t, srv, "sb-1-3000.preview.test", "/"); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tokenless preview: status = %d, want 401", resp.StatusCode)
	}
	// A port that was never issued a token must not become reachable, even
	// with a token for another port of the same sandbox.
	token, _, err := h.Tokens.Issue("sb-1", 3000, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if resp := vhostGet(t, srv, "sb-1-8080.preview.test", "/", &http.Cookie{Name: "roundpen_preview", Value: token}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("other port: status = %d, want 401", resp.StatusCode)
	}
	// Tokens are bound to one sandbox port, so a sibling host stays closed
	// even with a token in hand ("sb-1" reads as sandbox "sb", port 1).
	if resp := vhostGet(t, srv, "sb-1.preview.test", "/", &http.Cookie{Name: "roundpen_preview", Value: token}); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("sibling host: status = %d, want 401", resp.StatusCode)
	}
	// Names inside the zone that are not a {sandbox}-{port} label are nobody's
	// preview — never the console.
	for _, host := range []string{"nope.preview.test", "-3000.preview.test", "sb-1-0.preview.test", "sb-1-70000.preview.test", "a.b-3000.preview.test"} {
		if resp := vhostGet(t, srv, host, "/"); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", host, resp.StatusCode)
		}
	}
	// Outside the zone — including the zone apex, which may host the console.
	for _, host := range []string{"console.test", "preview.test"} {
		resp := vhostGet(t, srv, host, "/")
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		if string(body) != "console" {
			t.Fatalf("%s: body = %q, want the console handler", host, body)
		}
	}
}

func TestPathProxyKeepsQueryTokensTheAppOwns(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "path=%s query=%q", r.URL.Path, r.URL.RawQuery)
	}))
	defer upstream.Close()

	h := &Handler{
		Manager: fakeManager{sb: &sandbox.Sandbox{ID: "sb-1", Owner: "alice"}, addr: upstream.Listener.Addr().String()},
		Tokens:  NewStore(time.Minute),
	}
	token, _, err := h.Tokens.Issue("sb-1", 3000, "alice")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	h.Mount(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// The preview token authenticates and is dropped; the app's own parameter
	// with the same name must still arrive.
	resp := vhostGet(t, srv, "", "/p/sb-1/3000/cb?token="+token+"&code=x")
	body := readBody(t, resp)
	if want := `path=/cb query="code=x"`; body != want {
		t.Fatalf("upstream saw %s, want %s", body, want)
	}

	resp = vhostGet(t, srv, "", "/p/sb-1/3000/cb?token=app-secret", &http.Cookie{Name: "roundpen_preview", Value: token})
	if body = readBody(t, resp); !strings.Contains(body, `token=app-secret`) {
		t.Fatalf("app token was stripped: %s", body)
	}
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func consoleStub() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("console"))
	})
}

func vhostGet(t *testing.T, srv *httptest.Server, host, path string, cookies ...*http.Cookie) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if host != "" {
		req.Host = host
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func cookieNamed(resp *http.Response, name string) *http.Cookie {
	for _, c := range resp.Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func TestTokenIssueAndLookup(t *testing.T) {
	s := NewStore(time.Minute)
	tok, exp, err := s.Issue("sb-1", 3000, "alice")
	if err != nil || tok == "" {
		t.Fatalf("issue: %v %q", err, tok)
	}
	if exp.Before(time.Now()) {
		t.Fatal("expires in the past")
	}
	sid, port, owner, ok := s.Lookup(tok)
	if !ok || sid != "sb-1" || port != 3000 || owner != "alice" {
		t.Fatalf("lookup: ok=%v sid=%s port=%d owner=%s", ok, sid, port, owner)
	}
	if _, _, _, ok := s.Lookup("nope"); ok {
		t.Fatal("expected miss")
	}
}

func TestTokenExpiry(t *testing.T) {
	s := NewStore(10 * time.Millisecond)
	tok, _, err := s.Issue("sb-1", 80, "alice")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(30 * time.Millisecond)
	if _, _, _, ok := s.Lookup(tok); ok {
		t.Fatal("expected expired")
	}
}
