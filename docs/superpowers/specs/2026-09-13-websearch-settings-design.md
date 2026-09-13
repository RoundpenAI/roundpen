# 设计：Web 工具设置（Tavily endpoint + Key，live 生效）

> 日期：2026-09-13
> 状态：待评审
> 范围：`internal/settings`（存储 / 掩码 / 校验 / 解码兜底）/ `internal/acp/manager`（live getter）/ `cmd/roundpend/main.go`（接线与启动日志）/ 设置页 UI（新区块 + i18n）

## 背景与目标

上一轮（`2026-09-13-webfetch-websearch-tools-design.md`）把 WebSearch 做成 env-only：只有设置 `ROUNDPEN_WEB_SEARCH_ENDPOINT` / `_API_KEY` 时才注册工具，未配置则模型工具列表里没有 `WebSearch`。实测中用户只看到 `WebFetch`，且在设置页找不到配置入口。

目标：把 WebSearch 的配置搬进设置页（管理员级，与 LLMGW / CDP 同级），保存后**新开的 Agent 会话立即生效，无需重启 daemon**；env 继续可用，作为初始值/兜底。

## 需求结论（澄清结果）

1. 设置页新增「Web 工具」区块：`Endpoint`（可选，空 = 默认 `https://api.tavily.com`）+ `API Key`（掩码显示，与 LLMGW 的 Key 相同交互）。
2. live 生效：工具注册发生在**会话 Start**（`manager.Start` 构建 registry）。保存后新开会话即带 `WebSearch`；已在运行的会话保持原工具面（不做热更新已注册的工具）。
3. env 仍有效：`ROUNDPEN_WEB_SEARCH_*` 在首次 Bootstrap 时播种进设置；设置页保存后以 DB 为准（与 `llmgwOpenai*` 等既有字段同语义）。

## 方案选择

采用**方案 A：复用 LLMGW 设置模式 + live getter。**

- 数据面照抄既有模式：`AppSettings` 加两个字段 → `SanitizeForResponse` 掩码 → `MergeSecrets` 保留 → `Validate` 校验 → `ApplyToConfig` 落回 `cfg.WebTools` → `DecodeAppSettings` 兜底旧行。
- 生效面把 `manager.SysDeps.WebSearchEndpoint/WebSearchAPIKey` 两个字符串换成 `WebSearch func() (endpoint, key string)`（仿 `DefaultModel func() string` 的绑定方法模式），`Start` 时读取——天然 live，无需 manager 持有可变状态或锁。
- 备选 B（`RuntimeDeps` 钩子 + manager setter）：需要在 manager 维护可变字段与锁，且 registry 本来就是按会话构建的，getter 更简单 → 否。
- 备选 C（只做 env）：用户已否。

## 设计

### 1. 数据与 API（`internal/settings`）

`AppSettings` 新增：

```go
WebSearchEndpoint string `json:"webSearchEndpoint"`
WebSearchApiKey   string `json:"webSearchApiKey"`
```

- `FromConfig`：从 `cfg.WebTools.SearchEndpoint/SearchAPIKey` 播种。
- `SanitizeForResponse`：`WebSearchApiKey` 非空 → `MaskSecret`（`●●●●●●●●`）。
- `MergeSecrets`：提交为空或掩码值时保留旧值（`ResolveSecret`）。**`WebSearchApiKey` 是有意例外**：提交掩码 = 保留原值，提交空串 = 显式清除（设置页清空输入框即可关闭 WebSearch，与 UI 文案一致）；JSON 中缺省的字段在 `DecodeAppSettings` 阶段已回填当前值，不会被误清空。
- `Validate`：`WebSearchEndpoint` 非空时必须能被解析为 **http/https** URL，否则报错（`webSearchEndpoint must be an http(s) URL`）；Key 不联网校验。
- `ApplyToConfig`：写回 `cfg.WebTools.SearchEndpoint/SearchAPIKey`（保持 boot/apply 路径一致）。
- `DecodeAppSettings`：两个新字段加入 fallback 表（旧行缺 key 时取 fallback，与 `llmgw*` 同）。
- HTTP 面不变：`GET/PUT /v1/admin/settings`（`RequireAdmin`），PUT 仍是部分字段 + `DecodeAppSettings(raw, Current())`。

### 2. 生效路径（live）

- `manager.SysDeps`：

```go
WebSearch func() (endpoint, key string) // nil 或返回空表示未配置 → 不注册 WebSearch
```

- `manager.Start`：`endpoint, key := "", ""; if m.sys.WebSearch != nil { endpoint, key = m.sys.WebSearch() }`，再交给 `RegisterWebSearch`（未配置时不注册的判定仍在 tools 层）。
- `cmd/roundpend/main.go`：`WebSearch: func() (string, string) { s := settingsSvc.Current(); return s.WebSearchEndpoint, s.WebSearchApiKey }`（`Current()` 为 RWMutex 保护的值拷贝，线程安全）。
- 启动日志改为读 `Current()`（在 `Bootstrap()` 之后）打印一次启用状态；设置页每次 PUT 已有 audit 记录，无需额外日志。

### 3. UI（设置页）

- `web/src/lib/appNav.ts`：新增区块 `{key: 'webtools', labelKey: 'settings.section.webtools', admin: true}`。
- `web/src/pages/SettingsPage.tsx`：
  - `emptySettings` 与 `AppSettings` 类型（`web/src/api.ts`）加 `webSearchEndpoint` / `webSearchApiKey`；
  - `section === 'webtools'` 渲染两个字段：Endpoint（`Input inputMode="url"`，placeholder 提示默认 Tavily）、API Key（`Input mode="password"`，掩码值时 placeholder 用既有 `settings.browser.keepMasked`）。
- i18n：`web/src/i18n/en.ts` 与 `zh_CN.ts` 同步新增：区块名（Web tools / Web 工具）、两个字段 label、endpoint 的 hint（空 = 默认 Tavily）。

### 4. 测试

- `internal/settings`：decode 兜底（旧行缺新字段）、`SanitizeForResponse` 掩码、`MergeSecrets` 保留、`Validate` 非法 endpoint 报错、`ApplyToConfig` 写回。
- `internal/settings/http_test.go`（DB-gated）：PUT 两个字段后 GET 返回掩码 Key；再 PUT 掩码值不覆盖（沿用既有用例扩展）。
- `internal/acp/manager`：`Start` 会调用 `WebSearch` getter（计数 stub 断言被调用）；getter 为 nil 时不注册（既有行为，无需断言内部 registry）。
- tools 层表面测试已覆盖"未配置不注册 / 配置即注册"，不改。

## 非目标（本期不做）

- 每用户独立 Key（仅管理员全局设置）
- 保存时联网校验 Key（不调 Tavily 试搜）
- 对已运行会话热更新工具面
- WebFetch 的可配置项（UA / 超时 / 大小上限）

## 风险与边界

- **旧行兼容**：`DecodeAppSettings` 未加兜底会让老库读出的新字段为零值——已在设计中显式列出并有测试。
- **掩码误伤**：用户真实 Key 恰好等于掩码串（`●●●●●●●●`）时会被当作"未修改"——与 LLMGW 既有行为一致，接受。
- **生效时机**：用户保存后在**已打开**的会话里仍看不到 WebSearch，需要新建会话（或重开）；UI hint 与文档需说明。
- **env 与设置页竞争**：Bootstrap 只在 DB 无行时播种 env；一旦用户在设置页保存过，env 不再覆盖（与既有字段同语义）。
