package sysagent

import (
	"context"
	"encoding/json"
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
