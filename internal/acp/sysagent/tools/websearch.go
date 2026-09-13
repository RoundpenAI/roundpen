package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultWebSearchEndpoint = "https://api.tavily.com"
	webSearchTimeout         = 30 * time.Second
	webSearchDefaultResults  = 8
	webSearchMaxResults      = 20
	maxWebSearchErrorBody    = 1 << 20
)

// WebSearchBinder calls a Tavily-compatible search API.
type WebSearchBinder struct {
	Endpoint string       // 空 → https://api.tavily.com
	APIKey   string       // 空 → 不带 Authorization 头
	HTTP     *http.Client // 生产用 NewWebHTTPClient 构造
}

type tavilyHit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Content string `json:"content"`
}

// RegisterWebSearch adds the WebSearch tool. endpoint 与 key 都为空时不注册。
func RegisterWebSearch(r *Registry, b *WebSearchBinder) {
	if r == nil || b == nil || (strings.TrimSpace(b.Endpoint) == "" && strings.TrimSpace(b.APIKey) == "") {
		return
	}
	r.Register(Tool{
		Name: "WebSearch",
		Description: "Search the web for current information and return titles, URLs, and snippets. " +
			"Use it for recent events, current docs, and facts beyond your training data. " +
			"Cite the URLs you used in your answer.",
		Parameters: objectSchema(map[string]any{
			"query":           map[string]any{"type": "string", "description": "Search query"},
			"allowed_domains": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Only include results from these domains"},
			"blocked_domains": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Never include results from these domains"},
			"num_results":     map[string]any{"type": "integer", "description": "Number of results (default 8, max 20)"},
		}, "query"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				Query          string   `json:"query"`
				AllowedDomains []string `json:"allowed_domains"`
				BlockedDomains []string `json:"blocked_domains"`
				NumResults     int      `json:"num_results"`
			}
			if err := json.Unmarshal(args, &in); err != nil {
				return "", fmt.Errorf("query is required")
			}
			query := strings.TrimSpace(in.Query)
			if query == "" {
				return "", fmt.Errorf("query is required")
			}
			if len([]rune(query)) < 2 {
				return "", fmt.Errorf("query is too short")
			}
			if len(in.AllowedDomains) > 0 && len(in.BlockedDomains) > 0 {
				return "", fmt.Errorf("allowed_domains and blocked_domains cannot be used together")
			}
			return b.search(ctx, query, in.AllowedDomains, in.BlockedDomains, in.NumResults)
		},
	})
}

func (b *WebSearchBinder) search(ctx context.Context, query string, allowed, blocked []string, numResults int) (string, error) {
	if b.HTTP == nil {
		return "", fmt.Errorf("web search is not configured")
	}
	if numResults <= 0 {
		numResults = webSearchDefaultResults
	}
	if numResults > webSearchMaxResults {
		numResults = webSearchMaxResults
	}
	if allowed == nil {
		allowed = []string{}
	}
	if blocked == nil {
		blocked = []string{}
	}
	payload, err := json.Marshal(map[string]any{
		"query":           query,
		"search_depth":    "basic",
		"max_results":     numResults,
		"include_domains": allowed,
		"exclude_domains": blocked,
	})
	if err != nil {
		return "", err
	}
	endpoint := strings.TrimSpace(b.Endpoint)
	if endpoint == "" {
		endpoint = defaultWebSearchEndpoint
	}
	endpoint = strings.TrimRight(endpoint, "/") + "/search"
	ctx, cancel := context.WithTimeout(ctx, webSearchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("web search is not configured")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", webUserAgent)
	if key := strings.TrimSpace(b.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := b.HTTP.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return "", fmt.Errorf("web search failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxWebSearchErrorBody))
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			return "", fmt.Errorf("web search failed: HTTP %d", resp.StatusCode)
		}
		return "", fmt.Errorf("web search failed: HTTP %d: %s", resp.StatusCode, truncateRunes(msg, 500))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxWebBodyBytes+1))
	if err != nil {
		return "", fmt.Errorf("web search failed: %w", err)
	}
	if len(body) > maxWebBodyBytes {
		return "", fmt.Errorf("web search failed: response exceeds 10MB")
	}
	var out struct {
		Results []tavilyHit `json:"results"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("web search failed: %w", err)
	}
	return formatWebSearchResults(query, out.Results), nil
}

func formatWebSearchResults(query string, hits []tavilyHit) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Web search results for query: %q\n\n", query)
	if len(hits) == 0 {
		return sb.String() + "No search results found."
	}
	for _, h := range hits {
		fmt.Fprintf(&sb, "- [%s](%s)",
			strings.Join(strings.Fields(h.Title), " "),
			strings.Join(strings.Fields(h.URL), " "))
		if snippet := strings.Join(strings.Fields(h.Content), " "); snippet != "" {
			fmt.Fprintf(&sb, ": %s", snippet)
		}
		sb.WriteString("\n")
	}
	// 提醒放在截断之外：结果很长时引用来源的指令不能被挤掉。
	return truncateRunes(sb.String(), maxWebResult) + "\nInclude the sources above in your response as markdown links."
}
