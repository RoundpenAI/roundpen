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
)

// searchVendor calls the built-in platform's own web search and formats the
// hits the same way the Tavily backend does.
func (b *WebSearchBinder) searchVendor(ctx context.Context, query string, allowed, blocked []string, numResults int) (string, error) {
	if numResults <= 0 {
		numResults = webSearchDefaultResults
	}
	if numResults > webSearchMaxResults {
		numResults = webSearchMaxResults
	}
	var hits []tavilyHit
	var err error
	switch strings.TrimSpace(b.Vendor) {
	case "deepseek":
		hits, err = b.searchDeepSeek(ctx, query, numResults)
	case "kimi":
		hits, err = b.searchKimi(ctx, query, numResults)
	case "glm":
		hits, err = b.searchGLM(ctx, query, allowed, numResults)
	case "openrouter":
		hits, err = b.searchOpenRouter(ctx, query, numResults)
	default:
		return "", fmt.Errorf("web search failed: unknown provider %q", b.Vendor)
	}
	if err != nil {
		return "", err
	}
	return formatWebSearchResults(query, filterSearchHits(hits, allowed, blocked)), nil
}

func (b *WebSearchBinder) searchDeepSeek(ctx context.Context, query string, numResults int) ([]tavilyHit, error) {
	model := strings.TrimSpace(b.Model)
	if model == "" {
		model = "deepseek-flash"
	}
	body := map[string]any{
		"model":      model,
		"max_tokens": 4096,
		"thinking":   map[string]string{"type": "disabled"},
		"messages": []any{map[string]any{
			"role": "user",
			"content": []any{map[string]any{
				"type": "text",
				"text": "Perform a web search for the query: " + query,
			}},
		}},
		"tools": []any{map[string]any{
			"type":     "web_search_20250305",
			"name":     "web_search",
			"max_uses": 1,
		}},
	}
	raw, err := b.postVendor(ctx, strings.TrimRight(b.BaseURL, "/")+"/messages", body, map[string]string{
		"x-api-key":         b.APIKey,
		"anthropic-version": "2023-06-01",
	})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Content []struct {
			Type    string `json:"type"`
			Text    string `json:"text"`
			Content []struct {
				Type  string `json:"type"`
				URL   string `json:"url"`
				Title string `json:"title"`
			} `json:"content"`
			Citations []struct {
				URL       string `json:"url"`
				CitedText string `json:"cited_text"`
			} `json:"citations"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("web search failed: %w", err)
	}
	snippets := map[string]string{}
	for _, block := range resp.Content {
		if block.Type != "text" {
			continue
		}
		for _, cite := range block.Citations {
			if cite.URL != "" && cite.CitedText != "" {
				if _, ok := snippets[cite.URL]; !ok {
					snippets[cite.URL] = cite.CitedText
				}
			}
		}
	}
	var hits []tavilyHit
	seen := map[string]struct{}{}
	for _, block := range resp.Content {
		if block.Type != "web_search_tool_result" {
			continue
		}
		for _, item := range block.Content {
			if item.Type != "web_search_result" || item.URL == "" {
				continue
			}
			if _, ok := seen[item.URL]; ok {
				continue
			}
			seen[item.URL] = struct{}{}
			hits = append(hits, tavilyHit{Title: item.Title, URL: item.URL, Content: snippets[item.URL]})
			if len(hits) >= numResults {
				return hits, nil
			}
		}
	}
	if len(hits) == 0 {
		return nil, fmt.Errorf("web search failed: provider returned no results")
	}
	return hits, nil
}

func (b *WebSearchBinder) searchKimi(ctx context.Context, query string, numResults int) ([]tavilyHit, error) {
	raw, err := b.postVendor(ctx, strings.TrimRight(b.BaseURL, "/")+"/tools/search", map[string]any{
		"text_query":      query,
		"limit":           numResults,
		"timeout_seconds": 20,
	}, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		SearchResults []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Snippet string `json:"snippet"`
		} `json:"search_results"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("web search failed: %w", err)
	}
	hits := make([]tavilyHit, 0, len(resp.SearchResults))
	for _, item := range resp.SearchResults {
		if item.URL == "" && item.Title == "" {
			continue
		}
		hits = append(hits, tavilyHit{Title: item.Title, URL: item.URL, Content: item.Snippet})
	}
	return hits, nil
}

