# Browser 环境迁移到 Docker + Playwright 引擎 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 Browser 槽位从「每用户一台 QEMU VM」迁移为「每用户一个 Roundpen 托管的 browserless/chrome 容器」，控制面引擎从 chromedp 换成 playwright-go，并把浏览器来源做成可插拔（托管容器 / 局域网自建 / 商业云 / 本机 Chrome）。

**Architecture:** `internal/browser` 的 `Engine` 接口不变，新增 Playwright 实现（`ConnectOverCDP` 接托管/远程/云，`Launch` 接本机 Chrome）；托管来源由 `userenv.EnsureBrowser` 保证一个 Docker 沙箱（模板 `browser`，镜像 `ghcr.io/browserless/chrome:v2.56.7`，容器内 3000 端口经 `Backend.Dial` + 本地 TCP 转发暴露 CDP）；实时视图用 browserless 自带 debugger，经 `/v1/me/environments/browser/live/` 前缀反代并注入容器 token。

**Tech Stack:** Go 1.25、`github.com/mxschmitt/playwright-go v0.6201.1`（Playwright driver 1.62.1）、Docker（browserless v2）、React 19 + Semi UI。

**Spec:** `docs/superpowers/specs/2026-09-13-browser-docker-playwright-design.md`

---

## 已完成的 spike（2026-09-13，事实，勿再验证）

1. **引擎映射验证通过**（`ws://10.10.1.3:3000/chrome`，Chrome 153）：`ConnectOverCDP` 353ms；把现有 `snapshotJS` 注入脚本原样 `page.Evaluate` 可用（`data-rp-ref` refs 正常）；`Locator("[data-rp-ref=…]").Fill/Click` 可用；`Screenshot`、`Mouse().Click/Move/Wheel`、`Keyboard().Press/Type`、`Evaluate` 全部可用。
2. **debugger 前缀反代可用**：把 `/live/*` 反代到 `http://10.10.1.3:3000/debugger/*`，页面标题、正文、控制台输出与直连完全一致，无 404、无失败请求（仅字体解码告警，直连同样存在）。
3. **playwright-go API 事实**（v0.6201.1）：
   - 模块路径是 **`github.com/mxschmitt/playwright-go`**（go.mod 里 `module github.com/mxschmitt/playwright-go`；`playwright-community/...` 路径会报 module path 不匹配）。
   - `browser.Version() string`（单返回值）；`page.Mouse()` / `page.Keyboard()` 是**方法**不是字段；`locator.Fill(value string) error`（单返回值）。
   - driver 需显式安装：`playwright.Install(&playwright.RunOptions{SkipInstallBrowsers: true})` —— 只下 driver 不下载浏览器，首次约 14s，重复调用 0.8s（幂等）。未安装时 `playwright.Run()` 报 `please install the driver (v1.62.1) first`。
   - driver 支持 `PLAYWRIGHT_DRIVER_PATH`（driver 目录）、`PLAYWRIGHT_NODEJS_PATH`（用系统 node）、`PLAYWRIGHT_GO_NPM_REGISTRY`（npm 镜像）。
4. **连接失败报错文本**（用于重试匹配）：
   - 端口不通：`connect ECONNREFUSED 127.0.0.1:39999`
   - ws 路径不存在/未就绪：`WebSocket was closed before the connection was established`
5. **browserless v2 端点事实**：CDP ws 路径 `chrome` 镜像为 `/chrome`（`/`、`/chromium` → 404）；`GET /meta` 返回 `{"version":"2.56.7","playwright":[...],"puppeteer":[...]}`；`/json/version` 的 `webSocketDebuggerUrl` 是 bind 地址（`ws://0.0.0.0:3000/`），**不可用于发现**。

---

## 文件结构

**新建**

| 文件 | 职责 |
|---|---|
| `internal/browser/playwright.go` | Playwright driver 单例 + `PlaywrightEngine`（Engine 接口实现）|
| `internal/browser/cdp.go` | endpoint 解析、候选路径探测（`/chrome`→`/chromium`→`/`）、`ProbeCDP`（给设置页测试连接用）|
| `internal/browser/dial.go` | `PortDialer` 接口 + 本地 TCP 转发 `startCDPProxy`（从 `remote.go` 迁出）|
| `internal/browser/snapshot.go` | `snapshotJS`、`desktopChromeUA`、`stealthInitJS`（从 `chrome.go` 迁出）|
| `cmd/browserdriver/main.go` | 只装 Playwright driver 的 CLI（Makefile / Dockerfile 用）|
| `internal/browser/playwright_test.go` | 引擎 gated 集成测试（`ROUNDPEN_TEST_BROWSER_WS`）|
| `internal/browser/cdp_test.go` | 候选路径 / URL 拼装 / token 转义单测 |
| `internal/api/envapi/live_test.go` | live 反代路径映射与 token 注入测试（httptest 假上游）|

**修改**

| 文件 | 改动 |
|---|---|
| `internal/browser/hub.go` | `attach()` 换 Playwright；持有 `pwDriver`；`Close()` 停 driver |
| `internal/config/cdp.go`、`config.go` | 端口默认 3000；`BrowserImage`/`DefaultBrowserTemplate` 默认值；hint 文案 |
| `internal/userenv/userenv.go` | `BrowserTarget`、`EnsureBrowser` 返回类型、`EnvView.Provider`、`Cfg *config.Config` |
| `internal/api/envapi/handler.go` | 删 desktop 三件套，加 `live-link` + `live`（前缀反代 + token 注入）|
| `internal/api/agentapi/browser.go`、`tasks.go` | 改用 `BrowserTarget.Key` |
| `internal/acp/sysagent/tools/browser.go` | `BrowserSlot` 接口改签名 |
| `internal/runtime/probe.go` | `RequireBrowser` 按 provider 判定 |
| `internal/settings/http.go`、`service.go` | 新 admin 接口 `POST /v1/admin/settings/browser/test` |
| `internal/template/store.go`、`builtin.go` | 种子 `browser-desktop` → `browser`（OCI 镜像）|
| `internal/api/auth/middleware.go` | 删 desktop ws 白名单 |
| `cmd/roundpend/main.go` | 接线：driver、`envSvc.Cfg`、live 依赖 |
| `deploy/Dockerfile`、`Makefile` | 烘焙 driver / `browser-driver` 目标 |
| `web/src/pages/BrowserPage.tsx`、`api.ts`、`SettingsPage.tsx` | 实时视图入口 / 测试连接按钮 |
| `web/e2e/*`、`tests/uismoke/main.go` | 删 VNC 用例与 RFB 桩 |

**删除**：`internal/browser/chrome.go`、`internal/browser/remote.go`、`internal/rfbtest/`、`images/browser-qemu/`、`web/vnc.html`、`web/src/vnc-client.ts`、`web/src/lib/novnc-rfb.ts`、`web/src/novnc-rfb.d.ts`、`web/e2e/vnc.spec.ts`、`docs/architecture/qemu-browser.md`（重写为 `browser-env.md`）。

---

## Phase A — Playwright 引擎

### Task 1: 引入 playwright-go 与 driver 安装

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `cmd/browserdriver/main.go`
- Modify: `Makefile`

- [ ] **Step 1: 加依赖**

```bash
cd /home/mike/dev/sandbox/roundpen
go get github.com/mxschmitt/playwright-go@v0.6201.1
```

注意：模块声明路径是 `github.com/mxschmitt/playwright-go`。不要用 `playwright-community/...`（`go mod tidy` 会报 `module declares its path as` 而失败）。

- [ ] **Step 2: 写 driver 安装 CLI**

```go
// cmd/browserdriver/main.go
//
// Installs the Playwright driver (no browsers) for local dev and image builds.
package main

import (
	"fmt"
	"os"

	"github.com/mxschmitt/playwright-go"
)

func main() {
	err := playwright.Install(&playwright.RunOptions{
		SkipInstallBrowsers: true,
		Verbose:             true,
		DriverDirectory:     os.Getenv("PLAYWRIGHT_DRIVER_PATH"),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "playwright driver install:", err)
		os.Exit(1)
	}
	fmt.Println("playwright driver installed")
}
```

- [ ] **Step 3: Makefile 加目标**

```makefile
# Playwright driver (no browsers: the browser runs in the Browser env container).
browser-driver:
	go run ./cmd/browserdriver
```

放在 `browser-image` 目标原位置（该目标稍后删除）。

- [ ] **Step 4: 验证**

```bash
go run ./cmd/browserdriver
# 期望：playwright driver installed（首次约 14s，重复 1s 内）
go build ./...
# 期望：成功
```

- [ ] **Step 5: 提交**

```bash
git add go.mod go.sum cmd/browserdriver Makefile
git commit -m "feat(browser): add playwright-go dependency and driver installer"
```

### Task 2: Playwright Engine 实现

**Files:**
- Create: `internal/browser/snapshot.go`（从 `chrome.go` 迁出常量）
- Create: `internal/browser/playwright.go`
- Create: `internal/browser/cdp.go`
- Test: `internal/browser/cdp_test.go`、`internal/browser/playwright_test.go`

- [ ] **Step 1: 迁出共享常量**

把 `internal/browser/chrome.go` 中这三段**原样**移到新文件 `internal/browser/snapshot.go`（本任务不删 chrome.go）：

- `const snapshotJS = ` 脚本（chrome.go:19-76）
- `// desktopChromeUA ...` + `const desktopChromeUA = ...`（chrome.go:168-169）
- `// stealthInitJS ...` + `const stealthInitJS = `...`（chrome.go:171-189）

新文件头部：

```go
package browser

// Shared page-side scripts and UA used by every engine implementation.
```

同时把 `chrome.go` 里这三个定义删掉（`stealthInitAction()` 留下，它引用 `stealthInitJS`）。

- [ ] **Step 2: 写候选路径与 URL 拼装（先写测试）**

```go
// internal/browser/cdp_test.go
package browser

import "testing"

func TestCandidatePaths(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", []string{"/chrome", "/chromium", "/"}},
		{"/", []string{"/chrome", "/chromium", "/"}},
		{"/chrome", []string{"/chrome"}},
		{"/chromium", []string{"/chromium"}},
		{"/ws/v2", []string{"/ws/v2"}},
	}
	for _, c := range cases {
		got := candidatePaths(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("candidatePaths(%q) = %v, want %v", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("candidatePaths(%q) = %v, want %v", c.in, got, c.want)
			}
		}
	}
}

