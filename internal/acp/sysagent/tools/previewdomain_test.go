package tools_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

func TestPreviewDomainToolNeedsAZone(t *testing.T) {
	reg := tools.NewRegistry()
	binder := &tools.AgentBinder{Slots: &stubAgentSlots{id: "sb-1"}}
	tools.RegisterPreviewDomain(reg, binder, &tools.RoundpenHTTP{BaseURL: "http://127.0.0.1:1"}, "  ")
	if _, ok := reg.Get("ClaimPreviewDomain"); ok {
		t.Fatal("without a preview zone there is nothing to hand out")
	}
	// Registration with missing dependencies must not panic either.
	tools.RegisterPreviewDomain(nil, nil, nil, "preview.test")
}

func TestPreviewDomainToolClaimsForTheAgentWorkspace(t *testing.T) {
	type call struct {
		method string
		path   string
		body   map[string]any
	}
	var got call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := map[string]any{}
		_ = json.Unmarshal(raw, &body)
		got = call{method: r.Method, path: r.URL.Path, body: body}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"demo","address":"demo.preview.test","url":"https://demo.preview.test/?token=t"}`))
	}))
	defer srv.Close()

	reg := tools.NewRegistry()
	binder := &tools.AgentBinder{Slots: &stubAgentSlots{id: "sb-1"}}
	tools.RegisterPreviewDomain(reg, binder, &tools.RoundpenHTTP{BaseURL: srv.URL}, "preview.test")

	out, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "ClaimPreviewDomain",
		json.RawMessage(`{"name":"demo","port":3000}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPost || got.path != "/v1/preview-domains" {
		t.Fatalf("called %s %s", got.method, got.path)
	}
	if got.body["sandboxID"] != "sb-1" || got.body["name"] != "demo" || got.body["port"].(float64) != 3000 {
		t.Fatalf("body = %v", got.body)
	}
	if out != `{"name":"demo","address":"demo.preview.test","url":"https://demo.preview.test/?token=t"}` {
		t.Fatalf("tool answer = %s", out)
	}

	if _, err := reg.Call(context.Background(), tools.Actor{Username: "alice"}, "ClaimPreviewDomain",
		json.RawMessage(`{"name":"demo"}`)); err == nil {
		t.Fatal("a claim without a port must be refused")
	}
}
