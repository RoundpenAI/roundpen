# 设计：System Agent WebFetch / WebSearch 工具

> 日期：2026-09-13
> 状态：已评审待实施
> 范围：`internal/acp/sysagent/tools`（新增 web 工具）/ `internal/acp/manager`（接线）/ `internal/config`（Tavily 配置）/ 系统提示与文档

## 背景与目标

上一轮（`2026-09-12-agent-builtin-workspace-tools-design.md`）把 `WebFetch`、`WebSearch` 列为当期非目标。本轮补齐这两个 Claude Code 风格工具。参考实现位于 `~/dev/claude-code/packages/builtin-tools/src/tools/WebFetchTool` 与 `WebSearchTool`。

目标：System Agent 的模型工具面新增 `WebFetch`（抓取 URL → HTML 转 Markdown → 按 prompt 提炼）与 `WebSearch`（Tavily 搜索），与既有 `Read` / `Write` / `Edit` / `Glob` / `Grep` / `Bash` / `browser_*` 同级注册，遵守上一轮的语言规则（模型可见文案不出现 sandbox / guest / QEMU 等词）。

## 需求结论（澄清结果）

1. **搜索后端**：Tavily。参考实现的默认后端；已实测 `api.tavily.com` 从部署机可达，`duckduckgo.com` 被网络策略拦截。端到端验证需要用户提供 API Key。
2. **WebFetch 语义**：完整对齐参考实现——抓取 → HTML 转 Markdown → 经 llmgw 用小模型按 `prompt` 提炼后返回。大页面不进主循环上下文；代价是每次抓取多一次模型调用。
3. **权限与出站安全**：两个工具均为只读（`Mutating: false`，与 `Read` / `Glob` / `Grep` 同级，不弹权限框）；默认拦截环回地址与 link-local（含云元数据 `169.254.169.254`），内网网段与公网放行。

## 方案选择

采用方案 A：**控制面 Go 原生实现**。两个工具注册在 `internal/acp/sysagent/tools`，抓取与搜索都在控制面进程内完成，不经过 guest。

- 选它的原因：控制面才能做地址过滤（SSRF 防护）、Markdown 转换与经 llmgw 的二次模型调用；与上一轮 "structured builtins" 方向一致。
- 方案 B（复用 Browser/CDP 抓页面）被否：无 Markdown 转换、成本高、与 `browser_*` 职责混淆。后续可作增强（HTML 抓取失败时回退浏览器渲染），本轮不做。
- 方案 C（guest 内 `curl` 实现）被否：无过滤、无转换、无法调 llmgw。

## 设计

### 1. 工具面

| 工具 | 用途 | Mutating | 参数 |
|------|------|----------|------|
| `WebFetch` | 抓取 URL 内容并按 prompt 提炼 | no | `url`（必填）、`prompt`（可选） |
| `WebSearch` | 联网搜索（Tavily） | no | `query`（必填）、`allowed_domains?`、`blocked_domains?`、`num_results?`（默认 8） |

`WebSearch` 仅在配置了 Tavily（见 §5）时注册——未配置时不注册，模型看不到一个必然报错的工具。

### 2. WebFetch 管线

1. **校验**：URL 可解析、长度 ≤ 2000、scheme 仅 http / https（拒绝 `file:` / `data:` / `ftp:` 等）。
2. **抓取**：GET，`Accept: text/markdown, text/html, */*`，自定义 UA（`Roundpen-WebFetch/0.1`），30s 超时，响应体上限 10MB。走 §4 的防护 client。
3. **内容分支**（按 `Content-Type`）：
   - `text/html` / `application/xhtml+xml` → 用 `github.com/JohannesKaufmann/html-to-markdown` 转换为 Markdown；
   - `text/*`、`application/json`、`application/xml`、`text/markdown` 等文本类型 → 原文使用；
   - 其余（PDF、图片、二进制）→ 返回明确错误，不落盘（非目标）。
