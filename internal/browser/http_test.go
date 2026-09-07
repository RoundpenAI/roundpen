package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
)

type fakeEngine struct {
	mu     sync.Mutex
	url    string
	title  string
	typed  map[string]string
	clicks []string
}

func (f *fakeEngine) Navigate(_ context.Context, url string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.url = url
	f.title = "page"
	return nil
}
func (f *fakeEngine) URL() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.url
}
func (f *fakeEngine) Title(context.Context) (string, error) { return f.title, nil }
func (f *fakeEngine) Snapshot(context.Context) (Snapshot, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return Snapshot{
		URL:   f.url,
		Title: f.title,
		Text:  "- Page: " + f.title + "\n- button \"Go\" [ref=e1]\n",
		Nodes: []SnapNode{{Ref: "e1", Role: "button", Name: "Go"}},
	}, nil
}
func (f *fakeEngine) Hover(context.Context, string) error { return nil }
func (f *fakeEngine) Click(_ context.Context, ref string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clicks = append(f.clicks, ref)
	return nil
}
func (f *fakeEngine) Type(_ context.Context, ref, text string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.typed == nil {
		f.typed = map[string]string{}
	}
	f.typed[ref] = text
	return nil
}
func (f *fakeEngine) Press(context.Context, string) error { return nil }
func (f *fakeEngine) Screenshot(context.Context) ([]byte, error) {
	return []byte{0x89, 0x50, 0x4e, 0x47}, nil
}
func (f *fakeEngine) SetViewport(context.Context, int, int) error { return nil }
func (f *fakeEngine) Evaluate(context.Context, string) (json.RawMessage, error) {
	return json.RawMessage(`"ok"`), nil
}
func (f *fakeEngine) InputClick(_ context.Context, x, y float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clicks = append(f.clicks, fmt.Sprintf("xy:%.0f,%.0f", x, y))
	return nil
}
func (f *fakeEngine) InputMove(context.Context, float64, float64) error { return nil }
func (f *fakeEngine) InputWheel(context.Context, float64, float64, float64, float64) error {
	return nil
}
func (f *fakeEngine) InputType(context.Context, string) error { return nil }
func (f *fakeEngine) InputKey(context.Context, string) error  { return nil }
func (f *fakeEngine) Close() error                            { return nil }

type stubView struct {
	sb *sandbox.Sandbox
}

func (m *stubView) Get(_ context.Context, id string) (*sandbox.Sandbox, error) {
	if m.sb == nil || m.sb.ID != id {
		return nil, sandbox.ErrNotFound
	}
	return m.sb, nil
}
func (m *stubView) Resolve(_ context.Context, req sandbox.ResolveRequest) (*sandbox.Sandbox, error) {
	if m.sb == nil {
		return nil, sandbox.ErrNotFound
	}
	if req.Name != "" && !strings.EqualFold(req.Name, m.sb.Name) {
		return nil, sandbox.ErrNotFound
	}
	if req.Category != "" && !strings.EqualFold(req.Category, m.sb.Category) {
		return nil, sandbox.ErrNotFound
	}
	return m.sb, nil
}
func (m *stubView) Touch(context.Context, string) error { return nil }

func TestHandler_navigateAndSnapshot(t *testing.T) {
	hub := NewHub(t.TempDir(), nil)
	eng := &fakeEngine{}
	hub.newEngine = func(string, int, int) (Engine, error) { return eng, nil }

	sb := &sandbox.Sandbox{
		ID: "sb-1", Name: "cursor-browser", Category: "Browser",
		Status: sandbox.StatusRunning, Metadata: map[string]string{"profile": "browser"},
	}
	h := &Handler{Sandboxes: &stubView{sb: sb}, Hub: hub}
	mux := http.NewServeMux()
	h.Mount(mux)

	body, _ := json.Marshal(map[string]string{"url": "http://127.0.0.1:19000/"})
	req := httptest.NewRequest(http.MethodPost, "/v1/sandboxes/sb-1/browser/navigate", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("navigate status=%d body=%s", rec.Code, rec.Body.String())
	}
	if eng.url != "http://127.0.0.1:19000/" {
		t.Fatalf("url=%q", eng.url)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/browser", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status endpoint=%d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"attached":true`) {
		t.Fatalf("expected attached: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/sandboxes/sb-1/browser/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("mcp list=%d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "browser_navigate") {
		t.Fatalf("missing tools: %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/v1/browser/mcp", strings.NewReader(
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"browser_click","arguments":{"ref":"e1"}}}`))
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("mcp click=%d %s", rec.Code, rec.Body.String())
	}
	if len(eng.clicks) != 1 || eng.clicks[0] != "e1" {
		t.Fatalf("clicks=%v", eng.clicks)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/sandboxes/sb-1/browser/screenshot", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("screenshot=%d ct=%s", rec.Code, rec.Header().Get("Content-Type"))
	}
	png, _ := io.ReadAll(rec.Body)
	if len(png) < 4 {
		t.Fatal("empty png")
	}
}
