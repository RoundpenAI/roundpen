package sysagent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLLMConfigRun(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		var req struct {
			Model    string        `json:"model"`
			Messages []chatMessage `json:"messages"`
			Tools    []any         `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		if len(req.Tools) != 0 {
			t.Errorf("expected no tools, got %d", len(req.Tools))
		}
		if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[1].Role != "user" {
			t.Errorf("messages = %+v", req.Messages)
		}
		if req.Messages[0].Content != "sys" || req.Messages[1].Content != "user text" {
			t.Errorf("content = %+v", req.Messages)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "extracted"}, "finish_reason": "stop"},
			},
		})
	}))
	defer srv.Close()

	c := LLMConfig{BaseURL: srv.URL, APIKey: "k", Model: "m"}
	out, err := c.Run(context.Background(), "sys", "user text")
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "extracted" {
		t.Fatalf("out = %q", out)
	}
}

func TestLLMConfigRunWithoutSystem(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []chatMessage `json:"messages"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
			t.Errorf("messages = %+v", req.Messages)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"},
			},
		})
	}))
	defer srv.Close()

	c := LLMConfig{BaseURL: srv.URL, APIKey: "k", Model: "m"}
	if _, err := c.Run(context.Background(), "  ", "hi"); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestExtractReasoning(t *testing.T) {
	cases := []struct {
		name    string
		content any
		reason  any
		want    string
	}{
		{"none", nil, nil, ""},
		{"deepseek string", "思考中", nil, "思考中"},
		{"openai string", nil, "step1", "step1"},
		{"openai array", nil, []any{"a", "b"}, "ab"},
		{"openai blob array", nil, []any{map[string]any{".": "x"}, "y"}, "xy"},
		{"both", "r1", []any{"r2"}, "r1r2"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := extractReasoning(tc.content, tc.reason); got != tc.want {
				t.Fatalf("extractReasoning = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsTransientLLMError(t *testing.T) {
	if !isTransientLLMError(fmt.Errorf("upstream stream terminated")) {
		t.Fatal("mid-stream interruption should be transient")
	}
	if isTransientLLMError(&httpStatusError{code: 401}) {
		t.Fatal("4xx should not be retried")
	}
	if !isTransientLLMError(&httpStatusError{code: 502}) {
		t.Fatal("5xx should be retried")
	}
	if isTransientLLMError(nil) {
		t.Fatal("nil should not be transient")
	}
	if isTransientLLMError(context.Canceled) {
		t.Fatal("cancellation should not be retried")
	}
}