func TestBuildWSURL(t *testing.T) {
	cases := []struct {
		endpoint string
		path     string
		token    string
		want     string
	}{
		{"http://10.10.1.3:3000", "/chrome", "", "ws://10.10.1.3:3000/chrome"},
		{"https://cloud.example/", "/chrome", "a b&c", "wss://cloud.example/chrome?token=a+b%26c"},
		{"ws://127.0.0.1:1234/chrome", "/chrome", "tok", "ws://127.0.0.1:1234/chrome?token=tok"},
		{"wss://cloud.example/chrome?foo=1", "/chrome", "tok", "wss://cloud.example/chrome?foo=1&token=tok"},
	}
	for _, c := range cases {
		got, err := buildWSURL(c.endpoint, c.path, c.token)
		if err != nil {
			t.Fatalf("buildWSURL(%q, %q, %q): %v", c.endpoint, c.path, c.token, err)
		}
		if got != c.want {
			t.Fatalf("buildWSURL(%q, %q, %q) = %q, want %q", c.endpoint, c.path, c.token, got, c.want)
		}
	}
}
```

- [ ] **Step 3: 跑测试确认失败**

```bash
go test ./internal/browser/ -run 'TestCandidatePaths|TestBuildWSURL' -v
# 期望：编译失败（undefined: candidatePaths / buildWSURL）
```

- [ ] **Step 4: 实现 cdp.go**

```go
// internal/browser/cdp.go
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// candidateWSPaths are tried in order when the endpoint has no explicit path.
// browserless v2: /chrome for the chrome image, /chromium for chromium, / for
// single-browser images. /json/version cannot be used for discovery: it returns
// the server's bind address (ws://0.0.0.0:3000/).
var candidateWSPaths = []string{"/chrome", "/chromium", "/"}

func candidatePaths(endpointPath string) []string {
	p := strings.TrimSpace(endpointPath)
	if p == "" || p == "/" {
		return append([]string(nil), candidateWSPaths...)
	}
	return []string{p}
}

func buildWSURL(endpoint, wsPath, token string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return "", fmt.Errorf("cdp endpoint %q: %w", endpoint, err)
	}
	switch strings.ToLower(u.Scheme) {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
	default:
		return "", fmt.Errorf("cdp endpoint %q: scheme must be http(s) or ws(s)", endpoint)
	}
	if u.Host == "" {
		return "", fmt.Errorf("cdp endpoint %q: host is required", endpoint)
	}
	if !strings.HasPrefix(wsPath, "/") {
		wsPath = "/" + wsPath
	}
	u.Path = wsPath
	u.Fragment = ""
	if token = strings.TrimSpace(token); token != "" {
		q := u.Query()
		q.Set("token", token)
		u.RawQuery = q.Encode()
	}
	return u.String(), nil
}

// ProbeResult describes a reachable browserless endpoint.
type ProbeResult struct {
	Endpoint  string   `json:"endpoint"`
	Path      string   `json:"path"`
	Version   string   `json:"version,omitempty"`
	Playwright []string `json:"playwright,omitempty"`
	Puppeteer  []string `json:"puppeteer,omitempty"`
}

// ProbeCDP checks connectivity: GET {origin}/meta for the browserless version,
// then a CDP websocket handshake across the candidate paths.
func ProbeCDP(ctx context.Context, endpoint, token string) (*ProbeResult, error) {
	res := &ProbeResult{Endpoint: strings.TrimSpace(endpoint)}
	u, err := url.Parse(res.Endpoint)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("cdp endpoint %q: invalid URL", endpoint)
	}
	meta := *u
	switch strings.ToLower(meta.Scheme) {
	case "ws":
		meta.Scheme = "http"
	case "wss":
		meta.Scheme = "https"
	}
	meta.Path = "/meta"
	meta.RawQuery = ""
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, meta.String(), nil)
	if err != nil {
		return nil, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	hc := &http.Client{Timeout: 5 * time.Second}
	if resp, err := hc.Do(req); err == nil {
		defer resp.Body.Close()
		if resp.StatusCode == http.StatusOK {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			var metaResp struct {
				Version    string   `json:"version"`
				Playwright []string `json:"playwright"`
				Puppeteer  []string `json:"puppeteer"`
			}
			if json.Unmarshal(body, &metaResp) == nil {
				res.Version = metaResp.Version
				res.Playwright = metaResp.Playwright
				res.Puppeteer = metaResp.Puppeteer
			}
		}
	}
	path, err := ProbeWSPath(ctx, endpoint, token)
	if err != nil {
		return res, err
	}
	res.Path = path
	return res, nil
}

// ProbeWSPath returns the first candidate path whose websocket handshake succeeds.
func ProbeWSPath(ctx context.Context, endpoint, token string) (string, error) {
	var lastErr error
	for _, p := range candidatePaths(endpointPathOf(endpoint)) {
		wsURL, err := buildWSURL(endpoint, p, token)
		if err != nil {
			return "", err
		}
		if err := wsHandshake(ctx, wsURL); err != nil {
			lastErr = err
			continue
		}
		return p, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no candidate websocket path")
	}
	return "", fmt.Errorf("cdp websocket probe %s: %w", endpoint, lastErr)
}

func endpointPathOf(endpoint string) string {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil {
		return ""
	}
	return u.Path
}