4. **截断**：Markdown 正文截到 100K 字符（对齐参考实现 `MAX_MARKDOWN_LENGTH`），附截断提示。
5. **提炼**：`prompt` 提供时，构造「正文 + prompt + 简洁作答要求」的输入，经 `ModelRunner`（llmgw 默认模型，60s 超时）返回模型输出（截断到 32KB）；`prompt` 未提供时直接返回正文（同样截断到 32KB，与 Bash 结果同量级）。HTTP 非 2xx、内容类型不支持、二次模型失败都返回工具错误。

### 3. WebSearch（Tavily）

- 请求：`POST {endpoint}/search`，`Authorization: Bearer {key}`，30s 超时。请求体：

```json
{
  "query": "...",
  "search_depth": "basic",
  "max_results": 8,
  "include_domains": [],
  "exclude_domains": []
}
```

- 校验：`query` 至少 2 字符；`allowed_domains` 与 `blocked_domains` 不得同时提供；`num_results` 钳制到 1..20。
- 结果格式化（模型可见文本）：

```
Web search results for query: "..."

- [Title](URL): snippet
- ...

Include the sources above in your response as markdown links.
```

- 空结果显示 "No search results found."；HTTP 错误与非 2xx 返回工具错误。

### 4. 安全（出站防护）

- 自建 `http.Client`：`net.Dialer.Control` 校验**实际拨号的 IP**（DNS 解析之后，防 DNS rebinding）：拒绝环回（`127.0.0.0/8`、`::1`）、link-local（`169.254.0.0/16`、`fe80::/10`）、unspecified、multicast；内网网段（`10/8`、`172.16/12`、`192.168/16`、IPv6 ULA）与公网放行。用 `net.IP` 的标准方法判断（含 IPv4-mapped IPv6 形式）。
- 重定向：最多 10 跳，每跳经同一拨号校验（`Control` 天然覆盖，无需特殊处理）。
- 构造函数 `NewWebHTTPClient(opts WebClientOptions)`，`WebClientOptions{AllowLoopback bool}` 仅供测试使用（httptest 监听 127.0.0.1），生产恒为 `false`，不接入环境变量。
- 两个工具都是只读，不进入权限门；不做参考实现的 preapproved-domain 白名单——我们没有按域名粒度的权限规则，该机制无对应物。

### 5. 配置与接线

- `internal/config`：新增 `WebToolsConfig{SearchEndpoint, SearchAPIKey}`，环境变量 `ROUNDPEN_WEB_SEARCH_ENDPOINT`（默认 `https://api.tavily.com`）、`ROUNDPEN_WEB_SEARCH_API_KEY`。二者任一显式设置即启用（支持无 Key 的内网 Tavily 兼容代理，此时不带 `Authorization` 头）；都未设置则不注册 `WebSearch`。
- `manager.SysDeps` 增加 `WebSearchEndpoint` / `WebSearchAPIKey` 两个字符串字段（避免 manager 依赖 config 包），`cmd/roundpend/main.go` 从 `WebToolsConfig` 传入。启用状态在 daemon 启动时打一条 info 日志（endpoint + 是否带 Key），便于发现"只配了一半"的部署（如只设 endpoint 导致请求无鉴权）。
- tools 包不能 import sysagent（sysagent 已 import tools），因此 `ModelRunner` 接口定义在 tools 包：

```go
// ModelRunner 执行一次纯文本 LLM 调用（无工具），用于 WebFetch 二次提炼。
type ModelRunner interface {
    Run(ctx context.Context, system, user string) (string, error)
}
```

- `sysagent.LLMConfig` 实现导出方法 `Run`（内部复用 `chat`，`tools=nil`）；`manager.go` 把 `LLMConfig` 的构造从 `sysagent.New(...)` 调用点提出来，先用于 `RegisterWebFetch`，再传给 sysagent。
- 注册函数：`RegisterWebFetch(r *Registry, b *WebBinder)`、`RegisterWebSearch(r *Registry, b *WebSearchBinder)`，与既有 `RegisterShell` / `RegisterFiles` / `RegisterSearch` 风格一致。

### 6. 提示词与文案

- `history.go` 系统提示新增一段：WebFetch 抓取单个 URL 并按问题提炼（没有浏览器会话与 Cookie，登录墙页面会失败）；WebSearch 查最新信息并给出处。
- `docs/architecture/acp-agent-ui.md` 工具表补两行。
- `web/src/lib/toolStats.ts` 补两个工具的 UI 文案（若该文件按工具名分支）。

