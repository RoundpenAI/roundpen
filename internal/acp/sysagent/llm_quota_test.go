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
	if !strings.Contains(err.Error(), "系统管理 → 模型配置") {
		t.Fatalf("message should point at 系统管理 → 模型配置: %v", err)
	}
}

func TestChatStreamMaps401InstallAuthToInstallAuthFailed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "install auth: public key mismatch", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	_, _, err := LLMConfig{
		BaseURL:    srv.URL,
		APIKey:     "k",
		Model:      "m",
		HTTPClient: srv.Client(),
	}.chat(context.Background(), []chatMessage{{Role: "user", Content: "hi"}}, nil)
	if !errors.Is(err, ErrInstallAuthFailed) {
		t.Fatalf("got %v, want ErrInstallAuthFailed", err)
	}
	if !strings.Contains(err.Error(), "系统管理 → 模型配置") {
		t.Fatalf("message should point at 模型配置: %v", err)
	}
	if !strings.Contains(err.Error(), "安装身份已失效") {
		t.Fatalf("message should explain install identity: %v", err)
	}
}

func TestChatStreamKeepsGeneric401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"invalid api key"}}`, http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	_, _, err := LLMConfig{
		BaseURL:    srv.URL,
		APIKey:     "k",
		Model:      "m",
		HTTPClient: srv.Client(),
	}.chat(context.Background(), []chatMessage{{Role: "user", Content: "hi"}}, nil)
	if errors.Is(err, ErrInstallAuthFailed) {
		t.Fatal("generic 401 must not map to ErrInstallAuthFailed")
	}
	var he *httpStatusError
	if !errors.As(err, &he) || he.code != http.StatusUnauthorized {
		t.Fatalf("got %v, want httpStatusError 401", err)
	}
}