func wsHandshake(ctx context.Context, wsURL string) error {
	u, err := url.Parse(wsURL)
	if err != nil {
		return err
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	hc := &http.Client{Timeout: 5 * time.Second}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusSwitchingProtocols {
		return fmt.Errorf("%s: HTTP %d", u.Path, resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 5: 跑测试**

```bash
go test ./internal/browser/ -run 'TestCandidatePaths|TestBuildWSURL' -v
# 期望：PASS
```

- [ ] **Step 6: 写引擎**

```go
// internal/browser/playwright.go
package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/mxschmitt/playwright-go"
)

const pwNavTimeout = 45 * time.Second

// pwDriver starts the Playwright driver process once per roundpend process.
type pwDriver struct {
	once sync.Once
	pw   *playwright.Playwright
	err  error
}

func (d *pwDriver) get() (*playwright.Playwright, error) {
	d.once.Do(func() {
		if err := playwright.Install(&playwright.RunOptions{SkipInstallBrowsers: true}); err != nil {
			d.err = fmt.Errorf("playwright driver install: %w", err)
			return
		}
		d.pw, d.err = playwright.Run()
		if d.err != nil {
			d.err = fmt.Errorf("playwright driver start: %w", d.err)
		}
	})
	return d.pw, d.err
}

func (d *pwDriver) stop() {
	if d != nil && d.pw != nil {
		_ = d.pw.Stop()
	}
}

// pwAttach describes one engine attachment.
type pwAttach struct {
	Endpoint      string // ws(s):// or http(s):// origin/member path (empty for host mode)
	Token         string
	LaunchPath    string // host mode: Chrome executable
	Width, Height int
}

// PlaywrightEngine implements Engine over a Playwright browser.
type PlaywrightEngine struct {
	driver  *pwDriver
	browser playwright.Browser
	bctx    playwright.BrowserContext
	page    playwright.Page
	url     string
	width   int
	height  int
	mu      sync.Mutex
}

func newPlaywrightEngine(d *pwDriver, att pwAttach) (*PlaywrightEngine, error) {
	pw, err := d.get()
	if err != nil {
		return nil, err
	}
	if att.Width <= 0 {
		att.Width = 1280
	}
	if att.Height <= 0 {
		att.Height = 800
	}
	var browser playwright.Browser
	if strings.TrimSpace(att.LaunchPath) != "" {
		headless := true
		browser, err = pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
			ExecutablePath: playwright.String(att.LaunchPath),
			Headless:       &headless,
			Args: []string{
				fmt.Sprintf("--window-size=%d,%d", att.Width, att.Height),
				"--hide-scrollbars", "--mute-audio", "--disable-dev-shm-usage",
			},
		})
		if err != nil {
			return nil, fmt.Errorf("launch host chrome: %w", err)
		}
	} else {
		browser, err = connectCandidate(pw, att.Endpoint, att.Token)
		if err != nil {
			return nil, fmt.Errorf("cdp attach: %w", err)
		}
	}
	bctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		Viewport:  &playwright.Size{Width: att.Width, Height: att.Height},
		UserAgent: playwright.String(desktopChromeUA),
	})
	if err != nil {
		_ = browser.Close()
		return nil, fmt.Errorf("browser context: %w", err)
	}
	if _, err := bctx.AddInitScript(playwright.Script{Content: playwright.String(stealthInitJS)}); err != nil {
		_ = bctx.Close()
		_ = browser.Close()
		return nil, fmt.Errorf("stealth init script: %w", err)
	}
	page, err := bctx.NewPage()
	if err != nil {
		_ = bctx.Close()
		_ = browser.Close()
		return nil, fmt.Errorf("new page: %w", err)
	}
	return &PlaywrightEngine{
		driver: d, browser: browser, bctx: bctx, page: page,
		width: att.Width, height: att.Height,
	}, nil
}

// cdpPathCache remembers the working ws path per endpoint origin.
var cdpPathCache sync.Map // string -> string

// connectCandidate connects over CDP, trying candidate paths in order.
func connectCandidate(pw *playwright.Playwright, endpoint, token string) (playwright.Browser, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, fmt.Errorf("cdp endpoint is required")
	}
	paths := candidatePaths(endpointPathOf(endpoint))
	if v, ok := cdpPathCache.Load(endpoint); ok {
		paths = []string{v.(string)}
	}
	var lastErr error
	for _, p := range paths {
		wsURL, err := buildWSURL(endpoint, p, token)
		if err != nil {
			return nil, err
		}
		b, err := pw.Chromium.ConnectOverCDP(wsURL)
		if err == nil {
			cdpPathCache.Store(endpoint, p)
			return b, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no candidate path connected")
	}
	return nil, lastErr
}

func (e *PlaywrightEngine) Navigate(ctx context.Context, target string) error {
	timeout := float64(pwNavTimeout.Milliseconds())
	if deadline, ok := ctx.Deadline(); ok {
		if remain := time.Until(deadline).Milliseconds(); remain > 0 && float64(remain) < timeout {
			timeout = float64(remain)
		}
	}
	if _, err := e.page.Goto(strings.TrimSpace(target), playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(timeout),
	}); err != nil {
		return err
	}
	e.mu.Lock()
	e.url = e.page.URL()
	e.mu.Unlock()
	return nil
}

func (e *PlaywrightEngine) URL() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.url
}

func (e *PlaywrightEngine) Title(ctx context.Context) (string, error) {
	_ = ctx
	return e.page.Title()
}

func (e *PlaywrightEngine) Snapshot(ctx context.Context) (Snapshot, error) {
	_ = ctx
	raw, err := e.page.Evaluate(snapshotJS)
	if err != nil {
		return Snapshot{}, err
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return Snapshot{}, err
	}
	var out Snapshot
	if err := json.Unmarshal(body, &out); err != nil {
		return Snapshot{}, err
	}
	e.mu.Lock()
	e.url = out.URL
	e.mu.Unlock()
	return out, nil
}

func (e *PlaywrightEngine) locator(ref string) playwright.Locator {
	return e.page.Locator(fmt.Sprintf(`[data-rp-ref=%q]`, ref))
}

func (e *PlaywrightEngine) Hover(ctx context.Context, ref string) error {
	_ = ctx
	return e.locator(ref).Hover()
}

func (e *PlaywrightEngine) Click(ctx context.Context, ref string) error {
	_ = ctx
	return e.locator(ref).Click()
}

func (e *PlaywrightEngine) Type(ctx context.Context, ref, text string, submit bool) error {
	_ = ctx
	if err := e.locator(ref).Fill(text); err != nil {
		return err
	}
	if submit {
		return e.locator(ref).Press("Enter")
	}
	return nil
}

func (e *PlaywrightEngine) Press(ctx context.Context, key string) error {
	_ = ctx
	return e.page.Keyboard().Press(key)
}

func (e *PlaywrightEngine) Screenshot(ctx context.Context) ([]byte, error) {
	_ = ctx
	return e.page.Screenshot()
}

func (e *PlaywrightEngine) SetViewport(ctx context.Context, width, height int) error {
	_ = ctx
	if err := e.page.SetViewportSize(width, height); err != nil {
		return err
	}
	e.mu.Lock()
	e.width, e.height = width, height
	e.mu.Unlock()
	return nil
}

func (e *PlaywrightEngine) Evaluate(ctx context.Context, expression string) (json.RawMessage, error) {
	_ = ctx
	v, err := e.page.Evaluate(expression)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(raw), nil
}

func (e *PlaywrightEngine) InputClick(ctx context.Context, x, y float64) error {
	_ = ctx
	return e.page.Mouse().Click(x, y)
}

func (e *PlaywrightEngine) InputMove(ctx context.Context, x, y float64) error {
	_ = ctx
	return e.page.Mouse().Move(x, y)
}

func (e *PlaywrightEngine) InputWheel(ctx context.Context, x, y, deltaX, deltaY float64) error {
	_ = ctx
	return e.page.Mouse().Wheel(deltaX, deltaY)
}

func (e *PlaywrightEngine) InputType(ctx context.Context, text string) error {
	_ = ctx
	return e.page.Keyboard().Type(text)
}

func (e *PlaywrightEngine) InputKey(ctx context.Context, key string) error {
	_ = ctx
	return e.page.Keyboard().Press(key)
}

func (e *PlaywrightEngine) Close() error {
	if e.page != nil {
		_ = e.page.Close()
	}
	if e.bctx != nil {
		_ = e.bctx.Close()
	}
	if e.browser != nil {
		return e.browser.Close()
	}
	return nil
}
```

- [ ] **Step 7: 写 gated 集成测试**

```go
// internal/browser/playwright_test.go
package browser

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestPlaywrightEngineAgainstRealBrowser exercises the engine against a real
// CDP endpoint. Set ROUNDPEN_TEST_BROWSER_WS (e.g.
// ws://10.10.1.3:3000/chrome) to run; skipped otherwise.
func TestPlaywrightEngineAgainstRealBrowser(t *testing.T) {
	endpoint := strings.TrimSpace(os.Getenv("ROUNDPEN_TEST_BROWSER_WS"))
	if endpoint == "" {
		t.Skip("ROUNDPEN_TEST_BROWSER_WS not set")
	}
	d := &pwDriver{}
	defer d.stop()
	eng, err := newPlaywrightEngine(d, pwAttach{Endpoint: endpoint, Width: 1280, Height: 800})
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer eng.Close()

	ctx := context.Background()
	if err := eng.SetViewport(ctx, 1024, 768); err != nil {
		t.Fatalf("set viewport: %v", err)
	}
	// data: URL keeps the test independent of network access from the browser.
	html := "data:text/html,<html><head><title>engine</title></head><body>" +
		"<input placeholder=q><button onclick=\"document.title='clicked'\">Go</button></body></html>"
	if err := eng.Navigate(ctx, html); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	snap, err := eng.Snapshot(ctx)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	var inputRef, buttonRef string
	for _, n := range snap.Nodes {
		switch n.Tag {
		case "input":
			inputRef = n.Ref
		case "button":
			buttonRef = n.Ref
		}
	}
	if inputRef == "" || buttonRef == "" {
		t.Fatalf("refs missing in snapshot: %+v", snap.Nodes)
	}
	if err := eng.Type(ctx, inputRef, "hello", false); err != nil {
		t.Fatalf("type: %v", err)
	}
	if err := eng.Hover(ctx, buttonRef); err != nil {
		t.Fatalf("hover: %v", err)
	}
	if err := eng.Click(ctx, buttonRef); err != nil {
		t.Fatalf("click: %v", err)
	}
	if title, err := eng.Title(ctx); err != nil || title != "clicked" {
		t.Fatalf("title = %q, %v (want clicked)", title, err)
	}
	if _, err := eng.Evaluate(ctx, "1+1"); err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	png, err := eng.Screenshot(ctx)
	if err != nil || len(png) < 1000 {
		t.Fatalf("screenshot: %d bytes, %v", len(png), err)
	}
	if err := eng.InputClick(ctx, 20, 20); err != nil {
		t.Fatalf("input click: %v", err)
	}
	if err := eng.InputType(ctx, "x"); err != nil {
		t.Fatalf("input type: %v", err)
	}
	if err := eng.InputKey(ctx, "Tab"); err != nil {
		t.Fatalf("input key: %v", err)
	}
	if err := eng.Press(ctx, "Enter"); err != nil {
		t.Fatalf("press: %v", err)
	}
}
```

- [ ] **Step 8: 跑测试（用局域网实机）**

```bash
go test ./internal/browser/ -run TestPlaywrightEngine -v
# 期望：SKIP（未设环境变量）

ROUNDPEN_TEST_BROWSER_WS=ws://10.10.1.3:3000/chrome go test ./internal/browser/ -run TestPlaywrightEngine -v
# 期望：PASS
```

- [ ] **Step 9: 提交**

```bash
git add internal/browser/snapshot.go internal/browser/playwright.go internal/browser/cdp.go internal/browser/cdp_test.go internal/browser/playwright_test.go internal/browser/chrome.go
git commit -m "feat(browser): playwright engine over CDP with candidate-path probing"
```

### Task 3: Hub 接入 Playwright

**Files:**
- Create: `internal/browser/dial.go`
- Modify: `internal/browser/hub.go`

- [ ] **Step 1: 迁出 dial 相关代码**

新建 `internal/browser/dial.go`，把 `internal/browser/remote.go` 中以下内容**原样**移入（`package browser`）：`PortDialer` 接口、`startCDPProxy`、`proxyCDPConn`。其余（`newRemoteEngine`、`probeGuestCDP`、`cdpVersionOK`、`readHTTPResponse`、`httpContentLength`）本任务结束时随 remote.go 一起删除（Task 4）。

- [ ] **Step 2: 改 hub.attach**

把 `internal/browser/hub.go` 的 `attach()`（第 257-329 行）整体替换为：

```go
func (h *Hub) attach(ctx context.Context, id string) (*Session, error) {
	if h.newEngine != nil { // test injection, unchanged
		dir := filepath.Join(h.dataDir, "browser", id)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
		eng, err := h.newEngine(dir, 1280, 800)
		if err != nil {
			return nil, err
		}
		return &Session{SandboxID: id, Engine: eng, Width: 1280, Height: 800}, nil
	}

	h.mu.Lock()
	cfg := h.cfg
	dial := h.dial
	driver := h.driver
	token := ""
	if tok := h.browserToken(id); tok != "" {
		token = tok
	}
	h.mu.Unlock()

	width, height := 1280, 800
	provider := config.ResolveCDPProvider(cfg, ChromeOnPATH())
	att := pwAttach{Width: width, Height: height, Token: token}

	switch provider {
	case config.CDPProviderHost:
		if cfg != nil && strings.TrimSpace(cfg.CDP.Endpoint) != "" {
			att.Endpoint = cfg.CDP.Endpoint
			if cfg != nil {
				att.Token = cfg.CDP.Token
			}
			if att.Token == "" {
				att.Token = token
			}
			_ = dial
			eng, err := newPlaywrightEngine(driver, att)
			if err != nil {
				return nil, fmt.Errorf("host cdp: %w", err)
			}
			return &Session{SandboxID: id, Engine: eng, Width: width, Height: height}, nil
		}
		bin := lookupChrome()
		if bin == "" {
			return nil, fmt.Errorf("host chrome not found (set CHROME_PATH)")
		}
		att.LaunchPath = bin
		eng, err := newPlaywrightEngine(driver, att)
		if err != nil {
			return nil, fmt.Errorf("host chrome: %w", err)
		}
		return &Session{SandboxID: id, Engine: eng, Width: width, Height: height}, nil

	case config.CDPProviderRemote, config.CDPProviderCloud:
		endpoint := ""
		if cfg != nil {
			endpoint = cfg.CDP.Endpoint
		}
		att.Endpoint = endpoint
		if cfg != nil && cfg.CDP.Token != "" {
			att.Token = cfg.CDP.Token
		}
		eng, err := newPlaywrightEngine(driver, att)
		if err != nil {
			return nil, fmt.Errorf("%s cdp: %w", provider, err)
		}
		return &Session{SandboxID: id, Engine: eng, Width: width, Height: height}, nil

	case config.CDPProviderDocker:
		port := config.DefaultCDPPort
		if cfg != nil && cfg.CDP.Port > 0 {
			port = cfg.CDP.Port
		}
		if dial == nil {
			return nil, fmt.Errorf("docker cdp requires a sandbox dialer")
		}
		localURL, stop, err := startCDPProxy(dial, id, port)
		if err != nil {
			return nil, fmt.Errorf("env cdp: %w", err)
		}
		att.Endpoint = localURL
		eng, err := newPlaywrightEngine(driver, att)
		if err != nil {
			stop()
			return nil, fmt.Errorf("env cdp (Browser env :%d not serving CDP): %w", port, err)
		}
		return &Session{SandboxID: id, Engine: eng, Width: width, Height: height, release: stop}, nil
	default:
		_ = ctx
		return nil, fmt.Errorf("unknown cdp provider %q", provider)
	}
}
```

要点：
- `startCDPProxy` 返回的是 `http://127.0.0.1:<port>`，`connectCandidate` 会把它转成 `ws://...` 并按候选路径尝试（转发是原始 TCP，token 参数会原样带到上游 browserless）。
- 删掉 `probeGuestCDP` 调用（就绪探测改由 `newPlaywrightEngine` + 重试完成）。

- [ ] **Step 3: Hub 增加 driver 字段与 token 读取**

在 `Hub` 结构体（hub.go:19-28）加：

```go
	driver *pwDriver
```

`NewHub` 里初始化：`driver: &pwDriver{}`。新增方法：

```go
// browserToken returns the browserless token stored on the sandbox record.
func (h *Hub) browserToken(sandboxID string) string {
	if h == nil || h.tokenLookup == nil {
		return ""
	}
	return h.tokenLookup(sandboxID)
}
```

并在 Hub 上加字段：

```go
	// tokenLookup resolves a sandbox id to its stored browser token
	// (sandbox.Metadata["browserToken"]); wired in cmd/roundpend.
	tokenLookup func(sandboxID string) string
```

加 setter：

```go
// SetTokenLookup supplies sandbox metadata lookup for the browserless token.
func (h *Hub) SetTokenLookup(f func(sandboxID string) string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.tokenLookup = f
	h.mu.Unlock()
}
```

`Hub.Close()` 里在遍历关闭会话后调用 `h.driver.stop()`。

- [ ] **Step 4: 自定义引擎注入的测试仍要通过**

```bash
go test ./internal/browser/ -run 'TestHub|TestStatus|TestTakeover' -v
# 期望：PASS（现有 hub_test 用 newEngine 注入，不受影响）
```

- [ ] **Step 5: 提交**

```bash
git add internal/browser/hub.go internal/browser/dial.go
git commit -m "feat(browser): hub attaches sessions through the playwright engine"
```

### Task 4: 重试语义与 chromedp 移除

**Files:**
- Modify: `internal/browser/hub.go`（`cdpRetryable`）
- Delete: `internal/browser/chrome.go`、`internal/browser/remote.go`
- Modify: `go.mod`（`go mod tidy` 移除 chromedp）

- [ ] **Step 1: 更新可重试错误匹配**

```go
func cdpRetryable(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "requires a sandbox dialer") {
		return false
	}
	for _, needle := range []string{
		"econnrefused",
		"connection refused",
		"connection reset",
		"websocket was closed before the connection was established",
		"socket hang up",
		"timeout",
		"timed out",
		"empty reply",
		"eof",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
```

依据（spike 实测）：端口不通 → `connect ECONNREFUSED`；路径不可用/未就绪 → `WebSocket was closed before the connection was established`。

- [ ] **Step 2: 删 chrome.go / remote.go，确认无残留引用**

```bash
git rm internal/browser/chrome.go internal/browser/remote.go
grep -rn "newChromeEngine\|newRemoteEngine\|probeGuestCDP\|chromeEngine" --include="*.go" internal cmd
# 期望：无输出（hub.go 里对 lookupChrome/ChromeOnPATH 的引用保留在 playwright 路径上；
# 若 ChromeOnPATH/lookupChrome 原在 chrome.go，需要把它们挪到 internal/browser/host.go）
```

如 `ChromeOnPATH`/`lookupChrome` 随 chrome.go 被删，新建 `internal/browser/host.go` 放这两个函数**并新增导出的 `ChromePath()`**（原代码照搬，含 `CHROME_PATH` 环境变量与 `google-chrome`/`chromium` 名称扫描）：

```go
// internal/browser/host.go
package browser

// ChromePath returns the host Chrome executable path, or "" when absent.
func ChromePath() string { return lookupChrome() }
```

（Task 7 的设置页测试连接用它；`ChromeOnPATH()` 保留原签名。）

- [ ] **Step 3: 清理依赖**

```bash
go mod tidy
grep -n "chromedp" go.mod
# 期望：无输出
go build ./... && go vet ./...
```

- [ ] **Step 4: 全量测试**

```bash
go test ./internal/browser/... ./internal/runtime/... ./internal/api/... 2>&1 | tail -20
# 期望：PASS（DB 相关用例按仓库既有约定 skip）
```

- [ ] **Step 5: 提交**

```bash
git add -A internal/browser go.mod go.sum
git commit -m "refactor(browser): drop chromedp; retry on playwright connect errors"
```

---

## Phase B — 来源模型

### Task 5: 配置与端口默认值

**Files:**
- Modify: `internal/config/cdp.go:16`、`internal/config/config.go:25,53,56`、`internal/settings/settings.go:83-90`
- Test: `internal/config/cdp_test.go`

- [ ] **Step 1: 改默认值（先写测试）**

在 `internal/config/cdp_test.go` 追加：

```go
func TestDefaultCDPPortIsBrowserless(t *testing.T) {
	if DefaultCDPPort != 3000 {
		t.Fatalf("DefaultCDPPort = %d, want 3000 (browserless)", DefaultCDPPort)
	}
	cfg := CDPConfig{Provider: CDPProviderAuto}
	if err := NormalizeCDP(&cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Port != 3000 {
		t.Fatalf("normalized port = %d, want 3000", cfg.Port)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/config/ -run TestDefaultCDPPortIsBrowserless -v
# 期望：FAIL（got 9222）
```

- [ ] **Step 3: 改实现**

- `internal/config/cdp.go:16`：`DefaultCDPPort = 3000`。
- `internal/config/config.go:56`：`BrowserImage: getenv("ROUNDPEN_BROWSER_IMAGE", "ghcr.io/browserless/chrome:v2.56.7")`。
- `internal/config/config.go:53`：`DefaultBrowserTemplate: getenv("ROUNDPEN_DEFAULT_BROWSER_TEMPLATE", "browser")`。
- `internal/config/config.go:25` 注释改为 `// default "browser"`，`// default browser qcow2` → `// default browser OCI image`。
- `internal/config/cdp.go` 的 `CDPHint`：`docker` 分支改为 `"Runs a Roundpen-managed browserless/chrome container per user; the control plane dials its CDP port."`；`host` 分支改为 `"Launches Chrome installed on the roundpend host (no live view; screenshot takeover only)."`；`remote` 分支改为 `"Attaches to a self-hosted browserless endpoint (LAN) via CDP."`。
- `internal/settings/settings.go:88`：`s.CDPPort = config.DefaultCDPPort` 已引用常量，无需改；确认 `decode.go:42` 的 fallback 也走常量。
- `internal/settings/settings.go:199-201` 的 `CDPHint` 调用保持不变。

- [ ] **Step 4: 跑测试**

```bash
go test ./internal/config/ ./internal/settings/ -v 2>&1 | tail -20
# 期望：PASS
```

- [ ] **Step 5: 提交**

```bash
git add internal/config internal/settings
git commit -m "feat(config): browser provider defaults point at browserless (port 3000, OCI image)"
```

### Task 6: `BrowserTarget` 重构

**Files:**
- Modify: `internal/userenv/userenv.go`
- Modify: `internal/api/envapi/handler.go`、`internal/api/agentapi/browser.go`、`internal/api/agentapi/tasks.go`、`internal/acp/sysagent/tools/browser.go`
- Test: `internal/userenv/userenv_test.go`

- [ ] **Step 1: 写测试（外部 provider 不建沙箱）**

在 `internal/userenv/userenv_test.go` 追加（沿用文件里已有的 fake store / fake sandboxes 构造方式；若现有测试用 `fake_test.go` 的 `fakeSandboxes`，复用它）：

```go
func TestEnsureBrowserExternalProvider(t *testing.T) {
	svc, _, _ := newFakeService(t) // 现有测试的构造函数；如名称不同，复用同一套 fake
	svc.Cfg = &config.Config{CDP: config.CDPConfig{Provider: config.CDPProviderRemote, Endpoint: "ws://10.10.1.3:3000/chrome"}}
	target, err := svc.EnsureBrowser(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if target.Managed || target.Sandbox != nil {
		t.Fatalf("remote provider must not create a sandbox: %+v", target)
	}
	if target.Key != "browser-alice" {
		t.Fatalf("key = %q, want browser-alice", target.Key)
	}
	if target.Provider != config.CDPProviderRemote {
		t.Fatalf("provider = %q", target.Provider)
	}
}

func TestEnsureBrowserManagedProvider(t *testing.T) {
	svc, _, _ := newFakeService(t)
	svc.Cfg = &config.Config{CDP: config.CDPConfig{Provider: config.CDPProviderDocker}}
	target, err := svc.EnsureBrowser(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	if !target.Managed || target.Sandbox == nil {
		t.Fatalf("docker provider must create a sandbox: %+v", target)
	}
	if target.Key != target.Sandbox.ID {
		t.Fatalf("key = %q, want %q", target.Key, target.Sandbox.ID)
	}
}
```

（如果 `newFakeService` 在现有测试里叫别的名字，用它的等价物；本步骤的目标是断言上面两种行为，不引入新的 fake 框架。）

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/userenv/ -run 'TestEnsureBrowserExternalProvider|TestEnsureBrowserManagedProvider' -v
# 期望：编译失败（svc.Cfg 不存在 / EnsureBrowser 返回 *sandbox.Sandbox）
```

- [ ] **Step 3: 改 userenv**

`internal/userenv/userenv.go`：

```go
// BrowserTarget is the resolved browser source for one user.
type BrowserTarget struct {
	Key      string           // hub session key: sandbox id (managed) or browser-<user>
	Provider string           // resolved provider (docker|remote|cloud|host)
	Managed  bool             // true when Roundpen runs the container
	Sandbox  *sandbox.Sandbox // non-nil when Managed
}
```

`Service` 加字段 `Cfg *config.Config`（import `"github.com/RoundpenAI/roundpen/internal/config"`）。

`EnsureBrowser` 改为：

```go
// EnsureBrowser resolves the user's browser source. Managed (default) starts or
// resumes the Roundpen browserless container; external providers need no sandbox.
func (s *Service) EnsureBrowser(ctx context.Context, userID string) (*BrowserTarget, error) {
	provider := config.ResolveCDPProvider(s.Cfg, browser.ChromeOnPATH())
	if provider != config.CDPProviderDocker {
		if provider == config.CDPProviderRemote || provider == config.CDPProviderCloud {
			if s.Cfg == nil || strings.TrimSpace(s.Cfg.CDP.Endpoint) == "" {
				return nil, fmt.Errorf("cdp provider %s requires an endpoint", provider)
			}
		}
		return &BrowserTarget{Key: "browser-" + sanitizeUser(userID), Provider: provider}, nil
	}
	if s.Probe != nil {
		if err := s.Probe.RequireBrowser(); err != nil {
			return nil, err
		}
	}
	sb, err := s.ensure(ctx, userID, SlotBrowser, s.browserTemplate(), "Browser", runtime.EngineDocker)
	if err != nil {
		return nil, err
	}
	return &BrowserTarget{Key: sb.ID, Provider: provider, Managed: true, Sandbox: sb}, nil
}
```

要点：
- 托管分支的 engine 参数从 `runtime.EngineQEMU` 改为 `runtime.EngineDocker`（adopt 时校验 `runtime.EngineOfImage(sb.Image)`，旧 qcow2 记录会因此判失败 → 走既有删除重建路径，正是 spec §4.7 想要的）。
- `import "github.com/RoundpenAI/roundpen/internal/browser"` 用于 `ChromeOnPATH()`；若 userenv 引 browser 包产生循环依赖，则把 `ChromeOnPATH` 的判定下移为 `config.ResolveCDPProvider(nil, ...)` 等价逻辑（`internal/browser` 不 import `internal/userenv`，正常不成环）。
- `EnvView` 加字段 `Provider string \`json:"provider,omitempty"\``；`List()` 里对 browser 槽位填 `config.ResolveCDPProvider(s.Cfg, browser.ChromeOnPATH())`，外部 provider 且无沙箱记录时 `Status = "external"`。

- [ ] **Step 4: 改调用方**

- `internal/api/envapi/handler.go`：`Environments` 接口的 `EnsureBrowser` 返回类型改 `(*userenv.BrowserTarget, error)`；`ensureBrowser` 响应体改为：

```go
	target, err := h.Envs.EnsureBrowser(r.Context(), user.Username)
	if err != nil {
		if runtime.WriteNotReady(w, err) {
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	resp := map[string]any{
		"slot":     userenv.SlotBrowser,
		"provider": target.Provider,
		"managed":  target.Managed,
	}
	if target.Sandbox != nil {
		resp["sandboxId"] = target.Sandbox.ID
		resp["status"] = string(target.Sandbox.Status)
	}
	writeJSON(w, http.StatusOK, resp)
```

  同文件 `desktopLink` 里对 `sb.ID`/`sb.Status` 的使用随之改为 `target.Sandbox`（该 handler 在 Task 11 会被整体替换；本步骤先让它编译通过：`target.Sandbox == nil` 时返回 409 `"live view requires the managed browser container"`）。

- `internal/api/agentapi/browser.go`：`ensureHubKey`（59-75 行）与 `hubKey`（46-57 行）改为使用 target：

```go
func (h *Handler) ensureHubKey(r *http.Request, sess *agentsession.Session) string {
	if h.Envs != nil && sess != nil {
		userID := sess.UserID
		if user := auth.GetUser(r.Context()); user != nil {
			userID = user.Username
		}
		if target, err := h.Envs.EnsureBrowser(r.Context(), userID); err == nil && target != nil && target.Key != "" {
			return target.Key
		}
		if id, err := h.Envs.BrowserSandboxID(r.Context(), userID); err == nil && id != "" {
			return id
		}
	}
	return browser.AgentBrowserID(sess.ID)
}
```

  `hubKey` 同理（用 `BrowserSandboxID` 的返回值，命中不到再回落 `AgentBrowserID(sess.ID)`）。
- `internal/api/agentapi/tasks.go:112-117`：`_, _ = h.Envs.EnsureBrowser(...)` 不变（返回值现在带 target，忽略即可）。
- `internal/acp/sysagent/tools/browser.go:14-17` 接口改为：

```go
type BrowserSlot interface {
	EnsureBrowser(ctx context.Context, userID string) (*userenv.BrowserTarget, error)
}
```

  `resolveID`（26-41 行）改为：

```go
func (b *BrowserBinder) resolveID(ctx context.Context, userID string) (string, error) {
	if b != nil && b.Slots != nil && strings.TrimSpace(userID) != "" {
		target, err := b.Slots.EnsureBrowser(ctx, userID)
		if err != nil {
			return "", fmt.Errorf("ensure browser environment: %w", err)
		}
		if target == nil || target.Key == "" {
			return "", fmt.Errorf("browser is not available")
		}
		return target.Key, nil
	}
	if b == nil {
		return browser.AgentBrowserID(""), nil
	}
	return browser.AgentBrowserID(b.SessionID), nil
}
```

  文件 import 增加 `"github.com/RoundpenAI/roundpen/internal/userenv"`。

- `cmd/roundpend/main.go`：`envSvc` 构造处加 `Cfg: cfg`（第 215 行附近的 `&userenv.Service{...}` 字面量）。

- [ ] **Step 5: 跑测试与构建**

```bash
go build ./... && go test ./internal/userenv/... ./internal/acp/... ./internal/api/... 2>&1 | tail -20
# 期望：PASS
```

- [ ] **Step 6: 提交**

```bash
git add -A internal/userenv internal/api internal/acp cmd/roundpend
git commit -m "feat(userenv): browser target resolves managed vs external sources"
```

### Task 7: `RequireBrowser` 按 provider 判定 + 设置页「测试连接」

**Files:**
- Modify: `internal/runtime/probe.go:121-149`
- Modify: `internal/settings/service.go`、`internal/settings/http.go`
- Test: `internal/runtime/probe_test.go`

- [ ] **Step 1: 写测试**

`internal/runtime/probe_test.go` 追加：

```go
func TestRequireBrowserProviderAware(t *testing.T) {
	p := &Probe{Cfg: &config.Config{CDP: config.CDPConfig{Provider: config.CDPProviderRemote, Endpoint: "ws://lan:3000/chrome"}}}
	if err := p.RequireBrowser(); err != nil {
		t.Fatalf("remote with endpoint should be ready: %v", err)
	}
	p2 := &Probe{Cfg: &config.Config{CDP: config.CDPConfig{Provider: config.CDPProviderRemote}}}
	if err := p2.RequireBrowser(); err == nil {
		t.Fatal("remote without endpoint must be not-ready")
	}
	p3 := &Probe{Cfg: &config.Config{CDP: config.CDPConfig{Provider: config.CDPProviderDocker}}, DockerReady: false}
	if err := p3.RequireBrowser(); err == nil {
		t.Fatal("docker provider without docker must be not-ready")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/runtime/ -run TestRequireBrowserProviderAware -v
# 期望：FAIL（当前实现检查 QEMU 二进制/qcow2）
```

- [ ] **Step 3: 重写 RequireBrowser**

替换 `internal/runtime/probe.go:121-149` 为：

```go
// RequireBrowser returns NotReady when the configured browser source cannot run.
func (p *Probe) RequireBrowser() error {
	provider := config.ResolveCDPProvider(p.cfg(), false)
	switch provider {
	case config.CDPProviderRemote, config.CDPProviderCloud:
		if strings.TrimSpace(p.cfg().CDP.Endpoint) == "" {
			return &NotReady{
				Engine:  provider,
				Message: fmt.Sprintf("browser provider %s needs a CDP endpoint", provider),
				Setup: []SetupStep{{
					Title:  "Configure the browser endpoint",
					Detail: "Settings → Browser → CDP endpoint (ws://… or http://… of a browserless deployment).",
				}},
			}
		}
		return nil

	case config.CDPProviderHost:
		if browser.ChromeOnPATH() {
			return nil
		}
		return &NotReady{
			Engine:  provider,
			Message: "host browser provider needs Chrome on this machine",
			Setup: []SetupStep{{
				Title:   "Install Google Chrome",
				Detail:  "Host Chrome runs on the roundpend host with no live view; prefer the managed container.",
				Command: "sudo apt install google-chrome-stable",
			}},
		}
	}

	// docker — managed browserless container.
	snap := p.Snapshot()
	if !snap.DockerReady {
		return &NotReady{Engine: EngineDocker, Message: missingMessage(snap.Missing), Setup: snap.Setup}
	}
	if err := browser.ValidateImageRef(p.browserImage()); err != nil {
		return &NotReady{
			Engine:  EngineDocker,
			Message: "browser image is not available locally",
			Setup: []SetupStep{{
				Title:   "Pull the browser image",
				Detail:  err.Error(),
				Command: "docker pull " + p.browserImage(),
			}},
		}
	}
	return nil
}
```

新增（`internal/runtime/probe.go` 内）—— 复用既有的 `Probe.HasImage func(ctx, ref) bool`（`Snapshot()` 已经在用它查 Agent 镜像，`cmd/roundpend` 已接线，无需新增接口）：

```go
func (p *Probe) ValidateImageRef(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" || p == nil || p.HasImage == nil {
		return nil
	}
	if p.HasImage(context.Background(), ref) {
		return nil
	}
	return fmt.Errorf("image %s is not present locally", ref)
}
```

`browserImage()` 默认值改为 `"ghcr.io/browserless/chrome:v2.56.7"`。

import 增加 `internal/browser`；删除对 `internal/backend/qemu` 的 import 与 `browserImage()` 里的 qcow2 校验逻辑。

- [ ] **Step 4: 加测试连接接口**

`internal/settings/service.go` 加：

```go
// BrowserTestResult is the /v1/admin/settings/browser/test payload.
type BrowserTestResult struct {
	Provider   string   `json:"provider"`
	Endpoint   string   `json:"endpoint,omitempty"`
	Path       string   `json:"path,omitempty"`
	Version    string   `json:"version,omitempty"`
	Playwright []string `json:"playwright,omitempty"`
	ChromePath string   `json:"chromePath,omitempty"`
	ChromeOK   bool     `json:"chromeOk,omitempty"`
}

// TestBrowser probes the configured browser source.
func (s *Service) TestBrowser(ctx context.Context) (BrowserTestResult, error) {
	cfg := s.current() // 现有读取当前 settings 的方法；若无，用 s.store.Get + ApplyToConfig
	res := BrowserTestResult{Provider: config.ResolveCDPProvider(cfg, browser.ChromeOnPATH())}
	switch res.Provider {
	case config.CDPProviderHost:
		res.ChromePath = browser.ChromePath()
		res.ChromeOK = res.ChromePath != ""
		if !res.ChromeOK {
			return res, fmt.Errorf("no Chrome binary found on this host")
		}
		return res, nil
	case config.CDPProviderRemote, config.CDPProviderCloud, config.CDPProviderDocker, config.CDPProviderAuto:
		res.Endpoint = cfg.CDP.Endpoint
		if res.Provider == config.CDPProviderDocker {
			res.Endpoint = fmt.Sprintf("http://127.0.0.1 — container CDP port %d (per-user)", cfg.CDP.Port)
			return res, nil
		}
		probe, err := browser.ProbeCDP(ctx, cfg.CDP.Endpoint, cfg.CDP.Token)
		if probe != nil {
			res.Path = probe.Path
			res.Version = probe.Version
			res.Playwright = probe.Playwright
		}
		return res, err
	}
	return res, nil
}
```

（`browser.ChromePath()` = 导出的 `lookupChrome()`，在 Task 4 的 `internal/browser/host.go` 里导出。）

`internal/settings/http.go` 加路由（放在既有两条 `admin/settings` 之后）：

```go
	mux.HandleFunc("POST /v1/admin/settings/browser/test", auth.RequireAdmin(h.browserTest))
```

handler：

```go
func (h *Handler) browserTest(w http.ResponseWriter, r *http.Request) {
	res, err := h.Svc.TestBrowser(r.Context())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error(), "result": res})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": res})
}
```

- [ ] **Step 5: 跑测试**

```bash
go build ./... && go test ./internal/runtime/ ./internal/settings/ -v 2>&1 | tail -20
# 期望：PASS
```

- [ ] **Step 6: 提交**

```bash
git add internal/runtime internal/settings
git commit -m "feat(browser): provider-aware readiness probe and connection test API"
```

---

## Phase C — 托管容器

### Task 8: 模板种子改为 browserless 容器

**Files:**
- Modify: `internal/template/store.go:354,381-388`、`internal/template/builtin.go:10`
- Test: `internal/template/*_test.go`

- [ ] **Step 1: 写测试**

在模板包既有测试文件中追加：

```go
func TestBuiltinBrowserSeedUsesOCI(t *testing.T) {
	entries := builtinEntries("docker", "")
	var found bool
	for _, e := range entries {
		if e.Name != "browser" {
			continue
		}
		found = true
		if e.Slot != "browser" {
			t.Fatalf("slot = %q, want browser", e.Slot)
		}
		if strings.HasSuffix(e.ArtifactRef, ".qcow2") {
			t.Fatalf("artifact = %q must be an OCI image", e.ArtifactRef)
		}
		if !strings.Contains(e.ArtifactRef, "browserless/chrome") {
			t.Fatalf("artifact = %q, want browserless/chrome", e.ArtifactRef)
		}
	}
	if !found {
		t.Fatal("builtin entry \"browser\" not found")
	}
	if _, ok := BuiltinNames["browser-desktop"]; ok {
		t.Fatal("browser-desktop must be dropped from BuiltinNames")
	}
}
```

（`builtinEntries` 是包内函数，签名 `builtinEntries(backend, defaultImage string)`。）

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/template/ -run TestBuiltinBrowserSeedUsesOCI -v
# 期望：FAIL（找不到 browser 条目）
```

- [ ] **Step 3: 改种子与内置表**

`internal/template/store.go:354`：

```go
	browserArtifact := getenv("ROUNDPEN_BROWSER_IMAGE", "ghcr.io/browserless/chrome:v2.56.7")
```

`internal/template/store.go:381-388` 的 `browser-desktop` 条目替换为：

```go
		{
			Namespace: DefaultNamespace, Name: "browser",
			Description: "Browserless Chrome container (CDP :3000, live debugger)",
			Profile:     "browser", Slot: "browser",
			ArtifactRef: browserArtifact, BaseImage: browserArtifact,
			CPUCount: 2, MemoryMB: 2048, DiskSizeMB: 5120, Public: true,
		},
```

`internal/template/builtin.go:10`：`"browser-desktop": {}` → `"browser": {}`。

- [ ] **Step 4: 跑测试**

```bash
go test ./internal/template/... 2>&1 | tail -10
# 期望：PASS（DB 相关用例按既有约定 skip）
```

- [ ] **Step 5: 提交**

```bash
git add internal/template
git commit -m "feat(template): browser slot seeds a browserless OCI image"
```

### Task 9: 托管容器 env 与 token 持久化

**Files:**
- Modify: `internal/userenv/userenv.go`（`createSlot` / `ensure`）
- Test: `internal/userenv/userenv_test.go`

- [ ] **Step 1: 写测试**

```go
func TestBrowserSlotEnvCarriesTokenAndLaunchArgs(t *testing.T) {
	svc, _, _ := newFakeService(t)
	svc.Cfg = &config.Config{CDP: config.CDPConfig{Provider: config.CDPProviderDocker}}
	target, err := svc.EnsureBrowser(context.Background(), "alice")
	if err != nil {
		t.Fatal(err)
	}
	env := svc.lastCreate.Env // fake 记录最后一次 CreateRequest；未提供则加进 fake
	token := env["TOKEN"]
	if len(token) != 32 {
		t.Fatalf("TOKEN = %q (len %d), want 32 hex chars", token, len(token))
	}
	if env["MAX_CONCURRENT_SESSIONS"] != "3" {
		t.Fatalf("MAX_CONCURRENT_SESSIONS = %q", env["MAX_CONCURRENT_SESSIONS"])
	}
	if !strings.Contains(env["DEFAULT_LAUNCH_ARGS"], "--disable-dev-shm-usage") {
		t.Fatalf("DEFAULT_LAUNCH_ARGS = %q", env["DEFAULT_LAUNCH_ARGS"])
	}
	if target.Sandbox.Metadata["browserToken"] != token {
		t.Fatalf("metadata token = %q, want %q", target.Sandbox.Metadata["browserToken"], token)
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/userenv/ -run TestBrowserSlotEnvCarriesTokenAndLaunchArgs -v
# 期望：FAIL（env 里没有 TOKEN）
```

- [ ] **Step 3: 实现**

`internal/userenv/userenv.go`（`createSlot`，第 360-389 行）在构造 `env`/`meta` 后追加：

```go
	if slot == SlotBrowser {
		token, err := randomToken()
		if err != nil {
			return nil, err
		}
		env["TOKEN"] = token
		env["MAX_CONCURRENT_SESSIONS"] = "3"
		env["CONNECTION_TIMEOUT"] = "600000"
		env["ENABLE_DEBUGGER"] = "true"
		env["DEFAULT_LAUNCH_ARGS"] = `["--window-size=1280,800","--hide-scrollbars","--mute-audio","--disable-dev-shm-usage"]`
		meta["browserToken"] = token
	}
```

`meta` 需在 `sandbox.CreateRequest` 之前合并；确认 `meta` map 已存在（该函数已构造 `meta`）。加辅助函数：

```go
func randomToken() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
```

import `crypto/rand`、`encoding/hex`。

- [ ] **Step 4: 跑测试**

```bash
go test ./internal/userenv/... 2>&1 | tail -10
# 期望：PASS
```

顺带确认 workspace 挂载无需改动：`createSlot` 未设 `WorkspaceID` 时 `Sandbox.Manager` 会创建临时工作区并把宿主路径经 `backend.CreateOpts.MountDir` 传给 Docker 后端（docker backend 已挂到容器 `/workspace`），Browser 槽位沿用同一条路径，无需新增代码。

- [ ] **Step 5: 接线 token lookup（Hub）**

`cmd/roundpend/main.go` 在 `browserHub.SetDialer(sbSvc)`（197 行）附近加：

```go
	browserHub.SetTokenLookup(func(sandboxID string) string {
		sb, err := sbSvc.Get(context.Background(), sandboxID)
		if err != nil || sb == nil || sb.Metadata == nil {
			return ""
		}
		return sb.Metadata["browserToken"]
	})
```

`internal/sandbox` 的 `Get` 由 `Manager` 暴露（`sbSvc` 已实现）；若 `Get` 需要 actor 上下文，改用 `sbSvc.Resolve(ctx, sandbox.ResolveRequest{...})` 不行——`Get` 无需 actor，直接用。

- [ ] **Step 6: 提交**

```bash
git add internal/userenv cmd/roundpend
git commit -m "feat(browser): managed container carries a browserless token and launch args"
```

### Task 10: 引擎与容器的对接校验（端到端手工跑一次）

**Files:** 无新增（验证步骤）

- [ ] **Step 1: 本地起容器验证（不需要 roundpen 全跑）**

```bash
docker run -d --name rp-browser-test -p 127.0.0.1:31001:3000 \
  -e TOKEN=devtoken -e MAX_CONCURRENT_SESSIONS=3 \
  -e DEFAULT_LAUNCH_ARGS='["--window-size=1280,800","--disable-dev-shm-usage"]' \
  ghcr.io/browserless/chrome:v2.56.7
sleep 5
curl -s http://127.0.0.1:31001/meta
# 期望：{"version":"2.56.7", ...}
ROUNDPEN_TEST_BROWSER_WS=ws://127.0.0.1:31001/chrome go test ./internal/browser/ -run TestPlaywrightEngine -v
# 期望：PASS（带 token 时用 ws://127.0.0.1:31001/chrome?token=devtoken 再跑一次）
docker rm -f rp-browser-test
```

- [ ] **Step 2: 全链路手工验证（改走 remote，先不依赖容器）**

```bash
# 设置里把 provider 设为 remote，endpoint = ws://10.10.1.3:3000/chrome
go run ./cmd/roundpend
# 浏览器里：Settings → Browser → 测试连接 → 应显示 version 2.56.7 与命中路径 /chrome
# Browser 页面 → Start/resume → 用一次 browser 任务（explore）验证 snapshot/click 链路
```

- [ ] **Step 3: 记录结果并提交（如有代码微调）**

---

## Phase D — 实时视图

### Task 11: live-link 与 live 反代

**Files:**
- Modify: `internal/api/envapi/handler.go`
- Test: `internal/api/envapi/live_test.go`

- [ ] **Step 1: 写测试**

```go
// internal/api/envapi/live_test.go
package envapi

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RoundpenAI/roundpen/internal/sandbox"
	"github.com/RoundpenAI/roundpen/internal/userenv"
)

type fakeEnvs struct {
	target *userenv.BrowserTarget
}

func (f *fakeEnvs) List(context.Context, string) ([]userenv.EnvView, error) { return nil, nil }

func (f *fakeEnvs) EnsureBrowser(context.Context, string) (*userenv.BrowserTarget, error) {
	return f.target, nil
}

func (f *fakeEnvs) EnsureAgent(context.Context, string) (*sandbox.Sandbox, error) { return nil, nil }

func TestLiveProxyRewritesPathAndInjectsToken(t *testing.T) {
	var gotPath, gotQuery string
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery = r.URL.Path, r.URL.RawQuery
		io.WriteString(w, "ok")
	}))
	defer up.Close()
	host, port, _ := net.SplitHostPort(strings.TrimPrefix(up.URL, "http://"))

	target := &userenv.BrowserTarget{
		Key: "sb-browser", Provider: "docker", Managed: true,
		Sandbox: &sandbox.Sandbox{
			ID:       "sb-browser",
			Metadata: map[string]string{"browserToken": "browserless-token"},
		},
	}
	h := &Handler{
		Envs: &fakeEnvs{target: target},
		Dial: fakeDialer(func(context.Context, string, int) (net.Conn, error) {
			return net.Dial("tcp", host+":"+port)
		}),
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/me/environments/browser/live/app.bundle.js?x=1", nil)
	req = req.WithContext(sandbox.WithActor(req.Context(), sandbox.Actor{Username: "alice"}))
	rec := httptest.NewRecorder()
	h.live(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if gotPath != "/debugger/app.bundle.js" {
		t.Fatalf("upstream path = %q, want /debugger/app.bundle.js", gotPath)
	}
	if !strings.Contains(gotQuery, "token=browserless-token") {
		t.Fatalf("upstream query = %q, want injected token", gotQuery)
	}
	if !strings.Contains(gotQuery, "x=1") {
		t.Fatalf("upstream query = %q, want preserved x=1", gotQuery)
	}
}

type fakeDialer func(ctx context.Context, sandboxID string, destPort int) (net.Conn, error)

func (f fakeDialer) Dial(ctx context.Context, sandboxID string, destPort int) (net.Conn, error) {
	return f(ctx, sandboxID, destPort)
}
```

执行时按文件现状对齐两处：把用户放进 context 的方式（`auth.GetUser` 读取的那个 key），以及 `h.Envs == nil` 之外 `auth.GetUser` 为 nil 时 handler 的行为断言。上面用 `auth` 包构造用户上下文的助手名以该包现有测试为准（`internal/api/auth` 的测试文件里能找到同款写法）。

- [ ] **Step 2: 跑测试确认失败**

```bash
go test ./internal/api/envapi/ -run TestLiveProxy -v
# 期望：编译失败（Handler.live / Handler.dial 不存在）
```

- [ ] **Step 3: 实现**

`internal/api/envapi/handler.go` 改动：

1) 删除 `VNCSockLookup`、`desktopUpgrader`、`desktopLink`、`desktopWS`、`httpToWS`，删除路由注册中的两条 desktop 路由与 `VNC` 字段。`Handler` 改为（`Cfg` 与 hub 一样持有共享指针，settings 热更新原地改它，因此 endpoint/token 永远是最新值）：

```go
// SandboxDialer opens a TCP connection to a port inside a sandbox.
type SandboxDialer interface {
	Dial(ctx context.Context, sandboxID string, destPort int) (net.Conn, error)
}

// Handler serves /v1/me/environments*.
type Handler struct {
	Envs      Environments
	Tokens    *preview.Store
	PublicURL string
	Cfg       *config.Config
	Dial      SandboxDialer
}
```

2) `Environments` 接口加 `BrowserTarget(ctx context.Context, userID string) (*userenv.BrowserTarget, error)`（由 `userenv.Service` 提供，M1: `func (s *Service) BrowserTarget(ctx, userID) (*BrowserTarget, error)` = `EnsureBrowser` 的别名，避免部分调用方触发容器创建；实现里对 managed 走 `ensure`、对外部直接返回）。

3) 路由：

```go
	mux.HandleFunc("GET /v1/me/environments/browser/live-link", h.liveLink)
	mux.HandleFunc("/v1/me/environments/browser/live/", h.live)
```

4) handler：

```go
func (h *Handler) liveLink(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Envs == nil {
		writeErr(w, http.StatusServiceUnavailable, "environments not configured")
		return
	}
	target, err := h.Envs.EnsureBrowser(r.Context(), user.Username)
	if err != nil {
		if runtime.WriteNotReady(w, err) {
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	switch target.Provider {
	case config.CDPProviderDocker:
		writeJSON(w, http.StatusOK, map[string]any{
			"mode": "managed",
			"url":  "/v1/me/environments/browser/live/",
		})
	case config.CDPProviderRemote, config.CDPProviderCloud:
		endpoint, token := "", ""
		if h.Cfg != nil {
			endpoint, token = h.Cfg.CDP.Endpoint, h.Cfg.CDP.Token
		}
		u := strings.TrimRight(strings.TrimSpace(endpoint), "/")
		if u == "" {
			writeJSON(w, http.StatusOK, map[string]any{"mode": target.Provider, "url": ""})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"mode": target.Provider,
			"url":  debuggerURL(u, token),
		})
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"mode": "host", "url": "",
			"hint": "host Chrome has no live view; use the screenshot takeover panel",
		})
	}
}

// debuggerURL turns a browserless origin/path into its debugger page URL.
func debuggerURL(endpoint, token string) string {
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return ""
	}
	switch strings.ToLower(u.Scheme) {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	}
	u.Path = "/debugger/"
	u.RawQuery = ""
	if token != "" {
		q := u.Query()
		q.Set("token", token)
		u.RawQuery = q.Encode()
	}
	return u.String()
}

func (h *Handler) live(w http.ResponseWriter, r *http.Request) {
	user := auth.GetUser(r.Context())
	if user == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if h.Envs == nil || h.Dial == nil {
		http.Error(w, "live view not configured", http.StatusServiceUnavailable)
		return
	}
	target, err := h.Envs.EnsureBrowser(r.Context(), user.Username)
	if err != nil || target == nil || !target.Managed || target.Sandbox == nil {
		http.Error(w, "live view requires the managed browser container", http.StatusConflict)
		return
	}
	sandboxID := target.Sandbox.ID
	token := ""
	if target.Sandbox.Metadata != nil {
		token = target.Sandbox.Metadata["browserToken"]
	}
	conn, err := h.Dial.Dial(r.Context(), sandboxID, config.DefaultCDPPort)
	if err != nil {
		http.Error(w, "browser dial: "+err.Error(), http.StatusBadGateway)
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/v1/me/environments/browser/live")
	if rest == "" {
		rest = "/"
	}
	upstreamPath := "/debugger" + rest

	if isWebSocket(r) {
		h.proxyLiveWS(w, r, conn, upstreamPath, token)
		return
	}
	defer conn.Close()
	q := r.URL.Query()
	if token != "" {
		q.Set("token", token)
	}
	director := func(req *http.Request) {
		req.URL = &url.URL{Scheme: "http", Host: "127.0.0.1", Path: upstreamPath, RawQuery: q.Encode()}
		req.Host = "127.0.0.1"
		req.RequestURI = ""
	}
	proxy := &httputil.ReverseProxy{
		Director: director,
		Transport: &http.Transport{
			DialContext:       func(ctx context.Context, network, addr string) (net.Conn, error) { return conn, nil },
			DisableKeepAlives: true,
		},
		ErrorHandler: func(rw http.ResponseWriter, req *http.Request, err error) {
			http.Error(rw, "live proxy error: "+err.Error(), http.StatusBadGateway)
		},
	}
	proxy.ServeHTTP(w, r)
}

func isWebSocket(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func (h *Handler) proxyLiveWS(w http.ResponseWriter, r *http.Request, conn net.Conn, upstreamPath, token string) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		_ = conn.Close()
		http.Error(w, "hijack not supported", http.StatusInternalServerError)
		return
	}
	client, bufrw, err := hj.Hijack()
	if err != nil {
		_ = conn.Close()
		return
	}
	q := r.URL.Query()
	if token != "" {
		q.Set("token", token)
	}
	req := r.Clone(r.Context())
	req.URL.Scheme = "http"
	req.URL.Host = "127.0.0.1"
	req.URL.Path = upstreamPath
	req.URL.RawQuery = q.Encode()
	req.RequestURI = ""
	req.Header.Set("Host", "127.0.0.1")
	if err := req.Write(conn); err != nil {
		_ = conn.Close()
		_ = client.Close()
		return
	}
	_ = bufrw.Flush()

	errCh := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(conn, client)
		errCh <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, conn)
		errCh <- struct{}{}
	}()
	<-errCh
	_ = conn.Close()
	_ = client.Close()
}
```

（结构与 `internal/preview/preview.go` 的 `proxyWebSocket` 一致，区别只在 `upstreamPath` 与注入 token 后的查询串。）

新增 import：`net/http/httputil`、`net/url`、`io`、`internal/config`（`net`、`strings`、`context`、`net/http` 已有）。

- [ ] **Step 4: 跑测试**

```bash
go test ./internal/api/envapi/ -run TestLiveProxy -v
# 期望：PASS
```

- [ ] **Step 5: main.go 注入**

`cmd/roundpend/main.go` 里 `(&envapi.Handler{...}).Mount(mux)`（envapi 构造处）改为：

```go
	(&envapi.Handler{
		Envs:      envSvc,
		Tokens:    previewHandler.Tokens,
		PublicURL: publicURL,
		Cfg:       cfg,
		Dial:      sbSvc,
	}).Mount(mux)
