package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/PuerkitoBio/goquery"
)

const (
	maxWebBodyBytes = 10 << 20 // 单次抓取的响应体上限
	maxWebMarkdown  = 100_000  // 送入二次提炼的正文上限（对齐参考实现）
	maxWebResult    = 32 << 10 // 工具返回文本上限
	webFetchTimeout = 30 * time.Second
	webModelTimeout = 60 * time.Second
	webUserAgent    = "Roundpen-WebFetch/0.1"
	maxWebURLLen    = 2000
)

// ModelRunner 执行一次无工具的纯文本 LLM 调用，用于 WebFetch 二次提炼。
type ModelRunner interface {
	Run(ctx context.Context, system, user string) (string, error)
}

// WebBinder runs the WebFetch tool.
type WebBinder struct {
	HTTP  *http.Client // 生产用 NewWebHTTPClient 构造
	Model ModelRunner  // 二次提炼；nil 时带 prompt 的调用报错
}

// RegisterWebFetch adds the WebFetch tool (control-plane fetch, read-only).
func RegisterWebFetch(r *Registry, b *WebBinder) {
	if r == nil || b == nil {
		return
	}
	r.Register(Tool{
		Name: "WebFetch",
		Description: "Fetch a URL and answer a question about its content. " +
			"Use for web pages and online docs. The fetch has no browser session or cookies, " +
			"so pages behind a login fail — use browser_* for those.",
		Parameters: objectSchema(map[string]any{
			"url":    map[string]any{"type": "string", "description": "Absolute http(s) URL to fetch"},
			"prompt": map[string]any{"type": "string", "description": "What to extract from the page (optional; without it the page text is returned)"},
		}, "url"),
		Call: func(ctx context.Context, actor Actor, args json.RawMessage) (string, error) {
			var in struct {
				URL    string `json:"url"`
				Prompt string `json:"prompt"`
			}
			if err := json.Unmarshal(args, &in); err != nil || strings.TrimSpace(in.URL) == "" {
				return "", fmt.Errorf("url is required")
			}
			return b.fetch(ctx, in.URL, in.Prompt)
		},
	})
}

func (b *WebBinder) fetch(ctx context.Context, rawURL, prompt string) (string, error) {
	u, err := parseWebURL(rawURL)
	if err != nil {
		return "", err
	}
	fetchCtx, cancel := context.WithTimeout(ctx, webFetchTimeout)
	defer cancel()
	body, contentType, err := b.get(fetchCtx, u)
	if err != nil {
		return "", err
	}
	content, err := webContentToText(u, contentType, body)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(prompt) == "" {
		return truncateRunes(strings.TrimSpace(content), maxWebResult), nil
	}
	return b.extract(ctx, prompt, content)
}

func parseWebURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) > maxWebURLLen {
		return nil, fmt.Errorf("url is too long")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid url")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("url must use http or https")
	}
	if u.Host == "" {
		return nil, fmt.Errorf("invalid url")
	}
	if u.User != nil {
		return nil, fmt.Errorf("url must not contain credentials")
	}
	return u, nil
}

func (b *WebBinder) get(ctx context.Context, u *url.URL) ([]byte, string, error) {
	if b.HTTP == nil {
		return nil, "", fmt.Errorf("web fetch is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", fmt.Errorf("invalid url")
	}
	req.Header.Set("Accept", "text/markdown, text/html, */*")
	req.Header.Set("User-Agent", webUserAgent)
	resp, err := b.HTTP.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, "", fmt.Errorf("fetch failed: HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxWebBodyBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("fetch failed: %w", err)
	}
	if len(body) > maxWebBodyBytes {
		return nil, "", fmt.Errorf("fetch failed: response exceeds 10MB")
	}
	return body, resp.Header.Get("Content-Type"), nil
}

func webContentToText(u *url.URL, contentType string, body []byte) (string, error) {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch {
	case ct == "text/html", ct == "application/xhtml+xml":
		// 库的 domain 参数只接受主机名：DefaultGetAbsoluteURL 直接把它赋给 u.Host，
		// 且对相对链接把 scheme 兜底成 http。因此这里传空 domain，改用 GetAbsoluteURL
		// 以抓取到的页面 URL 为基准解析相对链接，保留原 scheme（https 页面不被降级）。
		conv := md.NewConverter("", true, &md.Options{
			GetAbsoluteURL: func(_ *goquery.Selection, rawURL string, _ string) string {
				ref, err := url.Parse(rawURL)
				if err != nil {
					return rawURL
				}
				return u.ResolveReference(ref).String()
			},
		})
		out, err := conv.ConvertString(string(body))
		if err != nil {
			return "", fmt.Errorf("convert page: %w", err)
		}
		return strings.TrimSpace(out), nil
	case strings.HasPrefix(ct, "text/"), ct == "application/json", ct == "application/xml", ct == "":
		return string(body), nil
	default:
		return "", fmt.Errorf("unsupported content type %q", ct)
	}
}

func (b *WebBinder) extract(ctx context.Context, prompt, content string) (string, error) {
	if b.Model == nil {
		return "", fmt.Errorf("web fetch extraction is not configured")
	}
	if len(content) > maxWebMarkdown {
		content = truncateRunes(content, maxWebMarkdown) + "\n\n[Content truncated due to length]"
	}
	ctx, cancel := context.WithTimeout(ctx, webModelTimeout)
	defer cancel()
	user := "Web page content:\n---\n" + content + "\n---\n\n" + prompt +
		"\n\nProvide a concise response based only on the content above. Include relevant details and code examples as needed."
	out, err := b.Model.Run(ctx, "", user)
	if err != nil {
		return "", fmt.Errorf("web fetch extraction failed: %w", err)
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return "No response from model", nil
	}
	return truncateRunes(out, maxWebResult), nil
}
