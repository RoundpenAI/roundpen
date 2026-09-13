# System Agent WebFetch / WebSearch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 给 System Agent 注册 `WebFetch`（控制面抓取 URL → HTML 转 Markdown → 经 llmgw 小模型按 prompt 提炼）与 `WebSearch`（Tavily 搜索）两个只读工具，带 SSRF 出站防护。

**Architecture:** 两个工具注册在 `internal/acp/sysagent/tools`，抓取与搜索全部在控制面进程内完成（不经过 guest）。出站 client 用 `net.Dialer.Control` 做拨号级 IP 校验；WebFetch 的二次提炼经 tools 包定义的 `ModelRunner` 接口调用 llmgw（实现挂在 `sysagent.LLMConfig` 上，manager 组装时透传）。`WebSearch` 仅在配置了 endpoint/key 时注册。

**Tech Stack:** Go 1.26（net/http、net.Dialer.Control、httptest）、`github.com/JohannesKaufmann/html-to-markdown` v1.6.0、llmgw（OpenAI 兼容 chat）、Tavily Search API。

**Spec:** `docs/superpowers/specs/2026-09-13-webfetch-websearch-tools-design.md`

**分支:** 在 `feat/agent-upgrade` 上执行（设计文档已提交于 `5a9b5c3`）。

**前置命令（每个任务开始前确保）：**

```bash
git branch --show-current   # 期望 feat/agent-upgrade
```

**参考实现（claude-code 源码，只读参考）：**
- `~/dev/claude-code/packages/builtin-tools/src/tools/WebFetchTool/`
- `~/dev/claude-code/packages/builtin-tools/src/tools/WebSearchTool/`

---

### Task 1: SSRF 防护出站 client

**Files:**
- Create: `internal/acp/sysagent/tools/webclient.go`
- Test: `internal/acp/sysagent/tools/webclient_test.go`

- [x] **Step 1: 写失败测试**

创建 `internal/acp/sysagent/tools/webclient_test.go`（package `tools`，可测未导出函数）：

```go
package tools

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBlockedDialAddr(t *testing.T) {
	cases := []struct {
		addr  string
		block bool
	}{
		{"127.0.0.1:80", true},
		{"[::1]:80", true},
		{"[::ffff:127.0.0.1]:80", true},
		{"169.254.169.254:80", true},
		{"[fe80::1]:80", true},
		{"[::ffff:169.254.169.254]:80", true},
		{"0.0.0.0:80", true},
		{"[::]:80", true},
		{"224.0.0.1:80", true},
		{"[ff02::1]:80", true},
		{"example.com:443", true},
		{"[fe80::1%eth0]:80", true},
		{"10.1.2.3:80", false},
		{"172.16.9.9:80", false},
		{"192.168.1.10:443", false},
		{"8.8.8.8:443", false},
		{"[2606:4700:4700::1111]:443", false},
	}
	for _, tc := range cases {
		err := blockedDialAddr(tc.addr, false)
		if tc.block && err == nil {
			t.Errorf("%s: expected blocked", tc.addr)
		}
		if !tc.block && err != nil {
			t.Errorf("%s: unexpected block: %v", tc.addr, err)
		}
	}
}

func TestBlockedDialAddrAllowLoopback(t *testing.T) {
	if err := blockedDialAddr("127.0.0.1:80", true); err != nil {
		t.Fatalf("loopback should be allowed when opted in: %v", err)
	}
	if err := blockedDialAddr("169.254.169.254:80", true); err == nil {
		t.Fatal("metadata address must stay blocked even with AllowLoopback")
	}
}

func TestWebHTTPClientBlocksLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	client := NewWebHTTPClient(WebClientOptions{})
	resp, err := client.Get(srv.URL)
	if err == nil {
		resp.Body.Close()
		t.Fatal("expected loopback request to be blocked")
	}
}

func TestWebHTTPClientAllowsLoopbackForTests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	client := NewWebHTTPClient(WebClientOptions{AllowLoopback: true})
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Fatalf("body = %q", body)
	}
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `go test ./internal/acp/sysagent/tools/ -run 'TestBlockedDialAddr|TestWebHTTPClient' -count=1`
Expected: FAIL（`undefined: blockedDialAddr`、`undefined: NewWebHTTPClient`）

- [x] **Step 3: 实现**

创建 `internal/acp/sysagent/tools/webclient.go`：

```go
package tools

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"
)

// WebClientOptions 控制 WebFetch / WebSearch 出站 client 的防护行为。
type WebClientOptions struct {
	// AllowLoopback 仅供测试使用（httptest 监听 127.0.0.1）；生产恒为 false。
	AllowLoopback bool
}

