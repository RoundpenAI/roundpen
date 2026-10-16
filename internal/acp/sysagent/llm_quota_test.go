package sysagent

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChatStreamMaps429ToFreeQuotaExceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"rate limited"}}`, http.StatusTooManyRequests)
	}))
	t.Cleanup(srv.Close)

	_, _, err := LLMConfig{
		BaseURL:    srv.URL,
		APIKey:     "k",
		Model:      "m",
		HTTPClient: srv.Client(),
	}.chat(context.Background(), []chatMessage{{Role: "user", Content: "hi"}}, nil)
	if !errors.Is(err, ErrFreeQuotaExceeded) {
		t.Fatalf("got %v, want ErrFreeQuotaExceeded", err)
	}
	if !strings.Contains(err.Error(), "系统管理 → LLM 网关") {
		t.Fatalf("message should point at 系统管理: %v", err)
	}
}
