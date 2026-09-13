package tools_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

type stubModel struct {
	gotSystem string
	gotUser   string
	reply     string
	err       error
}

func (s *stubModel) Run(_ context.Context, system, user string) (string, error) {
	s.gotSystem = system
	s.gotUser = user
	return s.reply, s.err
}

func callTool(t *testing.T, reg *tools.Registry, name string, args map[string]any) (string, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return reg.Call(context.Background(), tools.Actor{Username: "u", Role: "user"}, name, raw)
}

func newWebRegistry(m tools.ModelRunner) *tools.Registry {
	reg := tools.NewRegistry()
	tools.RegisterWebFetch(reg, &tools.WebBinder{
		HTTP:  tools.NewWebHTTPClient(tools.WebClientOptions{AllowLoopback: true}),
		Model: m,
	})
	return reg
}

func htmlServer(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWebFetchConvertsHTML(t *testing.T) {
	srv := htmlServer(t, "text/html; charset=utf-8",
		`<html><body><h1>Hello</h1><p>World <a href="/docs">docs</a></p><script>var x=1</script></body></html>`)
	reg := newWebRegistry(nil)
	out, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL + "/page"})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	if !strings.Contains(out, "# Hello") {
		t.Fatalf("missing markdown heading: %q", out)
	}
	if !strings.Contains(out, srv.URL+"/docs") {
		t.Fatalf("relative link not absolutized: %q", out)
	}
	if strings.Contains(out, "var x=1") {
		t.Fatalf("script content leaked: %q", out)
	}
}

func TestWebFetchPassesThroughJSON(t *testing.T) {
	srv := htmlServer(t, "application/json", `{"a":1}`)
	reg := newWebRegistry(nil)
	out, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	if out != `{"a":1}` {
		t.Fatalf("out = %q", out)
	}
}

func TestWebFetchRejectsUnsupportedContentType(t *testing.T) {
	srv := htmlServer(t, "application/pdf", "%PDF-1.4")
	reg := newWebRegistry(nil)
	_, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL})
	if err == nil || !strings.Contains(err.Error(), "unsupported content type") {
		t.Fatalf("err = %v", err)
	}
}

func TestWebFetchRejectsNonHTTPURL(t *testing.T) {
	reg := newWebRegistry(nil)
	_, err := callTool(t, reg, "WebFetch", map[string]any{"url": "file:///etc/passwd"})
	if err == nil || !strings.Contains(err.Error(), "http") {
		t.Fatalf("err = %v", err)
	}
}

func TestWebFetchRequiresURL(t *testing.T) {
	reg := newWebRegistry(nil)
	_, err := callTool(t, reg, "WebFetch", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestWebFetchReportsBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	reg := newWebRegistry(nil)
	_, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL})
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err = %v", err)
	}
}

func TestWebFetchTruncatesLongContent(t *testing.T) {
	srv := htmlServer(t, "text/plain", strings.Repeat("x", 200_000))
	reg := newWebRegistry(nil)
	out, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	if len(out) > (32<<10)+8 {
		t.Fatalf("result too long: %d bytes", len(out))
	}
}

func TestWebFetchErrorsWhenPromptGivenWithoutModel(t *testing.T) {
	srv := htmlServer(t, "text/html", "<p>hi</p>")
	reg := newWebRegistry(nil)
	_, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL, "prompt": "what?"})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("err = %v", err)
	}
}
