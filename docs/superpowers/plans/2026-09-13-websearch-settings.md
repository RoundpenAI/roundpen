# Web 工具设置（Tavily endpoint + Key，live 生效）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 把 WebSearch 的 Tavily 配置从 env-only 搬进设置页（管理员「Web 工具」区块），保存后新开的 Agent 会话即带 `WebSearch`，无需重启 daemon；env 作为初始值/兜底。

**Architecture:** 数据面完全复用 LLMGW 设置模式（`AppSettings` 字段 → 掩码/保留 → 校验 → `ApplyToConfig` → 解码兜底）；生效面把 `manager.SysDeps.WebSearchEndpoint/WebSearchAPIKey` 两个字符串换成 `WebSearch func() (endpoint, key string)`（仿 `DefaultModel`），`manager.Start` 构建 registry 时读取——registry 本就按会话构建，天然 live。

**Tech Stack:** Go 1.26（`internal/settings`、`internal/acp/manager`）、React + Semi UI（`web/src/pages/SettingsPage.tsx`）、i18n（en / zh_CN 键必须成对）。

**Spec:** `docs/superpowers/specs/2026-09-13-websearch-settings-design.md`

**分支:** 在 `master` 上执行（`feat/agent-upgrade` 已合并，PR #15）。开始前确认 `git branch --show-current` 输出 `master`，工作区干净（仅 `.worktrees/` 未跟踪）。

---

### Task 1: settings 数据面（字段 / 掩码 / 校验 / 兜底 / 写回）

**Files:**
- Modify: `internal/settings/settings.go`
- Modify: `internal/settings/decode.go`
- Test: `internal/settings/websearch_test.go`（新建）

- [ ] **Step 1: 写失败测试**

创建 `internal/settings/websearch_test.go`（package `settings`）：

```go
package settings

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/config"
)

func TestWebSearchSettingsFromConfigAndApply(t *testing.T) {
	cfg := &config.Config{WebTools: config.WebToolsConfig{
		SearchEndpoint: "https://api.tavily.com",
		SearchAPIKey:   "tvly-env",
	}}
	s := FromConfig(cfg)
	if s.WebSearchEndpoint != "https://api.tavily.com" || s.WebSearchApiKey != "tvly-env" {
		t.Fatalf("FromConfig = %+v", s)
	}
	s.WebSearchEndpoint = "https://search.internal.example/"
	s.WebSearchApiKey = "tvly-new"
	if err := ApplyToConfig(&s, cfg); err != nil {
		t.Fatalf("ApplyToConfig: %v", err)
	}
	if cfg.WebTools.SearchEndpoint != "https://search.internal.example/" || cfg.WebTools.SearchAPIKey != "tvly-new" {
		t.Fatalf("cfg.WebTools = %+v", cfg.WebTools)
	}
}

func TestWebSearchSecretMaskAndMerge(t *testing.T) {
	s := AppSettings{WebSearchApiKey: "tvly-secret"}
	if got := s.SanitizeForResponse().WebSearchApiKey; got != SecretMask {
		t.Fatalf("masked = %q", got)
	}
	s.WebSearchApiKey = SecretMask
	s.MergeSecrets(AppSettings{WebSearchApiKey: "tvly-stored"})
	if s.WebSearchApiKey != "tvly-stored" {
		t.Fatalf("merge = %q", s.WebSearchApiKey)
	}
	s.WebSearchApiKey = ""
	s.MergeSecrets(AppSettings{WebSearchApiKey: "tvly-stored"})
	if s.WebSearchApiKey != "tvly-stored" {
		t.Fatalf("empty submit must keep stored key, got %q", s.WebSearchApiKey)
	}
}

func TestWebSearchEndpointValidation(t *testing.T) {
	base := AppSettings{
		DefaultImage:           "ghcr.io/x/y:1",
		DefaultTtlSeconds:      60,
		PreviewTokenTtlSeconds: 60,
		TemplateBuilder:        "docker",
		CDPProvider:            "auto",
		CDPPort:                3000,
	}
	ok := base
	ok.WebSearchEndpoint = "https://api.tavily.com"
	if err := ok.Validate(); err != nil {
		t.Fatalf("valid endpoint rejected: %v", err)
	}
	empty := base
	if err := empty.Validate(); err != nil {
		t.Fatalf("empty endpoint must be valid: %v", err)
	}
	bad := base
	bad.WebSearchEndpoint = "api.tavily.com"
	if err := bad.Validate(); err == nil {
		t.Fatal("endpoint without scheme must fail validation")
	}
}

func TestDecodeAppSettingsKeepsWebSearchFallback(t *testing.T) {
	fallback := AppSettings{WebSearchEndpoint: "https://api.tavily.com", WebSearchApiKey: "tvly-env"}
	got, err := DecodeAppSettings([]byte(`{"defaultImage":"img"}`), fallback)
	if err != nil {
		t.Fatalf("DecodeAppSettings: %v", err)
	}
	if got.WebSearchEndpoint != "https://api.tavily.com" || got.WebSearchApiKey != "tvly-env" {
		t.Fatalf("fallback lost: %+v", got)
	}
	got2, err := DecodeAppSettings([]byte(`{"webSearchApiKey":"tvly-db"}`), fallback)
	if err != nil {
		t.Fatalf("DecodeAppSettings: %v", err)
	}
	if got2.WebSearchApiKey != "tvly-db" {
		t.Fatalf("stored value not applied: %+v", got2)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/settings/ -run 'TestWebSearch|TestDecodeAppSettingsKeepsWebSearchFallback' -count=1`