// NewWebHTTPClient 返回带 SSRF 防护的 HTTP client：在 DNS 解析之后、建立连接之前
// 校验实际拨号地址，拒绝环回、link-local（含云元数据 169.254.169.254）、
// unspecified 与 multicast；内网网段与公网放行。
func NewWebHTTPClient(opts WebClientOptions) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	dialer.Control = func(network, address string, _ syscall.RawConn) error {
		return blockedDialAddr(address, opts.AllowLoopback)
	}
	return &http.Client{
		Transport: &http.Transport{
			DialContext:           dialer.DialContext,
			MaxIdleConns:          8,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}
}

// blockedDialAddr 校验拨号地址（形如 "1.2.3.4:443" 或 "[::1]:443"）。
func blockedDialAddr(address string, allowLoopback bool) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = strings.Trim(address, "[]")
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("address %q is not allowed", address)
	}
	if ip.IsUnspecified() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return fmt.Errorf("address %s is not allowed", ip)
	}
	if ip.IsLoopback() && !allowLoopback {
		return fmt.Errorf("address %s is not allowed", ip)
	}
	return nil
}
```

- [x] **Step 4: 运行测试确认通过**

Run: `go test ./internal/acp/sysagent/tools/ -run 'TestBlockedDialAddr|TestWebHTTPClient' -count=1 -v`
Expected: 4 个测试全部 PASS

- [x] **Step 5: 提交**

```bash
git add internal/acp/sysagent/tools/webclient.go internal/acp/sysagent/tools/webclient_test.go
git commit -m "feat(tools): add SSRF-guarded outbound HTTP client"
```

---

### Task 2: WebFetch 抓取管线

**Files:**
- Create: `internal/acp/sysagent/tools/web.go`
- Test: `internal/acp/sysagent/tools/web_test.go`
- Modify: `go.mod` / `go.sum`（新增 html-to-markdown 依赖）

- [x] **Step 1: 引入依赖**

```bash
go get github.com/JohannesKaufmann/html-to-markdown@v1.6.0
go mod tidy
```
Expected: `go.mod` 的 require 块新增 `github.com/JohannesKaufmann/html-to-markdown v1.6.0`（GOPROXY=goproxy.cn 已实测可拉取）。

- [x] **Step 2: 写失败测试**

创建 `internal/acp/sysagent/tools/web_test.go`（package `tools_test`；`callTool` / `stubModel` 定义在本文件，Task 4 复用）：

```go
package tools_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

type stubModel struct {
	gotSystem string
	gotUser   string
	reply     string
	err       error
}

func (s *stubModel) Run(_ context.Context, system, user string) (string, error) {
	s.gotSystem = system
	s.gotUser = user
	return s.reply, s.err
}

func callTool(t *testing.T, reg *tools.Registry, name string, args map[string]any) (string, error) {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	return reg.Call(context.Background(), tools.Actor{Username: "u", Role: "user"}, name, raw)
}

func newWebRegistry(m tools.ModelRunner) *tools.Registry {
	reg := tools.NewRegistry()
	tools.RegisterWebFetch(reg, &tools.WebBinder{
		HTTP:  tools.NewWebHTTPClient(tools.WebClientOptions{AllowLoopback: true}),
		Model: m,
	})
	return reg
}