```

（删除原 `VNC: ...` 字段；`sbSvc` 实现 `Dial(ctx, sandboxID, port)`。）

- [ ] **Step 6: 删 auth 白名单**

`internal/api/auth/middleware.go:54` 的 `/v1/me/environments/browser/desktop/ws` 特例删除（live 路由走常规会话鉴权，无 token 例外）。

- [ ] **Step 7: 提交**

```bash
git add internal/api/envapi internal/api/auth cmd/roundpend
git commit -m "feat(envapi): proxy the browserless debugger as the browser live view"
```

### Task 12: 前端（Browser 页实时视图 + 设置页测试连接）

**Files:**
- Modify: `web/src/api.ts:448-470`、`web/src/pages/BrowserPage.tsx:92-105,200-217`、`web/src/pages/SettingsPage.tsx`
- Delete: `web/vnc.html`、`web/src/vnc-client.ts`、`web/src/lib/novnc-rfb.ts`、`web/src/novnc-rfb.d.ts`

- [ ] **Step 1: api.ts**

`DesktopLink` 替换为：

```ts
export type LiveLink = {
  mode: 'managed' | 'remote' | 'cloud' | 'host'
  url: string
  hint?: string
}
```

`environments` 对象里 `browserDesktop` 替换为：

```ts
  browserLive: () =>
    api<LiveLink>('/v1/me/environments/browser/live-link'),