Expected: FAIL（`s.WebSearchEndpoint undefined` 等编译错误）

- [ ] **Step 3: 实现**

`internal/settings/settings.go` 四处修改：

(a) `AppSettings` 结构体末尾（`CDPPort int \`json:"cdpPort"\`` 之后）加：

```go
	WebSearchEndpoint       string `json:"webSearchEndpoint"`
	WebSearchApiKey         string `json:"webSearchApiKey"`
```

(b) `FromConfig` 的 `CDPPort: cfg.CDP.Port,` 之后加：

```go
		WebSearchEndpoint:       cfg.WebTools.SearchEndpoint,
		WebSearchApiKey:         cfg.WebTools.SearchAPIKey,
```

(c) `SanitizeForResponse` 的 `out.CDPToken = MaskSecret(s.CDPToken)` 之后加：

```go
	out.WebSearchApiKey = MaskSecret(s.WebSearchApiKey)
```

`MergeSecrets` 的 `s.CDPToken = ResolveSecret(s.CDPToken, previous.CDPToken)` 之后加：

```go
	s.WebSearchApiKey = ResolveSecret(s.WebSearchApiKey, previous.WebSearchApiKey)
```

(d) `ApplyToConfig` 的 `cfg.CDP = config.CDPConfig{...}` 赋值**之前**加：

```go
	cfg.WebTools = config.WebToolsConfig{
		SearchEndpoint: strings.TrimSpace(s.WebSearchEndpoint),
		SearchAPIKey:   strings.TrimSpace(s.WebSearchApiKey),
	}
```

(e) `Validate` 的 `if _, err := config.ParseVirtualKeys(s.LlmgwVirtualKeys); err != nil {` 之后加（并在 import 块补 `"net/url"`）：

```go
	if ep := strings.TrimSpace(s.WebSearchEndpoint); ep != "" {
		u, err := url.Parse(ep)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("webSearchEndpoint must be an http(s) URL")
		}
	}
```

`internal/settings/decode.go`：在 `if _, ok := keys["cdpProvider"]; !ok { ... }` 块之后加：

```go
	if _, ok := keys["webSearchEndpoint"]; !ok {
		out.WebSearchEndpoint = fallback.WebSearchEndpoint
		out.WebSearchApiKey = fallback.WebSearchApiKey
	}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `go test ./internal/settings/ -count=1`
Expected: 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/settings/settings.go internal/settings/decode.go internal/settings/websearch_test.go
git commit -m "feat(settings): store web search endpoint and key with masking"
```