### 7. 与参考实现的有意偏离

| 点 | 参考实现 | 本设计 | 原因 |
|----|----------|--------|------|
| `prompt` 参数 | 必填 | 可选；缺省返回正文 | 我们的模型未必受过训练，缺省路径更鲁棒 |
| http→https 升级 | 自动升级 | 不升级，按原 scheme 抓取 | 内网部署大量 plain http 服务 |
| 跨域重定向 | 返回 "REDIRECT DETECTED"，要求模型重新发起 | 自动跟随（≤10 跳，逐跳校验） | 我们没有按域名的权限规则，重新发起无意义 |
| preapproved 域名 | 白名单免权限 | 不做 | 无域名粒度权限规则 |
| 抓取缓存 | 15 分钟 LRU | 不做（非目标） | YAGNI |
| 二进制内容 | 落盘并附提示 | 直接报错 | 非目标 |
| 二次模型 guideline | 含 125 字符引用上限等法务条款 | 只保留「仅依据正文、简洁作答」 | Anthropic 内部政策，Roundpen 不适用 |
| 搜索适配器 | api / bing / brave / exa / tavily 可插拔 | 只接 Tavily | YAGNI；后续新增后端只动 `websearch.go` 内部 |

## 非目标（本期不做）

- 二进制/PDF 落盘到 workspace
- 抓取结果缓存
- Browser 渲染回退（JS 页面抓取）
- Tavily 高级参数（`livecrawl`、`search_type`、`context_max_characters`）与多搜索后端
- 按域名的权限规则 / 审批提示
- 代理、证书校验例外等出站策略配置

## 测试计划

- **SSRF 防护**：表驱动测拨号校验函数（环回 / link-local / 元数据 / unspecified / multicast 拒绝；内网 / 公网放行；IPv4-mapped IPv6 拒绝）；生产 client 对 127.0.0.1 的真实请求被拒。
- **WebFetch 管线**（httptest，`AllowLoopback: true`）：HTML→Markdown；json/txt 直通；二进制报错；100K 截断；URL 校验（`file:` 等拒绝）；非 2xx 报错。
- **二次模型**：假 `ModelRunner` 断言正文与 prompt 都进入模型输入、工具返回其输出；`prompt` 缺省时不调用模型。
- **WebSearch**（httptest 假 Tavily）：断言请求体（query / max_results / include_domains / exclude_domains / Bearer 头）与结果格式化、空结果、非 2xx、双域名列表报错、`num_results` 钳制。
- **工具面**：扩展 `registry_surface_test.go`——两工具的名称与描述、描述不含 sandbox/guest/QEMU 等禁词、未配置时无 `WebSearch`、两工具均 `Mutating: false`。
- 沿用 TDD：先写测试再实现。

## 依赖与前置

- 新依赖：`github.com/JohannesKaufmann/html-to-markdown`（v1.6.0，`goproxy.cn` 可拉取，已实测）。
- 端到端验证需要 `ROUNDPEN_WEB_SEARCH_API_KEY`（Tavily）；单测用 stub，不依赖 Key。Key 仅经环境变量注入，不入库、不进提交。
- 二次模型调用复用 llmgw（`ROUNDPEN_LLMGW_*` 已配置）。

## 风险与边界

- **llmgw 不可用**：`prompt` 路径的工具调用失败并如实报错；缺省路径（无 prompt）不受影响。
- **大页面**：转换后正文超 100K 截断，可能丢失页面尾部信息；与参考实现同量级。
- **内网放行是刻意选择**：Agent 可以访问内网地址（与它已能执行 `Bash` 的能力一致）；环回与元数据仍被拦截，云环境凭据不被触及。
- **反爬**：部分站点对非浏览器 UA 返回 403/JS 挑战；本轮不解决（Browser 回退列为后续增强）。
- **Tavily 可用性**：网络策略若拦截 `api.tavily.com`，`WebSearch` 返回工具错误；可将 endpoint 指向内网兼容代理。
