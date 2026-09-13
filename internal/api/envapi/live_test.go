package envapi

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/storage"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

type fakeEnvs struct {
	target *userenv.BrowserTarget
	err    error
}

func (f *fakeEnvs) List(context.Context, string) ([]userenv.EnvView, error) { return nil, nil }

func (f *fakeEnvs) EnsureBrowser(context.Context, string) (*userenv.BrowserTarget, error) {
	return f.target, f.err
}

func (f *fakeEnvs) EnsureAgent(context.Context, string) (*sandbox.Sandbox, error) { return nil, nil }

type fakeDialer func(ctx context.Context, sandboxID string, destPort int) (net.Conn, error)

func (f fakeDialer) Dial(ctx context.Context, sandboxID string, destPort int) (net.Conn, error) {
	return f(ctx, sandboxID, destPort)
}

func TestLiveProxyRewritesPathAndInjectsToken(t *testing.T) {
	var gotPath, gotQuery string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		io.WriteString(w, "ok")
	}))
	defer up.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(up.URL, "http://"))

	target := &userenv.BrowserTarget{
		Key: "sb-browser", Provider: "docker", Managed: true,
		Sandbox: &sandbox.Sandbox{
			ID:       "sb-browser",
			Metadata: map[string]string{"browserToken": "browserless-token"},
		},
	}
	h := &Handler{
		Envs: &fakeEnvs{target: target},
		Dial: fakeDialer(func(context.Context, string, int) (net.Conn, error) {
			return net.Dial("tcp", host+":"+port)
		}),
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/me/environments/browser/live/app.bundle.js?x=1", nil)
	req = req.WithContext(authCtx(req.Context(), "alice"))
	rec := httptest.NewRecorder()
	h.live(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/debugger/app.bundle.js" {
		t.Fatalf("upstream path = %q, want /debugger/app.bundle.js", gotPath)
	}
	if !strings.Contains(gotQuery, "token=browserless-token") {
		t.Fatalf("upstream query = %q, want injected token", gotQuery)
	}
	if !strings.Contains(gotQuery, "x=1") {
		t.Fatalf("upstream query = %q, want preserved x=1", gotQuery)
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