```

- [ ] **Step 2: BrowserPage.tsx**

`openDesktop`（92-105 行）替换为：

```tsx
  async function openLive() {
    setBusy(true)
    setError(null)
    try {
      const link = await environments.browserLive()
      if (!link.url) {
        setError(link.hint || 'No live view for this browser source')
        return
      }
      window.open(link.url, '_blank', 'noopener,noreferrer')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'live view link failed')
    } finally {
      setBusy(false)
    }
  }
```

按钮（209-216 行）改为：

```tsx
                <Button
                  type="tertiary"
                  size="small"
                  disabled={busy}
                  onClick={() => void openLive()}
                >
                  Open live view
                </Button>
```

同文件里提到 QEMU/VNC 的说明文案（约 152 行、约 360 行）改为：`One fixed browser per user: a Roundpen-managed browserless Chrome container (or the configured LAN/cloud source). Agents drive Chrome over CDP; open the live view to watch or take over.`

- [ ] **Step 3: SettingsPage.tsx 加「测试连接」**

在 Browser 设置区（`settings.browser.cdpProvider` 附近）加按钮与结果展示：

```tsx
  const [browserTest, setBrowserTest] = useState<string>('')
  ...
  async function testBrowser() {
    setBrowserTest('…')
    try {
      const res = await api<{ ok: boolean; error?: string; result?: { provider: string; version?: string; path?: string; chromePath?: string } }>(
        '/v1/admin/settings/browser/test',
        { method: 'POST' },
      )
      if (!res.ok) {
        setBrowserTest(res.error || 'connection failed')
        return
      }
      const r = res.result || ({} as any)
      setBrowserTest(
        r.provider === 'host'
          ? `host chrome: ${r.chromePath || 'not found'}`
          : `provider=${r.provider}${r.version ? ` browserless=${r.version}` : ''}${r.path ? ` path=${r.path}` : ''}`,
      )
    } catch (e) {
      setBrowserTest(e instanceof Error ? e.message : 'connection failed')
    }
  }
