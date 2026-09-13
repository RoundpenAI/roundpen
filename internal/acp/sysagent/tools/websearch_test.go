package tools_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

func newSearchRegistry(t *testing.T, endpoint, key string) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	tools.RegisterWebSearch(reg, &tools.WebSearchBinder{
		Endpoint: endpoint,
		APIKey:   key,
		HTTP:     tools.NewWebHTTPClient(tools.WebClientOptions{AllowLoopback: true}),
	})
	return reg
}

func TestWebSearchCallsTavilyAndFormatsResults(t *testing.T) {
	var gotAuth string
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Errorf("path = %s", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{
			{"title": "Go docs", "url": "https://go.dev/doc", "content": "Official Go documentation."},
			{"title": "Second", "url": "https://example.com/b", "content": "More\nlines."},
		}})
	}))
	defer srv.Close()

	reg := newSearchRegistry(t, srv.URL, "tvly-test")
	out, err := callTool(t, reg, "WebSearch", map[string]any{
		"query":           "golang",
		"num_results":     3,
		"blocked_domains": []string{"spam.example"},
	})
	if err != nil {
		t.Fatalf("WebSearch: %v", err)
	}
	if gotAuth != "Bearer tvly-test" {
		t.Fatalf("auth header = %q", gotAuth)
	}
	if gotBody["query"] != "golang" {
		t.Fatalf("query = %v", gotBody["query"])
	}
	if gotBody["max_results"].(float64) != 3 {
		t.Fatalf("max_results = %v", gotBody["max_results"])
	}
	if gotBody["search_depth"] != "basic" {
		t.Fatalf("search_depth = %v", gotBody["search_depth"])
	}
	if ex := gotBody["exclude_domains"].([]any); len(ex) != 1 || ex[0] != "spam.example" {
		t.Fatalf("exclude_domains = %v", gotBody["exclude_domains"])
	}
	if !strings.Contains(out, "[Go docs](https://go.dev/doc): Official Go documentation.") {
		t.Fatalf("missing first hit: %q", out)
	}
	if !strings.Contains(out, "- [Second](https://example.com/b): More lines.") {
		t.Fatalf("snippet newlines not collapsed: %q", out)
	}
	if !strings.Contains(out, "Include the sources above in your response as markdown links.") {
		t.Fatalf("missing sources reminder: %q", out)
	}
}

func TestWebSearchDisabledWithoutConfig(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterWebSearch(reg, &tools.WebSearchBinder{})
	if _, ok := reg.Get("WebSearch"); ok {
		t.Fatal("WebSearch must not register without endpoint/key")
	}
}

func TestWebSearchRegistersWithEndpointOnly(t *testing.T) {
	reg := newSearchRegistry(t, "https://search.internal.example", "")
	if _, ok := reg.Get("WebSearch"); !ok {
		t.Fatal("WebSearch should register with endpoint-only config")
	}
}

func TestWebSearchClampsNumResults(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}})
	}))
	defer srv.Close()
	reg := newSearchRegistry(t, srv.URL, "k")
	if _, err := callTool(t, reg, "WebSearch", map[string]any{"query": "golang", "num_results": 99}); err != nil {
		t.Fatalf("WebSearch: %v", err)
	}
	if gotBody["max_results"].(float64) != 20 {
		t.Fatalf("max_results = %v", gotBody["max_results"])
	}
}

func TestWebSearchDefaultsNumResults(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}})
	}))
	defer srv.Close()
	reg := newSearchRegistry(t, srv.URL, "k")
	if _, err := callTool(t, reg, "WebSearch", map[string]any{"query": "golang"}); err != nil {
		t.Fatalf("WebSearch: %v", err)
	}
	if gotBody["max_results"].(float64) != 8 {
		t.Fatalf("max_results = %v", gotBody["max_results"])
	}
}

func TestWebSearchRejectsBothDomainLists(t *testing.T) {
	reg := newSearchRegistry(t, "https://search.internal.example", "k")
	_, err := callTool(t, reg, "WebSearch", map[string]any{
		"query":           "golang",
		"allowed_domains": []string{"a.example"},
		"blocked_domains": []string{"b.example"},
	})
	if err == nil || !strings.Contains(err.Error(), "cannot be used together") {
		t.Fatalf("err = %v", err)
	}
}

func TestWebSearchRequiresQuery(t *testing.T) {
	reg := newSearchRegistry(t, "https://search.internal.example", "k")
	if _, err := callTool(t, reg, "WebSearch", map[string]any{}); err == nil || !strings.Contains(err.Error(), "query is required") {
		t.Fatalf("err = %v", err)
	}
	if _, err := callTool(t, reg, "WebSearch", map[string]any{"query": "a"}); err == nil || !strings.Contains(err.Error(), "query is too short") {
		t.Fatalf("err = %v", err)
	}
}

func TestWebSearchEmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}})
	}))
	defer srv.Close()
	reg := newSearchRegistry(t, srv.URL, "k")
	out, err := callTool(t, reg, "WebSearch", map[string]any{"query": "nothing"})
	if err != nil {
		t.Fatalf("WebSearch: %v", err)
	}
	if !strings.Contains(out, "No search results found.") {
		t.Fatalf("out = %q", out)
	}
}

func TestWebSearchReportsHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(432)
		_, _ = w.Write([]byte(`{"detail":"plan limit"}`))
	}))
	defer srv.Close()
	reg := newSearchRegistry(t, srv.URL, "k")
	_, err := callTool(t, reg, "WebSearch", map[string]any{"query": "golang"})
	if err == nil || !strings.Contains(err.Error(), "HTTP 432") {
		t.Fatalf("err = %v", err)
	}
}