---

### Task 2: HTTP 往返与掩码保留（DB-gated 测试）

**Files:**
- Test: `internal/settings/http_test.go`（扩展 `TestAdminSettingsHTTP`）

- [ ] **Step 1: 扩展测试**

在 `TestAdminSettingsHTTP` 里，把 PUT 的 body 换成带新字段的版本：

```go
	body, _ := json.Marshal(settings.AppSettings{
		DefaultImage:           "python",
		DefaultTtlSeconds:      3600,
		PreviewTokenTtlSeconds: 900,
		TemplateBuilder:        "docker",
		WebSearchEndpoint:      "https://search.internal.example",
		WebSearchApiKey:        "tvly-db",
	})
```

PUT 之后（现有 `if cfg.DefaultImage != "python"` 断言之后）加：

```go
	if cfg.WebTools.SearchEndpoint != "https://search.internal.example" || cfg.WebTools.SearchAPIKey != "tvly-db" {
		t.Fatalf("cfg.WebTools = %+v", cfg.WebTools)
	}
```

再在函数末尾（`TestAdminSettingsHTTP` 内）追加掩码往返：

```go
	// GET 返回掩码后的 Key。
	req = httptest.NewRequest(http.MethodGet, "/v1/admin/settings", nil)
	req.Header.Set("X-API-Key", "rp-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", rec.Code, rec.Body.String())
	}
	var envelope struct {
		Settings settings.AppSettings `json:"settings"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode GET: %v", err)
	}
	if envelope.Settings.WebSearchApiKey != settings.SecretMask {
		t.Fatalf("GET must mask the stored key, got %q", envelope.Settings.WebSearchApiKey)
	}

	// 掩码值原样 PUT 回来不得覆盖已存 Key。
	body, _ = json.Marshal(envelope.Settings)
	req = httptest.NewRequest(http.MethodPut, "/v1/admin/settings", bytes.NewReader(body))
	req.Header.Set("X-API-Key", "rp-admin")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("masked PUT status=%d body=%s", rec.Code, rec.Body.String())
	}
	if cfg.WebTools.SearchAPIKey != "tvly-db" {
		t.Fatalf("masked PUT must keep the stored key, got %q", cfg.WebTools.SearchAPIKey)
	}