func htmlServer(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", contentType)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestWebFetchConvertsHTML(t *testing.T) {
	var gotAccept, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAccept = r.Header.Get("Accept")
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<html><body><h1>Hello</h1><p>World <a href="/docs">docs</a> and <a href="https://example.com/secure">secure</a></p><script>var x=1</script></body></html>`))
	}))
	defer srv.Close()

	reg := newWebRegistry(nil)
	out, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL + "/page"})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	if gotAccept != "text/markdown, text/html, */*" {
		t.Fatalf("Accept = %q", gotAccept)
	}
	if gotUA != "Roundpen-WebFetch/0.1" {
		t.Fatalf("User-Agent = %q", gotUA)
	}
	if !strings.Contains(out, "# Hello") {
		t.Fatalf("missing markdown heading: %q", out)
	}
	if !strings.Contains(out, srv.URL+"/docs") {
		t.Fatalf("relative link not absolutized: %q", out)
	}
	if !strings.Contains(out, "https://example.com/secure") {
		t.Fatalf("absolute https link not preserved: %q", out)
	}
	if strings.Contains(out, "var x=1") {
		t.Fatalf("script content leaked: %q", out)
	}
}

func TestWebFetchPassesThroughJSON(t *testing.T) {
	srv := htmlServer(t, "application/json", `{"a":1}`)
	reg := newWebRegistry(nil)
	out, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	if out != `{"a":1}` {
		t.Fatalf("out = %q", out)
	}
}

func TestWebFetchRejectsUnsupportedContentType(t *testing.T) {
	srv := htmlServer(t, "application/pdf", "%PDF-1.4")
	reg := newWebRegistry(nil)
	_, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL})
	if err == nil || !strings.Contains(err.Error(), "unsupported content type") {
		t.Fatalf("err = %v", err)
	}
}

func TestWebFetchRejectsNonHTTPURL(t *testing.T) {
	reg := newWebRegistry(nil)
	_, err := callTool(t, reg, "WebFetch", map[string]any{"url": "file:///etc/passwd"})
	if err == nil || !strings.Contains(err.Error(), "http") {
		t.Fatalf("err = %v", err)
	}
}

func TestWebFetchRequiresURL(t *testing.T) {
	reg := newWebRegistry(nil)
	_, err := callTool(t, reg, "WebFetch", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "url is required") {
		t.Fatalf("err = %v", err)
	}
}

func TestWebFetchReportsBadStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	reg := newWebRegistry(nil)
	_, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL})
	if err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("err = %v", err)
	}
}

func TestWebFetchTruncatesLongContent(t *testing.T) {
	srv := htmlServer(t, "text/plain", strings.Repeat("x", 200_000))
	reg := newWebRegistry(nil)
	out, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	if len(out) > (32<<10)+8 {
		t.Fatalf("result too long: %d bytes", len(out))
	}
	if !strings.HasPrefix(out, "xxx") {
		t.Fatalf("truncated output must keep the start of the page: %q", out[:min(20, len(out))])
	}
	if !strings.HasSuffix(out, "…") {
		t.Fatalf("truncated output must end with the ellipsis marker: %q", out[len(out)-min(20, len(out)):])
	}
}

func TestWebFetchRejectsOversizeBody(t *testing.T) {
	srv := htmlServer(t, "text/plain", strings.Repeat("x", (10<<20)+1))
	reg := newWebRegistry(nil)
	_, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL})
	if err == nil || !strings.Contains(err.Error(), "10MB") {
		t.Fatalf("err = %v", err)
	}
}

func TestWebFetchRejectsCredentialURL(t *testing.T) {
	reg := newWebRegistry(nil)
	_, err := callTool(t, reg, "WebFetch", map[string]any{"url": "http://user:pass@example.com/"})
	if err == nil || !strings.Contains(err.Error(), "credentials") {
		t.Fatalf("err = %v", err)
	}
}

func TestWebFetchErrorsWhenPromptGivenWithoutModel(t *testing.T) {
	srv := htmlServer(t, "text/html", "<p>hi</p>")
	reg := newWebRegistry(nil)
	_, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL, "prompt": "what?"})
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("err = %v", err)
	}
}
```

- [x] **Step 3: 运行测试确认失败**

Run: `go test ./internal/acp/sysagent/tools/ -run TestWebFetch -count=1`
Expected: FAIL（`undefined: tools.RegisterWebFetch`、`undefined: tools.WebBinder`）

- [x] **Step 4: 实现抓取管线**

创建 `internal/acp/sysagent/tools/web.go`：

```go
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

// extract 在 Task 3 实现；此处先返回未配置错误，保证 Task 2 的测试可编译。
func (b *WebBinder) extract(ctx context.Context, prompt, content string) (string, error) {
	return "", fmt.Errorf("web fetch extraction is not configured")
}
```

- [x] **Step 5: 运行测试确认通过**

Run: `go test ./internal/acp/sysagent/tools/ -run TestWebFetch -count=1 -v`
Expected: 全部 PASS（含 `TestWebFetchErrorsWhenPromptGivenWithoutModel`）

- [x] **Step 6: 提交**

```bash
git add go.mod go.sum internal/acp/sysagent/tools/web.go internal/acp/sysagent/tools/web_test.go
git commit -m "feat(tools): add WebFetch fetch and markdown pipeline"
```

---

### Task 3: WebFetch 二次提炼（llmgw）

**Files:**
- Modify: `internal/acp/sysagent/tools/web.go`（替换 Task 2 的 `extract` 占位实现）
- Test: `internal/acp/sysagent/tools/web_test.go`（追加测试）

- [x] **Step 1: 写失败测试**

在 `internal/acp/sysagent/tools/web_test.go` 末尾追加，并在 import 块补上 `"errors"`：

```go
func TestWebFetchUsesModelWhenPromptGiven(t *testing.T) {
	srv := htmlServer(t, "text/html", "<p>the page says blue</p>")
	m := &stubModel{reply: "The answer is blue."}
	reg := newWebRegistry(m)
	out, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL, "prompt": "What color?"})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	if out != "The answer is blue." {
		t.Fatalf("out = %q", out)
	}
	if !strings.Contains(m.gotUser, "the page says blue") {
		t.Fatalf("model did not receive page content: %q", m.gotUser)
	}
	if !strings.Contains(m.gotUser, "What color?") {
		t.Fatalf("model did not receive prompt: %q", m.gotUser)
	}
}

func TestWebFetchWithoutPromptSkipsModel(t *testing.T) {
	srv := htmlServer(t, "text/html", "<p>plain page</p>")
	m := &stubModel{reply: "should not be used"}
	reg := newWebRegistry(m)
	out, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL})
	if err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	if !strings.Contains(out, "plain page") {
		t.Fatalf("out = %q", out)
	}
	if m.gotUser != "" {
		t.Fatalf("model must not be called without prompt, got %q", m.gotUser)
	}
}

func TestWebFetchTruncatesModelInput(t *testing.T) {
	srv := htmlServer(t, "text/plain", strings.Repeat("y", 150_000))
	m := &stubModel{reply: "ok"}
	reg := newWebRegistry(m)
	if _, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL, "prompt": "summarize"}); err != nil {
		t.Fatalf("WebFetch: %v", err)
	}
	if !strings.Contains(m.gotUser, "[Content truncated due to length]") {
		t.Fatalf("model input not truncated: %d bytes", len(m.gotUser))
	}
	if len(m.gotUser) > 200_000 {
		t.Fatalf("model input too large: %d bytes", len(m.gotUser))
	}
}

func TestWebFetchModelErrorPropagates(t *testing.T) {
	srv := htmlServer(t, "text/html", "<p>hi</p>")
	m := &stubModel{err: errors.New("boom")}
	reg := newWebRegistry(m)
	_, err := callTool(t, reg, "WebFetch", map[string]any{"url": srv.URL, "prompt": "what?"})
	if err == nil || !strings.Contains(err.Error(), "extraction failed") {
		t.Fatalf("err = %v", err)
	}
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `go test ./internal/acp/sysagent/tools/ -run 'TestWebFetchUsesModel|TestWebFetchWithoutPromptSkipsModel|TestWebFetchTruncatesModelInput|TestWebFetchModelError' -count=1`
Expected: FAIL（`extraction is not configured`，模型未被调用）

- [x] **Step 3: 实现 extract**

把 `internal/acp/sysagent/tools/web.go` 中的占位实现替换为：

```go
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
```

- [x] **Step 4: 运行测试确认通过**

Run: `go test ./internal/acp/sysagent/tools/ -count=1`
Expected: 全部 PASS（含 Task 1、2 的测试）

- [x] **Step 5: 提交**

```bash
git add internal/acp/sysagent/tools/web.go internal/acp/sysagent/tools/web_test.go
git commit -m "feat(tools): extract WebFetch pages via model prompt"
```

---


> 评审补充（已实现）：`truncateRunes` 的截断边界改为按 rune 起点判断——原实现用整段前缀合法性，遇到非法 UTF-8 字节会把输出塌缩到该字节（150KB → 6B）。新增 `truncate_test.go`；`web_test.go` 追加空模型输出、32KB 输出上限与空 system prompt 断言。

### Task 4: WebSearch（Tavily）

**Files:**
- Create: `internal/acp/sysagent/tools/websearch.go`
- Test: `internal/acp/sysagent/tools/websearch_test.go`

- [x] **Step 1: 写失败测试**

创建 `internal/acp/sysagent/tools/websearch_test.go`（package `tools_test`，复用 Task 2 的 `callTool`）：

```go
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

func TestWebSearchTransportErrorHidesEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close() // 关闭监听后请求必然传输失败
	reg := newSearchRegistry(t, srv.URL, "k")
	_, err := callTool(t, reg, "WebSearch", map[string]any{"query": "golang"})
	if err == nil || !strings.Contains(err.Error(), "web search failed") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), srv.URL) {
		t.Fatalf("error must not echo the endpoint URL: %v", err)
	}
}

func TestWebSearchRejectsOversizeBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(strings.Repeat("x", (10<<20)+1)))
	}))
	defer srv.Close()
	reg := newSearchRegistry(t, srv.URL, "k")
	_, err := callTool(t, reg, "WebSearch", map[string]any{"query": "golang"})
	if err == nil || !strings.Contains(err.Error(), "10MB") {
		t.Fatalf("err = %v", err)
	}
}

func TestWebSearchKeepsSourcesReminderAfterTruncation(t *testing.T) {
	big := strings.Repeat("s", 5000)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		results := make([]map[string]any, 0, 20)
		for i := 0; i < 20; i++ {
			results = append(results, map[string]any{
				"title": "T", "url": "https://example.com/x", "content": big,
			})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"results": results})
	}))
	defer srv.Close()
	reg := newSearchRegistry(t, srv.URL, "k")
	out, err := callTool(t, reg, "WebSearch", map[string]any{"query": "golang"})
	if err != nil {
		t.Fatalf("WebSearch: %v", err)
	}
	if !strings.HasSuffix(out, "Include the sources above in your response as markdown links.") {
		t.Fatalf("sources reminder lost after truncation, tail = %q", out[len(out)-60:])
	}
}

func TestWebSearchOmitsAuthHeaderWithoutKey(t *testing.T) {
	authSet := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, authSet = r.Header["Authorization"]
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}})
	}))
	defer srv.Close()
	reg := newSearchRegistry(t, srv.URL, "")
	if _, err := callTool(t, reg, "WebSearch", map[string]any{"query": "golang"}); err != nil {
		t.Fatalf("WebSearch: %v", err)
	}
	if authSet {
		t.Fatal("Authorization header must be absent when no API key is configured")
	}
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `go test ./internal/acp/sysagent/tools/ -run TestWebSearch -count=1`
Expected: FAIL（`undefined: tools.WebSearchBinder`、`undefined: tools.RegisterWebSearch`）