```

按钮放在 provider 选择器之后：

```tsx
                <Button size="small" theme="borderless" onClick={() => void testBrowser()}>
                  {t('settings.browser.test')}
                </Button>
                {browserTest && <Typography.Text type="tertiary">{browserTest}</Typography.Text>}
```

i18n（`web/src/i18n/en.ts` 与 `zh_CN.ts` 同步加键）：

```ts
  'settings.browser.test': 'Test connection',
  'settings.cdp.docker': 'Roundpen-managed container (browserless/chrome)',
  'settings.cdp.host': 'Chrome on the roundpend host',
  'settings.cdp.remote': 'LAN / self-hosted browserless',
  'settings.cdp.cloud': 'Commercial cloud browser (CDP URL + token)',
```

（`zh_CN.ts` 对应中文：`测试连接`、`Roundpen 托管容器（browserless/chrome）`、`roundpend 主机上的 Chrome`、`局域网 / 自建 browserless`、`商业云浏览器（CDP 地址 + token）`。）

- [ ] **Step 4: 删 noVNC**

```bash
git rm web/vnc.html web/src/vnc-client.ts web/src/lib/novnc-rfb.ts web/src/novnc-rfb.d.ts
cd web && npm uninstall @novnc/novnc
```

- [ ] **Step 5: 构建前端**

```bash
cd web && npm run build 2>&1 | tail -5
# 期望：构建成功，无对 vnc 的引用报错
```

- [ ] **Step 6: 提交**

```bash
git add -A web
git commit -m "feat(web): browser live view entry replaces the noVNC desktop"
```

---

## Phase E — 清理

### Task 13: 删除 QEMU Browser 链路（Go 侧）

**Files:**
- Delete: `images/browser-qemu/`、`internal/rfbtest/`
- Modify: `internal/hostsetup/{runner,store,actions,plan,llm_plan}.go`、`Makefile`、`internal/backend/qemu/qemu.go:523-529`

- [ ] **Step 1: 删文件**

```bash
git rm -r images/browser-qemu internal/rfbtest
git rm Makefile.browser-image 2>/dev/null || true
```

- [ ] **Step 2: Makefile**

删除 `browser-image:` 目标（Task 1 已加 `browser-driver`）；检查 README 提到的 `make browser-image` 是否还有引用（Task 15 处理文档）。

- [ ] **Step 3: hostsetup**

- `actions.go`：删 `ActionInstallQEMU`、`ActionBuildBrowserImage` 两个常量与 `hostsetupActions` 里对应条目（7-8 行、42-49 行）。
- `runner.go`：删 `ActionBuildBrowserImage` 的 case（64-66 行）、`ActionInstallQEMU` 的 case（58-62 行）与 `installQEMUArgv`（79-84 行）；`FindRepoRoot` 的判定 marker 从 `images/browser-qemu/build.sh` 改为 `go.mod`（118 行），注释同步更新。
- `store.go`：删 `f.BinariesOK`/`f.BrowserImageOK` 赋值（82-89 行）、`ActionBuildBrowserImage` 的 case（220-221 行）与 `internal/backend/qemu` import。
- `plan.go`：删 `BinariesOK`/`BrowserImageOK` 字段（30-31 行）与 `needBrowser` 相关分支（42、48-53、63-67 行）；`code_browser` preset 只剩「Docker ready + browser image pull」一条路径：

```go
	if ctx.Preset == "code_browser" && !f.DockerReady {
		actions = append(actions, planned(hostsetup.ActionInstallDocker, "需要准备本机 Docker 环境", priv))
	}