```

注意：GET 响应的外层信封键名以 `internal/settings/http.go` 的实际 JSON tag 为准（应为 `"settings"`）；如果实现里不是该键名，按实际调整后同步本测试。

- [ ] **Step 2: 运行测试**

Run: `go test ./internal/settings/ -run TestAdminSettingsHTTP -count=1 -v`
Expected: 无 `DATABASE_URL`/`ROUNDPEN_TEST_DATABASE_URL` 时 SKIP（`testDB` helper 的门禁）；本机有 pg0 时应 PASS。若环境可用，用：
`ROUNDPEN_TEST_DATABASE_URL="postgres://roundpen:roundpen@127.0.0.1:5432/roundpen_test?sslmode=disable" go test ./internal/settings/ -run TestAdminSettingsHTTP -count=1 -v`

- [ ] **Step 3: 提交**

```bash
git add internal/settings/http_test.go
git commit -m "test(settings): cover web search key masking round-trip"
```

---

### Task 3: manager live getter + main.go 接线

**Files:**
- Modify: `internal/acp/manager/manager.go`（SysDeps 字段 + Start 读取）
- Modify: `cmd/roundpend/main.go`（getter 接线 + 启动日志）
- Test: `internal/acp/manager/manager_test.go`

- [ ] **Step 1: 写失败测试**

在 `internal/acp/manager/manager_test.go` 末尾追加（`sync/atomic` 已在 import 中）：

```go
func TestStartReadsWebSearchGetter(t *testing.T) {
	llm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer llm.Close()

	var calls atomic.Int32
	m := manager.New(slog.Default(), noopMgr{}, providers.Default(), manager.SysDeps{
		LoopbackBase: llm.URL,
		LLMKey:       "vk-test",
		DefaultModel: func() string { return "gpt-test" },
		WebSearch: func() (string, string) {
			calls.Add(1)
			return "https://api.tavily.com", "tvly-test"
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := m.Start(ctx, "sess-web", "", "sysadmin", manager.StartOpts{
		AutoApprove: true,
		Actor:       manager.Actor{Username: "u", Role: "user", APIKey: "k"},
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer m.Stop("sess-web")

	if calls.Load() == 0 {
		t.Fatal("WebSearch getter must be read when a runtime starts")
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `go test ./internal/acp/manager/ -run TestStartReadsWebSearchGetter -count=1`
Expected: FAIL（`unknown field WebSearch`）

- [ ] **Step 3: 实现**

`internal/acp/manager/manager.go`：

(a) `SysDeps` 中把

```go
	WebSearchEndpoint string // Tavily 兼容搜索 endpoint（空 = 默认 https://api.tavily.com）
	WebSearchAPIKey   string // 二者任一非空即注册 WebSearch 工具
```

替换为

```go
	WebSearch func() (endpoint, key string) // nil 或返回空表示未配置 → 不注册 WebSearch
```

(b) `Start` 中把

```go
		tools.RegisterWebSearch(reg, &tools.WebSearchBinder{
			Endpoint: m.sys.WebSearchEndpoint,
			APIKey:   m.sys.WebSearchAPIKey,
			HTTP:     webClient,
		})
```

替换为

```go
		webSearchEndpoint, webSearchAPIKey := "", ""
		if m.sys.WebSearch != nil {
			webSearchEndpoint, webSearchAPIKey = m.sys.WebSearch()
		}
		tools.RegisterWebSearch(reg, &tools.WebSearchBinder{
			Endpoint: webSearchEndpoint,
			APIKey:   webSearchAPIKey,
			HTTP:     webClient,
		})
```

`cmd/roundpend/main.go`：

(c) 把现有的启动日志块

```go
	if cfg.WebTools.SearchEndpoint != "" || cfg.WebTools.SearchAPIKey != "" {
		endpoint := cfg.WebTools.SearchEndpoint
		if endpoint == "" {
			endpoint = "(default)"
		}
		logger.Info("web search enabled",
			"endpoint", endpoint,
			"api_key_set", cfg.WebTools.SearchAPIKey != "")
	}
```

替换为（改为读 Settings 的 live 值；`settingsSvc` 定义在 L294，早于 L324 的此处）：

```go
	if s := settingsSvc.Current(); s.WebSearchEndpoint != "" || s.WebSearchApiKey != "" {
		endpoint := s.WebSearchEndpoint
		if endpoint == "" {
			endpoint = "(default)"
		}
		logger.Info("web search enabled",
			"endpoint", endpoint,
			"api_key_set", s.WebSearchApiKey != "")
	}
```

(d) 把 `manager.SysDeps` 字面量里的

```go
		WebSearchEndpoint: cfg.WebTools.SearchEndpoint,
		WebSearchAPIKey:   cfg.WebTools.SearchAPIKey,
```

替换为

```go
		WebSearch: func() (string, string) {
			s := settingsSvc.Current()
			return s.WebSearchEndpoint, s.WebSearchApiKey
		},
```

- [ ] **Step 4: 运行测试与构建**

Run: `go build ./... && go test ./internal/acp/... -count=1`
Expected: 构建通过；manager 测试（含新用例）与 sysagent/tools 全部 PASS

- [ ] **Step 5: 提交**

```bash
git add internal/acp/manager/manager.go internal/acp/manager/manager_test.go cmd/roundpend/main.go
git commit -m "feat(agent): read web search config live from settings"
```

---

### Task 4: 设置页「Web 工具」区块

**Files:**
- Modify: `web/src/lib/appNav.ts`
- Modify: `web/src/api.ts`
- Modify: `web/src/pages/SettingsPage.tsx`
- Modify: `web/src/i18n/en.ts`
- Modify: `web/src/i18n/zh_CN.ts`

- [ ] **Step 1: appNav（类型联合 + 区块数组）**

`web/src/lib/appNav.ts`：

```ts
export type SettingsSectionKey =
  | 'git'
  | 'agent'
  | 'general'
  | 'preview'
  | 'builds'
  | 'browser'
  | 'llmgw'
  | 'webtools'
  | 'system'
```

`SETTINGS_SECTIONS` 数组在 `{ key: 'llmgw', ... }` 之后插入一行：

```ts
  { key: 'webtools', labelKey: 'settings.section.webtools', admin: true },
```

- [ ] **Step 2: api.ts 类型**

`web/src/api.ts` 的 `AppSettings` 中，`llmgwVirtualKeys: string` 之后加：

```ts
  webSearchEndpoint: string
  webSearchApiKey: string
```

- [ ] **Step 3: SettingsPage（默认值 + 区块 JSX）**

`web/src/pages/SettingsPage.tsx`：

`emptySettings` 中 `llmgwVirtualKeys: '',` 之后加：

```ts
  webSearchEndpoint: '',
  webSearchApiKey: '',
```

在 `{section === 'system' && (` 之前插入新区块（结构与 llmgw 区块一致）：

```tsx
        {section === 'webtools' && (
          isAdmin && loading ? (
            <Loading tip={t('settings.loadingSystem')} />
          ) : isAdmin ? (
            <Form labelPosition="top" labelAlign="left" style={sectionGap}>
              <div style={{ ...sectionGap, paddingTop: 16 }}>
                <Typography.Text type="tertiary">
                  {t('settings.webtools.intro')}
                </Typography.Text>
                <div
                  style={{
                    display: 'grid',
                    gap: 16,
                    gridTemplateColumns:
                      'repeat(auto-fit, minmax(220px, 1fr))',
                  }}
                >
                  <Field
                    label={t('settings.webtools.endpoint')}
                    hint={t('settings.webtools.endpointHint')}
                  >
                    <Input
                      inputMode="url"
                      autoComplete="off"
                      placeholder="https://api.tavily.com"
                      value={form.webSearchEndpoint}
                      onChange={(v) => patch({ webSearchEndpoint: v })}
                    />
                  </Field>
                  <Field
                    label={t('settings.webtools.apiKey')}
                    hint={t('settings.webtools.keepSecret')}
                  >
                    <Input
                      mode="password"
                      autoComplete="new-password"
                      placeholder={t('settings.browser.keepMasked')}
                      value={form.webSearchApiKey}
                      onChange={(v) => patch({ webSearchApiKey: v })}
                    />
                  </Field>
                </div>
                <Typography.Text type="tertiary" size="small">
                  {t('settings.webtools.note')}
                </Typography.Text>
              </div>
            </Form>
          ) : null
        )}
```

（若 `Typography` 在该文件中未导入，按 llmgw 区块已有用法无需新增导入；`Field`、`Input`、`Form`、`Loading`、`sectionGap`、`patch`、`t` 均为文件内既有符号。）

- [ ] **Step 4: i18n（en + zh_CN 键必须成对）**

`web/src/i18n/en.ts`：在 `'settings.section.llmgw': 'LLM gateway',` 之后加

```ts
  'settings.section.webtools': 'Web tools',
```

在 llmgw 区块之后（文件内任意同一对象内位置，建议紧跟 `'settings.llmgw.secretsNote'` 之后）加

```ts
  'settings.webtools.intro':
    'Web tools let Agents fetch pages and search the web from the control plane. Configure the Tavily-compatible search endpoint below; WebFetch needs no configuration.',
  'settings.webtools.endpoint': 'Search endpoint',
  'settings.webtools.endpointHint':
    'Tavily-compatible API base URL. Leave empty to use https://api.tavily.com. Loopback addresses (127.0.0.1) are rejected.',
  'settings.webtools.apiKey': 'Search API key',
  'settings.webtools.keepSecret': 'Leave masked to keep the stored secret.',
  'settings.webtools.note':
    'Saving applies to new Agent sessions — reopen a session to see the WebSearch tool. Leave both empty to disable WebSearch.',
```

`web/src/i18n/zh_CN.ts`：对应键

```ts
  'settings.section.webtools': 'Web 工具',
```

```ts
  'settings.webtools.intro':
    'Web 工具让 Agent 从控制面抓取网页并联网搜索。在下方配置 Tavily 兼容的搜索端点；WebFetch 无需配置。',
  'settings.webtools.endpoint': '搜索端点',
  'settings.webtools.endpointHint':
    'Tavily 兼容 API 的 Base URL。留空使用 https://api.tavily.com。环回地址（127.0.0.1）会被拒绝。',
  'settings.webtools.apiKey': '搜索 API 密钥',
  'settings.webtools.keepSecret': '保持掩码以保留已存储的密钥。',
  'settings.webtools.note':
    '保存后对新开的 Agent 会话生效——重开会话即可看到 WebSearch 工具。两项都留空则关闭 WebSearch。',
```

- [ ] **Step 5: 类型检查与构建**

Run: `cd web && npm run build`
Expected: `tsc` 与 vite 构建通过（i18n 键缺一即类型错误）

- [ ] **Step 6: 提交**

```bash
git add web/src/lib/appNav.ts web/src/api.ts web/src/pages/SettingsPage.tsx web/src/i18n/en.ts web/src/i18n/zh_CN.ts
git commit -m "feat(web): add Web tools settings section"
```

---

### Task 5: 文档同步与全量回归

**Files:**
- Modify: `.env.example`
- Modify: `docs/superpowers/specs/2026-09-13-webfetch-websearch-tools-design.md`（§5 补一句配置来源变更）
- Modify: `docs/architecture/acp-agent-ui.md`（工具表 WebSearch 行的配置说明）

- [ ] **Step 1: 文档**

`.env.example` 中的 Web tools 注释块替换为：

```
# Web tools: WebSearch (Tavily-compatible). Prefer Settings → Web tools in the UI;
# these vars only seed the first boot and the settings row overrides them.
# Loopback endpoints (127.0.0.1) are rejected by the outbound dial guard.
# ROUNDPEN_WEB_SEARCH_ENDPOINT=https://api.tavily.com
# ROUNDPEN_WEB_SEARCH_API_KEY=tvly-...
```

`docs/superpowers/specs/2026-09-13-webfetch-websearch-tools-design.md` §5 的配置段末尾追加一句：

```
> 后续迭代（`2026-09-13-websearch-settings-design.md`）已把配置迁到设置页（管理员「Web 工具」），env 仅作首次 Bootstrap 的初始值。
```

`docs/architecture/acp-agent-ui.md` 工具表 WebSearch 行改为：

```
| `WebSearch` | 联网搜索（Tavily，只读；Settings → Web tools 配置，未配置则不注册） |
```

- [ ] **Step 2: 全量回归**

Run: `go vet ./... && go test ./... -count=1`
Expected: 全部 ok（既有 env-gated skip 不变）

- [ ] **Step 3: 提交**

```bash
git add .env.example docs/superpowers/specs/2026-09-13-webfetch-websearch-tools-design.md docs/architecture/acp-agent-ui.md
git commit -m "docs: point web search configuration at the settings page"
```

---

## 完成标准（对照 spec）

1. 设置页出现管理员区块「Web 工具」，可填 Endpoint（可空 = 默认 Tavily）与 API Key；Key 掩码显示、掩码提交不覆盖。
2. 保存非空配置后，**新开**的 Agent 会话工具面包含 `WebSearch`（旧会话不变）；两项都空则不注册。
3. `ROUNDPEN_WEB_SEARCH_*` 仍作为首次 Bootstrap 的初始值，设置页保存后以 DB 为准。
4. 旧设置行（无新字段）解码兜底后行为与升级前一致。
5. `go vet ./...`、`go test ./...`、`cd web && npm run build` 全绿。