- [x] **Step 3: 实现**

创建 `internal/acp/sysagent/tools/websearch.go`：

```go
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
```

- [x] **Step 4: 运行测试确认通过**

Run: `go test ./internal/acp/sysagent/tools/ -count=1`
Expected: 全部 PASS

- [x] **Step 5: 提交**

```bash
git add internal/acp/sysagent/tools/websearch.go internal/acp/sysagent/tools/websearch_test.go
git commit -m "feat(tools): add Tavily-backed WebSearch tool"
```

---


> 评审补充（已实现）：传输错误解包 `*url.Error`，不回显 endpoint；响应体超 10MB 报错（与 WebFetch 对齐）；来源提醒置于 32KB 截断之外；标题/URL 折叠空白。追加 4 个测试覆盖以上行为。

### Task 5: Tavily 配置（env）

**Files:**
- Create: `internal/config/webtools.go`
- Modify: `internal/config/config.go`（Config 结构体 + Load）
- Test: `internal/config/webtools_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/config/webtools_test.go`（与既有 config 测试一致，直接测子加载器，不耦合 `Load()` 的其余环境依赖）：

```go
package config

import "testing"

func TestLoadWebToolsEmpty(t *testing.T) {
	t.Setenv("ROUNDPEN_WEB_SEARCH_ENDPOINT", "")
	t.Setenv("ROUNDPEN_WEB_SEARCH_API_KEY", "")
	cfg := loadWebTools()
	if cfg.SearchEndpoint != "" || cfg.SearchAPIKey != "" {
		t.Fatalf("expected empty web tools config, got %+v", cfg)
	}
}

func TestLoadWebToolsFromEnv(t *testing.T) {
	t.Setenv("ROUNDPEN_WEB_SEARCH_ENDPOINT", "https://search.internal.example/")
	t.Setenv("ROUNDPEN_WEB_SEARCH_API_KEY", " tvly-x ")
	cfg := loadWebTools()
	if cfg.SearchEndpoint != "https://search.internal.example/" {
		t.Fatalf("endpoint = %q", cfg.SearchEndpoint)
	}
	if cfg.SearchAPIKey != "tvly-x" {
		t.Fatalf("key = %q", cfg.SearchAPIKey)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/config/ -run TestLoadWebTools -count=1`
Expected: FAIL（`cfg.WebTools undefined`）

- [ ] **Step 3: 实现**

创建 `internal/config/webtools.go`：

```go
package config

import (
	"os"
	"strings"
)

// WebToolsConfig configures the optional web tools (WebSearch via Tavily).
// endpoint 与 key 都为空时不注册 WebSearch。
type WebToolsConfig struct {
	SearchEndpoint string // ROUNDPEN_WEB_SEARCH_ENDPOINT；空 = 默认 https://api.tavily.com
	SearchAPIKey   string // ROUNDPEN_WEB_SEARCH_API_KEY
}

func loadWebTools() WebToolsConfig {
	return WebToolsConfig{
		SearchEndpoint: strings.TrimSpace(os.Getenv("ROUNDPEN_WEB_SEARCH_ENDPOINT")),
		SearchAPIKey:   strings.TrimSpace(os.Getenv("ROUNDPEN_WEB_SEARCH_API_KEY")),
	}
}
```

