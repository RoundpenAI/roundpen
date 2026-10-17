package tools_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

func vendorRegistry(t *testing.T, vendor, base string) *tools.Registry {
	t.Helper()
	reg := tools.NewRegistry()
	tools.RegisterWebSearch(reg, &tools.WebSearchBinder{
		Vendor:  vendor,
		BaseURL: base,
		APIKey:  "sk-vendor",
		Model:   "test-model",
		HTTP:    tools.NewWebHTTPClient(tools.WebClientOptions{AllowLoopback: true}),
	})
	return reg
}

func TestVendorSearchKimiGLMAndOpenRouter(t *testing.T) {
	kimi := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tools/search" {
			t.Errorf("kimi path = %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer sk-vendor" {
			t.Errorf("kimi auth = %q", r.Header.Get("Authorization"))
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["text_query"] != "golang release" {
			t.Errorf("query = %v", body["text_query"])
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"search_results": []map[string]any{
				{"title": "Go", "url": "https://go.dev/doc", "snippet": "Docs"},
				{"title": "Skip", "url": "https://spam.example/x", "snippet": "nope"},
			},
		})
	}))
	defer kimi.Close()
	out, err := callTool(t, vendorRegistry(t, "kimi", kimi.URL), "WebSearch", map[string]any{
		"query": "golang release", "blocked_domains": []string{"spam.example"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[Go](https://go.dev/doc): Docs") || strings.Contains(out, "spam.example") {
		t.Fatalf("kimi results: %q", out)
	}

	glm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/web_search" {
			t.Errorf("glm path = %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["search_engine"] != "search_std" || body["search_domain_filter"] != "go.dev" {
			t.Errorf("glm body = %v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"search_result": []map[string]any{{
				"title": "Release", "link": "https://go.dev/doc/devel/release", "content": "notes",
			}},
		})
	}))
	defer glm.Close()
	out, err = callTool(t, vendorRegistry(t, "glm", glm.URL), "WebSearch", map[string]any{
		"query": "golang release", "allowed_domains": []string{"go.dev"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[Release](https://go.dev/doc/devel/release): notes") {
		t.Fatalf("glm results: %q", out)
	}

	or := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("openrouter path = %s", r.URL.Path)
		}
		var body struct {
			Model   string `json:"model"`
			Plugins []struct {
				ID string `json:"id"`
			} `json:"plugins"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Model != "test-model" || len(body.Plugins) != 1 || body.Plugins[0].ID != "web" {
			t.Errorf("openrouter body = %+v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{
				"annotations": []any{map[string]any{
					"type": "url_citation",
					"url_citation": map[string]any{
						"url": "https://example.com/a", "title": "Cite", "content": "snippet",
					},
				}},
			}}},
		})
	}))
	defer or.Close()
	out, err = callTool(t, vendorRegistry(t, "openrouter", or.URL), "WebSearch", map[string]any{"query": "golang release"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[Cite](https://example.com/a): snippet") {
		t.Fatalf("openrouter results: %q", out)
	}
}

func TestVendorSearchDeepSeekParsesToolResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "sk-vendor" || r.Header.Get("anthropic-version") == "" {
			t.Errorf("headers api=%q version=%q", r.Header.Get("x-api-key"), r.Header.Get("anthropic-version"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"content": []any{
				map[string]any{
					"type": "web_search_tool_result",
					"content": []any{map[string]any{
						"type": "web_search_result", "url": "https://go.dev", "title": "Go",
					}},
				},
				map[string]any{
					"type":      "text",
					"text":      "see",
					"citations": []any{map[string]any{"url": "https://go.dev", "cited_text": "The language."}},
				},
			},
		})
	}))
	defer srv.Close()
	out, err := callTool(t, vendorRegistry(t, "deepseek", srv.URL), "WebSearch", map[string]any{"query": "golang docs"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[Go](https://go.dev): The language.") {
		t.Fatalf("deepseek results: %q", out)
	}
}

func TestVendorSearchRequiresKey(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterWebSearch(reg, &tools.WebSearchBinder{Vendor: "kimi"})
	if _, ok := reg.Get("WebSearch"); ok {
		t.Fatal("vendor search must not register without an API key")
	}
}