func (b *WebSearchBinder) searchGLM(ctx context.Context, query string, allowed []string, numResults int) ([]tavilyHit, error) {
	payload := map[string]any{
		"search_engine": "search_std",
		"search_query":  query,
		"count":         numResults,
	}
	if len(allowed) == 1 {
		payload["search_domain_filter"] = allowed[0]
	}
	raw, err := b.postVendor(ctx, strings.TrimRight(b.BaseURL, "/")+"/web_search", payload, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		SearchResult []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Content string `json:"content"`
		} `json:"search_result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("web search failed: %w", err)
	}
	hits := make([]tavilyHit, 0, len(resp.SearchResult))
	for _, item := range resp.SearchResult {
		hits = append(hits, tavilyHit{Title: item.Title, URL: item.Link, Content: item.Content})
	}
	return hits, nil
}

func (b *WebSearchBinder) searchOpenRouter(ctx context.Context, query string, numResults int) ([]tavilyHit, error) {
	model := strings.TrimSpace(b.Model)
	if model == "" {
		model = "openrouter/auto"
	}
	raw, err := b.postVendor(ctx, strings.TrimRight(b.BaseURL, "/")+"/chat/completions", map[string]any{
		"model": model,
		"messages": []any{map[string]any{
			"role":    "user",
			"content": query,
		}},
		"plugins": []any{map[string]any{"id": "web", "max_results": numResults}},
	}, nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Annotations []struct {
					Type        string `json:"type"`
					URLCitation struct {
						URL     string `json:"url"`
						Title   string `json:"title"`
						Content string `json:"content"`
					} `json:"url_citation"`
				} `json:"annotations"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("web search failed: %w", err)
	}
	var hits []tavilyHit
	seen := map[string]struct{}{}
	for _, choice := range resp.Choices {
		for _, ann := range choice.Message.Annotations {
			if ann.Type != "url_citation" || ann.URLCitation.URL == "" {
				continue
			}
			if _, ok := seen[ann.URLCitation.URL]; ok {
				continue
			}
			seen[ann.URLCitation.URL] = struct{}{}
			hits = append(hits, tavilyHit{
				Title:   ann.URLCitation.Title,
				URL:     ann.URLCitation.URL,
				Content: ann.URLCitation.Content,
			})
			if len(hits) >= numResults {
				return hits, nil
			}
		}
	}
	if len(hits) == 0 {
		return nil, fmt.Errorf("web search failed: provider returned no results")
	}
	return hits, nil
}

func (b *WebSearchBinder) postVendor(ctx context.Context, endpoint string, payload any, headers map[string]string) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, webSearchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("web search endpoint is invalid")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", webSearchUserAgent)
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(b.APIKey))
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := b.HTTP.Do(req)
	if err != nil {
		var uerr *url.Error
		if errors.As(err, &uerr) {
			err = uerr.Err
		}
		return nil, fmt.Errorf("web search failed: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxWebBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("web search failed: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := strings.TrimSpace(string(raw))
		if msg == "" {
			return nil, fmt.Errorf("web search failed: HTTP %d", resp.StatusCode)
		}
		return nil, fmt.Errorf("web search failed: HTTP %d: %s", resp.StatusCode, truncateRunes(msg, 500))
	}
	if len(raw) > maxWebBodyBytes {
		return nil, fmt.Errorf("web search failed: response exceeds 10MB")
	}
	return raw, nil
}

func filterSearchHits(hits []tavilyHit, allowed, blocked []string) []tavilyHit {
	if len(allowed) == 0 && len(blocked) == 0 {
		return hits
	}
	out := make([]tavilyHit, 0, len(hits))
	for _, hit := range hits {
		if searchHostAllowed(hit.URL, allowed, blocked) {
			out = append(out, hit)
		}
	}
	return out
}

func searchHostAllowed(raw string, allowed, blocked []string) bool {
	host := strings.ToLower(raw)
	if u, err := url.Parse(raw); err == nil && u.Hostname() != "" {
		host = strings.ToLower(u.Hostname())
	}
	if len(allowed) > 0 {
		ok := false
		for _, domain := range allowed {
			if hostMatches(host, domain) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, domain := range blocked {
		if hostMatches(host, domain) {
			return false
		}
	}
	return true
}

func hostMatches(host, domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" {
		return false
	}
	return host == domain || strings.HasSuffix(host, "."+domain)
}
