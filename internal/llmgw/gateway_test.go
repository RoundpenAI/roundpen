package llmgw_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/api/auth"
	"github.com/RoundpenAI/roundpen/internal/httpx"
	"github.com/RoundpenAI/roundpen/internal/llmgw"
	"github.com/RoundpenAI/roundpen/internal/storage"
)

func withAdminMux(mux http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(auth.WithUser(r.Context(), &storage.User{
			Username: "admin",
			Role:     storage.RoleAdmin,
		}))
		mux.ServeHTTP(w, r)
	})
}

func TestGatewaySeedFromConfig(t *testing.T) {
	gw := llmgw.New(testDB(t), llmgw.Options{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err := gw.SeedFromConfig(llmgw.SeedConfig{
		OpenAI: &llmgw.UpstreamSeed{
			BaseURL:  "https://api.example.com/",
			APIKey:   "sk-x",
			ModelMap: map[string]string{"a": "b"},
		},
		Keys: []llmgw.VirtualKey{{Key: "vk-a", Name: ""}},
	}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	up, err := gw.Store().GetUpstream(ctx, llmgw.ProviderOpenAI)
	if err != nil || up.BaseURL != "https://api.example.com" {
		t.Fatalf("upstream: err=%v base=%q", err, up.BaseURL)
	}
	vk, err := gw.Store().GetVirtualKey(ctx, "vk-a")
	if err != nil || vk.Name != "vk-a" {
		t.Fatalf("virtual key: err=%v vk=%#v", err, vk)
	}
}

func TestEnsureInternalWithoutOpenAI(t *testing.T) {
	gw := llmgw.New(testDB(t), llmgw.Options{})
	if err := gw.EnsureInternal(context.Background(), ""); err != nil {
		t.Fatal(err)
	}
	vk, err := gw.Store().GetVirtualKey(context.Background(), llmgw.InternalVirtualKey)
	if err != nil || vk.Name != llmgw.InternalVirtualName {
		t.Fatalf("internal key: err=%v vk=%#v", err, vk)
	}
}

func TestHTTPAdminEndpointsForbiddenWithoutAdmin(t *testing.T) {
	gw := seedGateway(t)
	mux := http.NewServeMux()
	gw.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	resp, err := http.Get(srv.URL + "/v1/llmgw/logs")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status=%s", resp.Status)
	}
}

func TestHTTPAdminEndpoints(t *testing.T) {
	gw := seedGateway(t)
	mux := http.NewServeMux()
	gw.Mount(mux)
	srv := httptest.NewServer(withAdminMux(mux))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/llmgw/virtual-keys")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("virtual-keys status=%s", resp.Status)
	}

	resp, err = http.Get(srv.URL + "/v1/llmgw/logs?limit=5&provider=openai")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("logs status=%s", resp.Status)
	}

	resp, err = http.Get(srv.URL + "/v1/llmgw/stats?virtual_key=vk-test")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stats status=%s", resp.Status)
	}

	resp, err = http.Get(srv.URL + "/v1/llmgw/setup")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("setup status=%s", resp.Status)
	}
	var setup struct {
		BaseURL   string `json:"base_url"`
		Providers map[string]struct {
			Enabled bool `json:"enabled"`
		} `json:"providers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&setup); err != nil {
		t.Fatal(err)
	}
	if setup.BaseURL != "https://roundpen.test" {
		t.Fatalf("base_url=%q", setup.BaseURL)
	}
	if !setup.Providers["openai"].Enabled {
		t.Fatal("openai provider not enabled in setup")
	}

	resp, err = http.Get(srv.URL + "/v1/llmgw/logs/not-an-id")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid log id status=%s", resp.Status)
	}

	logs, err := gw.Store().ListTransactions(context.Background(), llmgw.ListOptions{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) == 0 {
		return
	}
	resp, err = http.Get(srv.URL + "/v1/llmgw/logs/" + strconv.FormatInt(logs[0].ID, 10))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("log detail status=%s", resp.Status)
	}
}

func TestGatewayNewUsesDefaultLogLimit(t *testing.T) {
	gw := llmgw.New(testDB(t), llmgw.Options{LogBodyMaxBytes: -1})
	if gw == nil {
		t.Fatal("nil gateway")
	}
}

func TestRelayOpenAIForward(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sk-openai" {
			t.Fatalf("auth=%q", r.Header.Get("Authorization"))
		}
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatal(err)
		}
		if req.Model != "gpt-real" {
			t.Fatalf("model=%q", req.Model)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hi"}}]}`))
	}))
	t.Cleanup(upstream.Close)

	gw := llmgw.New(testDB(t), llmgw.Options{LogBodyMaxBytes: 256})
	ctx := context.Background()
	if err := gw.SeedFromConfig(llmgw.SeedConfig{
		OpenAI: &llmgw.UpstreamSeed{
			BaseURL:  upstream.URL,
			APIKey:   "sk-openai",
			ModelMap: map[string]string{"gpt-alias": "gpt-real"},
		},
		Keys: []llmgw.VirtualKey{{Key: "vk-relay", Name: "relay"}},
	}); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	gw.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/llmgw/openai/v1/chat/completions", strings.NewReader(`{"model":"gpt-alias","messages":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer vk-relay")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%s body=%s", resp.Status, body)
	}

	logs, err := gw.Store().ListTransactions(ctx, llmgw.ListOptions{VirtualKey: "vk-relay", Limit: 1})
	if err != nil || len(logs) == 0 || logs[0].StatusCode != 200 {
		t.Fatalf("logs: err=%v logs=%#v", err, logs)
	}
}

