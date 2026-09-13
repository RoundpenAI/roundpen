package tools_test

import (
	"context"
	"encoding/json"
	"errors"
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
	var gotAccept, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><h1>Hello</h1><p>World <a href="/docs">docs</a> and <a href="https://example.com/secure">secure</a></p><script>var x=1</script></body></html>`))
	}))
	defer srv.Close()

	reg := newWebRegistry(nil)
	out, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL + "/page"})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	if gotAccept != "text/markdown, text/html, */*" {
		t.Fatalf("Accept = %q", gotAccept)
	}
	if gotUA != "Roundpen-WebFetch/0.1" {
		t.Fatalf("User-Agent = %q", gotUA)
	}
	if !strings.Contains(out, "# Hello") {
		t.Fatalf("missing markdown heading: %q", out)
	}
	if !strings.Contains(out, srv.URL+"/docs") {
		t.Fatalf("relative link not absolutized: %q", out)
	}
	if !strings.Contains(out, "https://example.com/secure") {
		t.Fatalf("absolute https link not preserved: %q", out)
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
	if !strings.HasPrefix(out, "xxx") {
		t.Fatalf("truncated output must keep the start of the page: %q", out[:min(20, len(out))])
	}
	if !strings.HasSuffix(out, "…") {
		t.Fatalf("truncated output must end with the ellipsis marker: %q", out[len(out)-min(20, len(out)):])
	}
}

func TestWebFetchRejectsOversizeBody(t *testing.T) {
	srv := htmlServer(t, "text/plain", strings.Repeat("x", (10<<20)+1))
	reg := newWebRegistry(nil)
	_, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL})
	if err == nil || !strings.Contains(err.Error(), "10MB") {
		t.Fatalf("err = %v", err)
	}
}

func TestWebFetchRejectsCredentialURL(t *testing.T) {
	reg := newWebRegistry(nil)
	_, err := callTool(t, reg, "WebFetch", map[string]any{"url": "http://user:pass@example.com/"})
	if err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("err = %v", err)
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

func TestWebFetchUsesModelWhenPromptGiven(t *testing.T) {
	srv := htmlServer(t, "text/html", "<p>the page says blue</p>")
	m := &stubModel{reply: "The answer is blue."}
	reg := newWebRegistry(m)
	out, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL, "prompt": "What color?"})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	if out != "The answer is blue." {
		t.Fatalf("out = %q", out)
	}
	if !strings.Contains(m.gotUser, "the page says blue") {
		t.Fatalf("model did not receive page content: %q", m.gotUser)
	}
	if !strings.Contains(m.gotUser, "What color?") {
		t.Fatalf("model did not receive prompt: %q", m.gotUser)
	}
}

func TestWebFetchWithoutPromptSkipsModel(t *testing.T) {
	srv := htmlServer(t, "text/html", "<p>plain page</p>")
	m := &stubModel{reply: "should not be used"}
	reg := newWebRegistry(m)
	out, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	if !strings.Contains(out, "plain page") {
		t.Fatalf("out = %q", out)
	}
	if m.gotUser != "" {
		t.Fatalf("model must not be called without prompt, got %q", m.gotUser)
	}
}

func TestWebFetchTruncatesModelInput(t *testing.T) {
	srv := htmlServer(t, "text/plain", strings.Repeat("y", 150_000))
	m := &stubModel{reply: "ok"}
	reg := newWebRegistry(m)
	if _, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL, "prompt": "summarize"}); err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	if !strings.Contains(m.gotUser, "[Content truncated due to length]") {
		t.Fatalf("model input not truncated: %d bytes", len(m.gotUser))
	}
	if len(m.gotUser) > 200_000 {
		t.Fatalf("model input too large: %d bytes", len(m.gotUser))
	}
}

func TestWebFetchModelErrorPropagates(t *testing.T) {
	srv := htmlServer(t, "text/html", "<p>hi</p>")
	m := &stubModel{err: errors.New("boom")}
	reg := newWebRegistry(m)
	_, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL, "prompt": "what?"})
	if err == nil || !strings.Contains(err.Error(), "extraction failed") {
		t.Fatalf("err = %v", err)
	}
}