在 `internal/config/config.go` 的 `Config` 结构体中，`LLMGW LLMGWConfig` 一行之后加入：

```go
	WebTools                WebToolsConfig
```

并在 `Load()` 中 `cfg.LLMGW = llmgwCfg` 之后加入：

```go
	cfg.WebTools = loadWebTools()
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/config/ -count=1`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/config/config.go internal/config/webtools.go internal/config/webtools_test.go
git commit -m "feat(config): add ROUNDPEN_WEB_SEARCH_* settings"
```

---

### Task 6: `sysagent.LLMConfig.Run`（ModelRunner 实现）

**Files:**
- Modify: `internal/acp/sysagent/llm.go`
- Test: `internal/acp/sysagent/llm_run_test.go`

- [ ] **Step 1: 写失败测试**

创建 `internal/acp/sysagent/llm_run_test.go`（package `sysagent`）：

```go
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
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/acp/sysagent/ -run TestLLMConfigRun -count=1`
Expected: FAIL（`c.Run undefined`）

- [ ] **Step 3: 实现**

在 `internal/acp/sysagent/llm.go` 的 `func (c LLMConfig) chat(...)` 之后加入：

```go
// Run executes a tool-less chat completion and returns the assistant text.
// It satisfies tools.ModelRunner for WebFetch page extraction.
func (c LLMConfig) Run(ctx context.Context, system, user string) (string, error) {
	msgs := make([]chatMessage, 0, 2)
	if strings.TrimSpace(system) != "" {
		msgs = append(msgs, chatMessage{Role: "system", Content: system})
	}
	msgs = append(msgs, chatMessage{Role: "user", Content: user})
	msg, _, err := c.chat(ctx, msgs, nil)
	if err != nil {
		return "", err
	}
	return msg.Content, nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/acp/sysagent/ -run TestLLMConfigRun -count=1 -v`
Expected: 2 个测试 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/acp/sysagent/llm.go internal/acp/sysagent/llm_run_test.go
git commit -m "feat(sysagent): add tool-less LLMConfig.Run for web fetch extraction"
```

---

### Task 7: 接线（SysDeps / manager / main）

**Files:**
- Modify: `internal/acp/manager/manager.go`（SysDeps 字段 + Start 注册）
- Modify: `cmd/roundpend/main.go:324-332`（透传配置）

- [ ] **Step 1: SysDeps 增加字段**

在 `internal/acp/manager/manager.go` 的 `SysDeps` 结构体中，`AgentSlots tools.AgentSlot` 之后加入：

```go
	WebSearchEndpoint string // Tavily 兼容搜索 endpoint（空 = 默认 https://api.tavily.com）
	WebSearchAPIKey   string // 二者任一非空即注册 WebSearch 工具
```

- [ ] **Step 2: Start 中注册 web 工具**

把 `manager.go` 中 `case "sysadmin", "mock", "":` 分支的开头（`reg := tools.NewRegistry()` 之前）改成先构造 `llmCfg`，并在 `tools.RegisterSearch(reg, binder)` 之后注册 web 工具，最后把 `sysagent.New` 的 `LLM` 换成 `llmCfg`。改完后该段为：

```go
		llmCfg := sysagent.LLMConfig{
			BaseURL:      strings.TrimRight(m.sys.LoopbackBase, "/") + "/llmgw/openai",
			APIKey:       m.sys.LLMKey,
			DefaultModel: m.sys.DefaultModel,
		}
		reg := tools.NewRegistry()
		tools.RegisterRoundpen(reg, &tools.RoundpenHTTP{BaseURL: m.sys.LoopbackBase})
		tools.RegisterBrowser(reg, &tools.BrowserBinder{
			Hub:       m.sys.BrowserHub,
			Slots:     m.sys.BrowserSlots,
			SessionID: sessionID,
		})
		binder := &tools.AgentBinder{
			Slots: m.sys.AgentSlots,
			Exec:  m.sandboxes,
			Files: m.sandboxes,
		}
		tools.RegisterShell(reg, binder)
		tools.RegisterFiles(reg, binder)
		tools.RegisterSearch(reg, binder)
		webClient := tools.NewWebHTTPClient(tools.WebClientOptions{})
		tools.RegisterWebFetch(reg, &tools.WebBinder{HTTP: webClient, Model: llmCfg})
		tools.RegisterWebSearch(reg, &tools.WebSearchBinder{
			Endpoint: m.sys.WebSearchEndpoint,
			APIKey:   m.sys.WebSearchAPIKey,
			HTTP:     webClient,
		})
		agent := sysagent.New(sysagent.Deps{
			LLM:       llmCfg,
			Tools:     reg,
			Actor:     opts.Actor,
			History:   m.sys.History,
			SessionID: sessionID,
		})
```

（`LLMConfig` 的 `chat`/`chatStream` 是值接收者方法，`llmCfg` 值同时满足 `sysagent.Deps.LLM` 与 `tools.ModelRunner`。）

- [ ] **Step 3: main.go 透传**

在 `cmd/roundpend/main.go` 的 `manager.New(...)` 调用里追加两个字段：

```go
	acpMgr := manager.New(logger, mgr, providers.Default(), manager.SysDeps{
		LoopbackBase: loopback,
		LLMKey:       llmgw.InternalVirtualKey,
		DefaultModel: gw.DefaultModel,
		BrowserHub:   browserHub,
		BrowserSlots: envSvc,
		AgentSlots:   envSvc,
		History:      agentStore,

		WebSearchEndpoint: cfg.WebTools.SearchEndpoint,
		WebSearchAPIKey:   cfg.WebTools.SearchAPIKey,
	})
```

- [ ] **Step 4: 构建与既有测试**

Run: `go build ./... && go vet ./internal/acp/... && go test ./internal/acp/... -count=1`
Expected: 构建通过；`internal/acp/manager` 与 `internal/acp/sysagent` 既有测试全部 PASS（`SysDeps` 新增零值字段不影响既有构造）。

- [ ] **Step 5: 提交**

```bash
git add internal/acp/manager/manager.go cmd/roundpend/main.go
git commit -m "feat(agent): wire WebFetch and WebSearch into the System Agent"
```

---

### Task 8: 工具面测试 + 系统提示 + 文档

**Files:**
- Modify: `internal/acp/sysagent/tools/registry_surface_test.go`（追加测试）
- Modify: `internal/acp/sysagent/history.go`（系统提示）
- Modify: `internal/acp/sysagent/history_test.go`（追加断言）
- Modify: `docs/architecture/acp-agent-ui.md`（工具表）

- [ ] **Step 1: 扩展工具面测试**

在 `internal/acp/sysagent/tools/registry_surface_test.go` 末尾追加：

```go
func TestWebToolSurface(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterWebFetch(reg, &tools.WebBinder{
		HTTP: tools.NewWebHTTPClient(tools.WebClientOptions{AllowLoopback: true}),
	})
	tools.RegisterWebSearch(reg, &tools.WebSearchBinder{}) // 未配置 → 不注册

	if _, ok := reg.Get("WebFetch"); !ok {
		t.Fatal("missing WebFetch")
	}
	if _, ok := reg.Get("WebSearch"); ok {
		t.Fatal("WebSearch must not register without endpoint/key")
	}

	for _, tool := range reg.List() {
		lower := strings.ToLower(tool.Description)
		for _, bad := range []string{"sandbox", "guest", "qemu"} {
			if strings.Contains(lower, bad) {
				t.Fatalf("%s description mentions %q", tool.Name, bad)
			}
		}
		if tool.Mutating {
			t.Fatalf("%s must be read-only", tool.Name)
		}
	}

	reg2 := tools.NewRegistry()
	tools.RegisterWebSearch(reg2, &tools.WebSearchBinder{APIKey: "tvly-test"})
	search, ok := reg2.Get("WebSearch")
	if !ok {
		t.Fatal("missing WebSearch when configured")
	}
	if search.Mutating {
		t.Fatal("WebSearch must be read-only")
	}
}
```

- [ ] **Step 2: 运行确认通过**

Run: `go test ./internal/acp/sysagent/tools/ -run TestWebToolSurface -count=1 -v`
Expected: PASS

- [ ] **Step 3: 系统提示加行**

在 `internal/acp/sysagent/history.go` 的 `system` 字符串里，`Browser tools (browser_*): Chrome only — cannot run git or shell.` 一行之后插入一行：

```
Web tools: WebFetch reads one URL and answers your question about the page (no browser session or cookies, so login-walled pages fail). WebSearch looks up current information; cite the URLs you used.
```

- [ ] **Step 4: 追加提示词测试**

在 `internal/acp/sysagent/history_test.go` 末尾追加（`strings` 已在导入中，需补 `"context"`）：

```go
func TestSystemPromptMentionsWebTools(t *testing.T) {
	a := New(Deps{})
	msgs := a.buildPromptMessages(context.Background(), "hi")
	if len(msgs) == 0 || msgs[0].Role != "system" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}
	for _, want := range []string{"WebFetch", "WebSearch"} {
		if !strings.Contains(msgs[0].Content, want) {
			t.Fatalf("system prompt missing %q", want)
		}
	}
}
```

- [ ] **Step 5: 运行确认通过**

Run: `go test ./internal/acp/sysagent/ -run 'TestSystemPromptMentionsWebTools|TestProjectHistory|TestCompactHistory' -count=1`
Expected: PASS

- [ ] **Step 6: 更新架构文档工具表**

在 `docs/architecture/acp-agent-ui.md` 的工具表中，`Bash` 一行之后插入：

```
| `WebFetch` | 抓取 URL 转 Markdown 并按 prompt 提炼（控制面执行，只读） |
| `WebSearch` | 联网搜索（Tavily；`ROUNDPEN_WEB_SEARCH_*` 未配置则不注册） |
```

注：`web/src/lib/toolStats.ts` 已按名支持 `webfetch`/`websearch`（分别归入 explore/search 分类，见 `classifyTool` 的正则），无需改动。

- [ ] **Step 7: 提交**

```bash
git add internal/acp/sysagent/tools/registry_surface_test.go internal/acp/sysagent/history.go internal/acp/sysagent/history_test.go docs/architecture/acp-agent-ui.md
git commit -m "docs(agent): surface WebFetch/WebSearch in prompt, tests, and tool table"
```

---

### Task 9: 实网验证与全量回归

**Files:**
- Create: `internal/acp/sysagent/tools/web_live_test.go`

- [ ] **Step 1: 写 env-gated 实网测试**

创建 `internal/acp/sysagent/tools/web_live_test.go`（未设置环境变量时自动 skip，CI 保持离线）：

```go
package tools_test

import (
	"os"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

func TestWebFetchLive(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
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
```

- [ ] **Step 2: 运行实网测试（需 Key，仅本地执行）**

Run:
```bash
ROUNDPEN_WEB_SEARCH_API_KEY=<你的 Tavily Key> \
go test ./internal/acp/sysagent/tools/ -run 'TestWebFetchLive|TestWebSearchLive' -v -count=1
```
Expected: 两个测试 PASS（WebFetch 抓 go.dev；WebSearch 返回 3 条结果）。Key 只经环境变量注入，不得写入任何提交文件。

- [ ] **Step 3: 全量回归**

Run: `go vet ./... && go test ./... -count=1`
Expected: 全部 PASS（无 env 的测试自动 skip）

- [ ] **Step 4: 提交**

```bash
git add internal/acp/sysagent/tools/web_live_test.go
git commit -m "test(tools): add env-gated live checks for web tools"
```

- [ ] **Step 5: 端到端手测（daemon + UI）**

```bash
# 在 .env 追加（.env 已被 gitignore，不会入库）
# ROUNDPEN_WEB_SEARCH_ENDPOINT=https://api.tavily.com
# ROUNDPEN_WEB_SEARCH_API_KEY=<你的 Tavily Key>
make dev
```

在 Agent 会话里让模型执行：
1. `WebSearch`：查一条近期新闻，确认返回带链接的来源列表且答案含引用；
2. `WebFetch`：抓 `https://go.dev/`（无 prompt，返回正文；带 prompt，返回提炼答案）；
3. 反例：`WebFetch` 抓 `http://169.254.169.254/` 与 `http://localhost:19001/`，确认被拒（"address ... is not allowed"）；
4. 确认设置页/会话 UI 中两个工具都显示为只读、无权限弹窗。

Expected: 1-2 正常返回；3 被拦截；4 无权限询问。

---

## 完成标准（对照 spec）

1. `WebFetch` 与 `WebSearch` 在 System Agent 工具面注册，名称与描述符合语言规则（无 sandbox/guest/QEMU），均为只读。
2. `WebFetch` 完成"校验 → 抓取 → HTML 转 Markdown → 100K 截断 → 经 llmgw 二次提炼"；无 prompt 时返回正文（32KB 截断）。
3. `WebSearch` 走 Tavily（Bearer 鉴权，endpoint/key 可配），未配置时不注册。
4. 出站防护拦截环回 / link-local / 元数据 / unspecified / multicast；内网与公网放行；重定向逐跳校验。
5. 全量 `go test ./...` 通过；env-gated 实网测试在提供 Key 时通过。
