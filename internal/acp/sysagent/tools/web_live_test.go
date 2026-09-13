package tools_test

import (
	"os"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

// 实网测试需显式开启：ROUNDPEN_WEB_LIVE_TESTS=1（CI/内网默认离线）。
func webLiveEnabled(t *testing.T) {
	t.Helper()
	if os.Getenv("ROUNDPEN_WEB_LIVE_TESTS") != "1" {
		t.Skip("ROUNDPEN_WEB_LIVE_TESTS=1 not set")
	}
}

func TestWebFetchLive(t *testing.T) {
	webLiveEnabled(t)
	reg := tools.NewRegistry()
	tools.RegisterWebFetch(reg, &tools.WebBinder{HTTP: tools.NewWebHTTPClient(tools.WebClientOptions{})})
	out, err := callTool(t, reg, "WebFetch", map[string]any{"url": "https://go.dev/"})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("empty result")
	}
}

func TestWebSearchLive(t *testing.T) {
	webLiveEnabled(t)
	key := os.Getenv("ROUNDPEN_WEB_SEARCH_API_KEY")
	if key == "" {
		t.Skip("ROUNDPEN_WEB_SEARCH_API_KEY not set")
	}
	reg := newSearchRegistry(t, os.Getenv("ROUNDPEN_WEB_SEARCH_ENDPOINT"), key)
	out, err := callTool(t, reg, "WebSearch", map[string]any{"query": "golang html to markdown", "num_results": 3})
	if err != nil {
		t.Fatalf("WebSearch: %v", err)
	}
	if !strings.Contains(out, "http") {
		t.Fatalf("no links in result: %q", out)
	}
}