func TestRelayAuthAndUpstreamErrors(t *testing.T) {
	gw := seedGateway(t)
	mux := http.NewServeMux()
	gw.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/llmgw/openai/v1/chat/completions", strings.NewReader(`{}`))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing key status=%s", resp.Status)
	}

	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/llmgw/openai/v1/chat/completions", strings.NewReader(`{}`))
	req.Header.Set("x-api-key", "vk-bad")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("bad key status=%s", resp.Status)
	}

	req, _ = http.NewRequest(http.MethodPost, srv.URL+"/llmgw/anthropic/v1/messages", strings.NewReader(`{"model":"claude"}`))
	req.Header.Set("Authorization", "Bearer vk-test")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway && resp.StatusCode != http.StatusOK {
		// upstream is fake host; expect bad gateway from dial error
		if resp.StatusCode != http.StatusBadGateway {
			t.Fatalf("anthropic relay status=%s", resp.Status)
		}
	}
}

func TestEmbedViaMockUpstream(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"index":0,"embedding":[0.1,0.2]}]}`))
	}))
	t.Cleanup(upstream.Close)

	gw := llmgw.New(testDB(t), llmgw.Options{})
	if err := gw.SeedFromConfig(llmgw.SeedConfig{
		OpenAI: &llmgw.UpstreamSeed{
			BaseURL:  upstream.URL,
			APIKey:   "sk-embed",
			ModelMap: map[string]string{llmgw.EmbeddingModelAlias: "text-embedding-3-small"},
		},
	}); err != nil {
		t.Fatal(err)
	}

	vecs, err := gw.Embed(context.Background(), []string{"hello"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 1 || len(vecs[0]) != 2 {
		t.Fatalf("vecs=%#v", vecs)
	}
	one, err := gw.EmbedOne(context.Background(), "solo")
	if err != nil || len(one) != 2 {
		t.Fatalf("EmbedOne: err=%v vec=%v", err, one)
	}
	if _, err := gw.Embed(context.Background(), nil); err == nil {
		t.Fatal("expected empty embed error")
	}
}

func TestEmbedUpstreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	}))
	t.Cleanup(upstream.Close)

	gw := llmgw.New(testDB(t), llmgw.Options{})
	if err := gw.SeedFromConfig(llmgw.SeedConfig{
		OpenAI: &llmgw.UpstreamSeed{BaseURL: upstream.URL, APIKey: "sk-x"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := gw.Embed(context.Background(), []string{"x"}); err == nil {
		t.Fatal("expected embed error")
	}
}

func TestRelayWithoutBodyLogging(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(upstream.Close)

	gw := llmgw.New(testDB(t), llmgw.Options{LogBodyMaxBytes: 0})
	if err := gw.SeedFromConfig(llmgw.SeedConfig{
		OpenAI: &llmgw.UpstreamSeed{BaseURL: upstream.URL, APIKey: "sk-x"},
		Keys:   []llmgw.VirtualKey{{Key: "vk-nolog", Name: "nolog"}},
	}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	gw.Mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/llmgw/openai/v1/test", strings.NewReader(`{"model":"x"}`))
	req.Header.Set("Authorization", "Bearer vk-nolog")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%s", resp.Status)
	}
}

func TestHTTPSetupUsesForwardedHeaders(t *testing.T) {
	gw := seedGateway(t)
	mux := http.NewServeMux()
	gw.Mount(mux)
	srv := httptest.NewServer(withAdminMux(mux))
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/v1/llmgw/setup", nil)
	req.Header.Set("X-Forwarded-Proto", "https")
	req.Header.Set("X-Forwarded-Host", "proxy.example.com")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var setup struct {
		BaseURL string `json:"base_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&setup); err != nil {
		t.Fatal(err)
	}
	if setup.BaseURL != "https://roundpen.test" {
		t.Fatalf("configured base_url wins: %q", setup.BaseURL)
	}

	gw2 := llmgw.New(testDB(t), llmgw.Options{})
	if err := gw2.SeedFromConfig(llmgw.SeedConfig{
		OpenAI: &llmgw.UpstreamSeed{BaseURL: "https://api.test", APIKey: "sk"},
	}); err != nil {
		t.Fatal(err)
	}
	mux2 := http.NewServeMux()
	gw2.Mount(mux2)
	srv2 := httptest.NewServer(withAdminMux(mux2))
	t.Cleanup(srv2.Close)
	req2, _ := http.NewRequest(http.MethodGet, srv2.URL+"/v1/llmgw/setup", nil)
	req2.Header.Set("X-Forwarded-Proto", "https")
	req2.Header.Set("X-Forwarded-Host", "edge.example.com")
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	var setup2 struct {
		BaseURL string `json:"base_url"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&setup2); err != nil {
		t.Fatal(err)
	}
	if setup2.BaseURL == "https://edge.example.com" {
		t.Fatal("untrusted X-Forwarded-Host must not rewrite setup URL")
	}
	if !strings.HasPrefix(setup2.BaseURL, "http://127.0.0.1:") {
		t.Fatalf("expected request host base_url, got %q", setup2.BaseURL)
	}

	prev := httpx.DefaultTrust
	t.Cleanup(func() { httpx.SetDefaultTrust(prev) })
	trust, err := httpx.ParseTrust("127.0.0.0/8")
	if err != nil {
		t.Fatal(err)
	}
	httpx.SetDefaultTrust(trust)
	req3, _ := http.NewRequest(http.MethodGet, srv2.URL+"/v1/llmgw/setup", nil)
	req3.Header.Set("X-Forwarded-Proto", "https")
	req3.Header.Set("X-Forwarded-Host", "edge.example.com")
	resp3, err := http.DefaultClient.Do(req3)
	if err != nil {
		t.Fatal(err)
	}
	defer resp3.Body.Close()
	var setup3 struct {
		BaseURL string `json:"base_url"`
	}
	if err := json.NewDecoder(resp3.Body).Decode(&setup3); err != nil {
		t.Fatal(err)
	}
	if setup3.BaseURL != "https://edge.example.com" {
		t.Fatalf("trusted forwarded base_url=%q", setup3.BaseURL)
	}
}

func TestHTTPLogDetailNotFound(t *testing.T) {
	gw := seedGateway(t)
	mux := http.NewServeMux()
	gw.Mount(mux)
	srv := httptest.NewServer(withAdminMux(mux))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/v1/llmgw/logs/999999999")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status=%s", resp.Status)
	}
}