```

- `llm_plan.go`：删 `ActionBuildBrowserImage` 分支（34-37 行），把允许的动作列表（83 行）注释改为 `install_docker`，facts 打印去掉 `binariesOK/browserImageOK`。

- [ ] **Step 4: qemu backend 的 browser 特判**

`internal/backend/qemu/qemu.go:523-529`：

```go
func useVirtioWorkspace(opts backend.CreateOpts) bool {
	slot := strings.ToLower(strings.TrimSpace(opts.Slot))
	if slot == "" {
		slot = strings.ToLower(strings.TrimSpace(opts.Env["ROUNDPEN_SLOT"]))
	}
	return slot != "mobile"
}
```

（browser 不再走 qemu；mobile 保留。）

- [ ] **Step 5: 构建与测试**

```bash
go build ./... && go vet ./...
go test ./internal/hostsetup/... ./internal/backend/qemu/... ./tests/... 2>&1 | tail -20
# 期望：PASS；如 hostsetup 测试引用已删字段，按测试意图删除对应断言（不再有 qcow2 概念）
```

- [ ] **Step 6: 提交**

```bash
git add -A
git commit -m "chore(browser): drop the QEMU browser chain (images, hostsetup, make target)"
```

### Task 14: 删除 VNC 用例与 uismoke RFB；e2e 修复

**Files:**
- Modify: `tests/uismoke/main.go`、`web/e2e/*`
- Delete: `web/e2e/vnc.spec.ts`

- [ ] **Step 1: uismoke**

- 删 RFB 相关：`rfbtest` import、`sock`/`ln` 建立与关闭（30-36 行）、`vncSock` 类型（238-240 行）、`envapi.Handler` 的 `VNC:` 字段。
- `slotEnvs` 的 `EnsureBrowser` 返回类型改为 `*userenv.BrowserTarget`：

```go
func (s *slotEnvs) EnsureBrowser(_ context.Context, _ string) (*userenv.BrowserTarget, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ensureErr != nil {
		err := s.ensureErr
		s.ensureErr = nil
		return nil, err
	}
	if s.browser == nil {
		s.browser = &sandbox.Sandbox{
			ID:       "sb-browser",
			Name:     "browser-admin",
			Status:   sandbox.StatusRunning,
			Metadata: map[string]string{"browserToken": "uismoke-token"},
		}
	}
	return &userenv.BrowserTarget{Key: s.browser.ID, Provider: "docker", Managed: true, Sandbox: s.browser}, nil
}
```

  并补上 `BrowserTarget(ctx, userID)` 方法（同实现，供 live-link 路径用）。
- 模板 stub 的 `"browser-desktop"` 改为 `"browser"`（88 行附近）。
- uismoke 需要一个假的上游 debugger，供 live 反代有东西可连：

```go
	// Fake browserless debugger upstream for the live view smoke test.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".js") {
			w.Header().Set("Content-Type", "application/javascript")
			io.WriteString(w, "export const ok = true")
			return
		}
		w.Header().Set("Content-Type", "text/html")
		io.WriteString(w, `<html><head><title>Browserless debugger</title></head><body>stub debugger</body></html>`)
	}))
	defer upstream.Close()
	upHost, upPort, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	upPortInt, _ := strconv.Atoi(upPort)
```

  然后 `envapi.Handler` 注入 `Dial: fakeDialer(...)`（同 `live_test.go` 的 `fakeDialer`，uismoke 内再定义一份）与 `Cfg: &config.Config{CDP: config.CDPConfig{Provider: config.CDPProviderDocker}}`；`Dial` 的实现忽略 `destPort`，固定连 `upHost:upPort`。

- [ ] **Step 2: e2e**

```bash
git rm web/e2e/vnc.spec.ts
```

`web/e2e/helpers.ts` 里 `desktopWsUrl` 等 VNC 助手删除；`web/e2e/browser.spec.ts` 中依赖 desktop 的断言改为断言 live-view 按钮的返回（uismoke 下 `live-link` 返回 `{mode:"managed", url:"/v1/me/environments/browser/live/"}`）。新增一条：

```ts
test('browser page exposes the live view entry', async ({ page }) => {
  await login(page)
  await page.goto('/browser')
  await expect(page.getByRole('button', { name: 'Open live view' })).toBeVisible()
})
```

（`login` 用 helpers 里现有的登录助手。）

- [ ] **Step 3: 跑 e2e**

```bash
cd web && npx playwright test 2>&1 | tail -20
# 期望：全部通过（不需要外部浏览器；uismoke 本地起）
```

- [ ] **Step 4: 提交**

```bash
git add -A tests web
git commit -m "test(e2e): drop VNC coverage; smoke the browser live view entry"
```

### Task 15: 文档

**Files:**
- Delete/rewrite: `docs/architecture/qemu-browser.md` → `docs/architecture/browser-env.md`
- Modify: `README.md:12-14,24,30-31,38,41,45,49,56,83,116,122`、`docs/architecture/project-layout.md`、`docs/architecture/environment-services.md`

- [ ] **Step 1: 重写架构文档**

```bash
git mv docs/architecture/qemu-browser.md docs/architecture/browser-env.md
```

新内容按 spec §3/§4 写：四种来源表格、托管容器（镜像/env/token/端口/生命周期）、Playwright 引擎与 driver 落地、实时视图（live 路由 + 上游 debugger + 截图接管兜底）、配置项表、许可提醒（browserless 商业 license、Google Chrome 条款）、部署前置（Docker + 镜像 pull；host 模式需要主机 Chrome）。

- [ ] **Step 2: README 等**

- README 能力表：`Browser | QEMU | XFCE + Chrome；CDP + 主机 VNC→WebSocket` → `Browser | Docker | browserless/chrome 容器；CDP + 自带 debugger 实时视图（来源可配：托管/局域网/云/本机）`。
- 删除「Browser 槽位需要宿主机 qemu-system-x86_64、qemu-img 与 make browser-image」段落（83 行），改为「Browser 槽位需要 Docker，首次使用会拉取 `ghcr.io/browserless/chrome:v2.56.7`；开发机首次跑引擎会自动安装 Playwright driver（`make browser-driver`）」。
- 第 13、14、38、116 行的「Browser→QEMU」表述同步更新。
- `project-layout.md`：`images/browser-qemu/` 条目删除；`backend/qemu` 注释改为「Desktop/Mobile 槽位落点（Browser 已迁 Docker）」。
- `environment-services.md`：Profile 表 `browser` 行改为 `dev + Browser（browserless 容器 + Playwright 引擎）`；Terminal 实现指引里 QEMU 行保留（Desktop/Mobile），但把 Browser 从 QEMU 例子中移除。

- [ ] **Step 3: 校验文档无死链**

```bash
grep -rn "qemu-browser\|browser-image\|browser-desktop" --include="*.md" . | grep -v node_modules | grep -v "docs/superpowers"
# 期望：无输出（历史 plan/spec 文档除外）
```

- [ ] **Step 4: 提交**

```bash
git add -A docs README.md
git commit -m "docs(browser): browser env architecture, config, and license notes"
```

---

## Phase F — 验收

### Task 16: 全量验收

- [ ] **Step 1: Go 全量测试**

```bash
go build ./... && go vet ./... && go test ./... 2>&1 | tail -30
# 期望：PASS（DB/网络 gated 用例 skip）
```

- [ ] **Step 2: gated 引擎测试（LAN 实机）**

```bash
ROUNDPEN_TEST_BROWSER_WS=ws://10.10.1.3:3000/chrome go test ./internal/browser/ -run TestPlaywrightEngine -v
# 期望：PASS
```

- [ ] **Step 3: web 全量**

```bash
cd web && npm run build && npx playwright test 2>&1 | tail -20
# 期望：PASS
```

- [ ] **Step 4: 手工验收（对照 spec §9）**

1. `remote` 指向 LAN：Browser 页显示来源与 v2.56.7；跑一次 explore 任务；接管面板截图 + 像素输入可用；「打开上游 debugger」能打开。
2. 托管（devbox）：`provider=docker` → Ensure 拉起容器 → 状态 running；`browser_*` 工具全绿；「Open live view」经 `/v1/me/environments/browser/live/` 打开 debugger 并能看到会话、接管；Stop/Start/Delete 回收容器。
3. `host`：装 Chrome 的机器上可导航/截图/接管；未装时设置页测试连接给出明确提示。
4. 升级路径：伪造一条旧 qcow2 browser 沙箱记录（或保留旧实例），升级后 ensure 自动删除重建为容器。

- [ ] **Step 5: 更新 spec 状态并提交**

```bash
sed -i 's/^状态：待评审/状态：已实现/' docs/superpowers/specs/2026-09-13-browser-docker-playwright-design.md
git add docs/superpowers/specs/2026-09-13-browser-docker-playwright-design.md
git commit -m "docs(specs): mark browser docker+playwright design as implemented"
```

---

## 风险与回滚

| 风险 | 触发点 | 处理 |
|---|---|---|
| debugger 反代在真实会话接管时出问题（browserless #4224）| Task 11/16 手工验收 | 保留截图接管面板为兜底；必要时按 spec §4.4 兜底 B 实现 screencast |
| alpine + apk nodejs 版本不满足 Playwright driver | Task 1 之后第一次跑 `deploy/Dockerfile` 构建 | 构建期验证：容器内 `node --version` ≥ 20；不满足则基础镜像升到 alpine 3.22 |
| 局域网/云来源无 token 或 token 失效 | Task 7 测试连接 | 提示明确错误；endpoint 支持显式带 `?token=` |
| 旧 QEMU browser 沙箱 adopt 失败 | Task 16 升级验收 | 既有 `errSlotFailed` → 删除重建；如重建失败，`POST /v1/me/environments/browser/ensure` 会返回错误而不是黑屏 |
| 回滚 | — | 分支 `feat/browser-docker-playwright` 整体 revert；数据面无 schema 变更 |

## Self-Review 记录

- **Spec 覆盖**：§4.1 provider→Task 5/6/7；§4.2 托管容器→Task 8/9/10；§4.3 引擎→Task 1/2/3/4；§4.4 实时视图→Task 11/12；§4.5 API/设置/UI→Task 6/7/11/12；§4.6 删除清单→Task 13/14；§4.7 升级→Task 16；§5 配置→Task 5/8；§6 测试→Task 2/11/14/16；spike→已完成并记录在计划头部。
- **类型一致性**：`BrowserTarget{Key,Provider,Managed,Sandbox}` 在 Task 6 定义，Task 9/11/12 使用同一组字段；`pwAttach{Endpoint,Token,LaunchPath,Width,Height}` 在 Task 2 定义，Task 3 使用；`ProbeCDP` 返回的 `Path/Version/Playwright` 与 Task 7 设置接口字段一致。
- **已知待执行时确认点**（不阻塞计划）：`internal/userenv` 现有测试的 fake 构造函数确切名称；`settings.Service.current()` 的确切读取方法名；`internal/api/auth` 测试里放入用户上下文的确切助手名。执行时按文件现状照抄，不改变上述行为断言。
