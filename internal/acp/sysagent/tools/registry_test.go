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

func TestRegistry_OpenAIToolsAndCall(t *testing.T) {
	r := tools.NewRegistry()
	r.Register(tools.Tool{
		Name:        "echo",
		Description: "echo args",
		Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"msg": map[string]any{"type": "string"},
			},
		},
		Call: func(_ context.Context, _ tools.Actor, args json.RawMessage) (string, error) {
			return string(args), nil
		},
	})
	oa := r.OpenAITools()
	if len(oa) != 1 {
		t.Fatalf("len=%d", len(oa))
	}
	out, err := r.Call(context.Background(), tools.Actor{}, "echo", json.RawMessage(`{"msg":"hi"}`))
	if err != nil || out != `{"msg":"hi"}` {
		t.Fatalf("call=%q err=%v", out, err)
	}
}

func TestRoundpenHTTP_List(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "k" {
			http.Error(w, "unauthorized", 401)
			return
		}
		switch r.URL.Path {
		case "/v1/me/environments":
			_, _ = w.Write([]byte(`{"environments":[{"slot":"agent","status":"absent","sandboxId":"sb-secret"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	reg := tools.NewRegistry()
	tools.RegisterRoundpen(reg, &tools.RoundpenHTTP{BaseURL: srv.URL, HTTPClient: srv.Client()})
	out, err := reg.Call(context.Background(), tools.Actor{APIKey: "k"}, "ListEnvironments", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"environments"`) {
		t.Fatalf("got %q", out)
	}
	if strings.Contains(out, "sb-secret") || strings.Contains(out, "sandboxId") || strings.Contains(strings.ToLower(out), "sandbox") {
		t.Fatalf("leaked sandbox fields: %q", out)
	}
	if strings.Contains(out, "roundpen_ensure_agent") {
		t.Fatalf("old ensure wording: %q", out)
	}
	if _, err := reg.Call(context.Background(), tools.Actor{APIKey: "k"}, "roundpen_ensure_agent", nil); err == nil {
		t.Fatal("ensure must be gone")
	}
}
