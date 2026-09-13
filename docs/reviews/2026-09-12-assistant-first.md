# 代码走查：assistant-first 分支（系统助手 + sysadmin 进程内 Agent）

> 日期：2026-09-12
> 范围：分支 `feat/assistant-first` 相对 `origin/feat/assistant-first` 的 21 个提交，外加工作区未提交改动（`internal/assistant/*`、`internal/api/agentapi/*`、`internal/acp/sysagent/*`、`web/src/lib/session*`、`web/src/pages/ChatSessionPage.tsx` 等）
> 方法：静态走查，重点核对 sysadmin 进程内 Agent 的工具面、自动批准、浏览器 hub 归属、会话/助手所有权、前端渲染 XSS
> 本文记录走查结论，供后续修复使用；不是对外宣传稿。

## 结论

本次改动引入 `assistant.Kind` 系统助手、把新对话的默认 provider 切到**进程内 sysadmin**（`DefaultAssistantProvider = "sysadmin"`），并把浏览器 hub 从「会话独立」改为「按用户共享」。底层路径约束（工作区文件 `..`/symlink 防护、Bash/Glob/Grep argv 传参）、鉴权所有权检查（`loadOwnedSession`、`ownedAssistant`）、bcrypt、cookie session、前端 React 文本节点渲染等既有防线仍然成立。

核心风险集中在一处：**sysadmin 进程内 Agent 的可变工具默认全自动批准、无任何人确认边界**，叠加「工具结果原样回灌 LLM 上下文」的提示注入面，和「浏览器按用户共享 + 无内网 URL 限制」的 SSRF 面。对单用户本机部署是「自己打自己」；一旦走多账号、把控制面挂到不可信网络、或 Agent 浏览攻击者页面，即可变为实际越权 / 内网探测。

| 严重度 | 数量 | 含义 |
|--------|------|------|
| 高危 | 2 | 无确认边界的自主 Agent + 自动批准；浏览器无内网限制的 SSRF |
| 中危 | 3 | 全局并发门限跨用户 DoS；运行时永不回收；浏览器按用户共享的串扰 |
| 低危/设计 | 3 | provider 判断不一致；列表接口写放大；系统助手可被改名改能力 |

---

## 安全相关问题

### 高危

#### S1. 进程内 sysadmin 智能体对可变工具默认全自动批准，无人工确认边界

sysadmin 现为新建助手会话的默认 provider（`internal/api/agentapi/handler.go:134`），进程内运行，工具面含沙箱 Bash（`/bin/sh -c`）、Write/Edit、`browser_evaluate`（任意 JS）等全部 `Mutating` 工具。权限链路：

- `handler.go:368` 启动运行时传 `AutoApprove: true`；
- `handler.go:418-419` 服务端 `autoMode := true` 且无条件 `rt.SetAutoApprove(true)`；
- `internal/acp/client/perm.go:11` 的 `PickOrdinaryAllow` 直接返回首个 `allow_once` 选项；
- 前端 `readAutoMode()` 默认也是 true（`web/src/pages/ChatSessionPage.tsx:75-82`）。

即每次调用都被自动批「允许一次」，用户在 `internal/acp/sysagent/agent.go:193-221` 的权限请求实际上从不弹窗（除非手动关掉 auto）。

**后果**：智能体零确认地自主行动。配合提示注入——`agent.go:256-261` 把工具输出（网页快照、文件内容、bash stdout）原样 append 回 LLM 上下文——任何被浏览的攻击者页面或仓库都能操纵该智能体：`browser_navigate` 到恶意站点 → 页面指令注入 → 自动批准地执行 Bash / 往用户已登录站点 `browser_type` 输入 / 提交表单。用户感知「助手去看了个网页」，实际它在全程自动点击、跑命令。

**建议**：默认只对只读工具自动批准；Bash / 写文件 / 浏览器输入至少保留一次显式确认；或为 sysadmin 工具面区分「低风险」与「高风险」白名单。

#### S2. 浏览器工具任意 URL 导航 = SSRF，无内网限制且自动批准

`internal/acp/sysagent/tools/browser.go:79-104` 的 `browser_navigate` 对 URL 完全不做校验（仅 `browsetask` 入口走了 `MustStartURL`；`internal/browser/explore.go:93-99` 的 `ValidateStartURL` 也只要求 http(s)+host，私有/回环/云元数据地址全部放行）。`browser_explore` 同理。浏览器跑在宿主上的 QEMU VM，可经网关（如 `10.0.2.2`）触达宿主机内网服务。

**后果**：被提示注入的智能体 `browser_navigate("http://10.0.2.2:<port>/...")` 探测宿主机内部服务，再以 `browser_snapshot` / `browser_evaluate` 读取响应内容，通过自身回复外传。这是带「渲染 JS + 读页面」能力的 SSRF，比纯 curl 危险。

**建议**：`browser_navigate` / `browser_explore` 与 `ValidateStartURL` 一致，并加内网/回环/链路本地/云元数据地址拦截；对 agent 可导航的 URL 做可配置白名单。

---

### 中危

#### S3. 全局 3 槽 prompt 并发门限为跨用户共享，无每用户配额 → 拒绝服务

`internal/acp/manager/manager.go:25` `maxConcurrentPrompts = 3`，`manager.go:90` 为全局共享 `gate`；每个 prompt 用 `context.WithTimeout(context.Background(), 10*time.Minute)`（`handler.go:620`）占槽最长 10 分钟。所有 sysadmin 智能体又共用 `llmgw.InternalVirtualKey`（`manager.go:82`）调 LLM，无每用户身份/配额。

**后果**：一个用户开 N 个会话刷 prompt，可占满全局 3 槽并耗尽服务端模型预算，阻塞其他所有用户的智能体。缺少每用户限流与配额。

**建议**：门限按用户分片或加每用户并发上限；LLM 调用带用户身份用于配额；对会话创建/消息发送加限流。

#### S4. 运行时 map 永不回收 + 会话/助手创建无速率限制 → 内存与 goroutine 泄漏

`manager.go:242-244` `runtimes[sessionID]` 只增不减，`StartForAssistant`/`createSession` 会立刻启动运行时并常驻内存（管道 + ACP 读循环 goroutine）。会话删除才 `Stop`，没有任何 eviction。

**后果**：长期运行 + 任意创建会话（见 S3，无速率限制），内存/goroutine 随会话数线性增长直至 OOM。

**建议**：运行时按空闲 TTL 回收；限制每用户活跃会话数。

#### S5. 浏览器改为按用户共享：跨会话状态串扰，任一会话可查看/操控共享浏览器

`internal/api/agentapi/browser.go:46-57` 的 `hubKey`（及 `ensureHubKey`）解析到该用户的共享 Browser 沙箱（`userenv.BrowserSandboxID` / `EnsureBrowser`），不再是会话独立；`browserStatus`/`browserScreenshot`/`browserInput` 对用户任意一个会话都落到同一个 hub。所有权仍由 `loadOwnedSession` 兜底，未越权到其他用户。

**后果**：同一用户运行两个并发智能体会抢同一浏览器（Agent A 的导航会打断 B）；用户在会话 B 的浏览器面板可看到会话 A 中智能体正在浏览的页面并截图/输入。同用户内属隐私/设计缺陷，不是跨用户漏洞。

**建议**：明确「每用户一浏览器」的产品语义；如需并发，改为会话级或增加互斥/可见性提示。

---

### 低危 / 设计问题

#### D1. `ensureSession` 用 `providers.Default()` 而非实际配置的 providers

`internal/assistant/http.go:221`：判断既有会话是否可复用时查硬编码默认表，与 `h.ACP.Providers()`（配置后）可能不一致；叠加 `DefaultAssistantProvider` 硬编码 `"sysadmin"`（`handler.go:134`）。运维自定义 provider 后复用判断失准，可能反复新建会话或报 `unknown provider`。

**建议**：复用判断改走 `h.ACP.Providers()`；provider 选择与默认值收敛到配置。

#### D2. 每次 `GET /v1/assistants` 都无条件执行 `AttachOrphanSessions` 写库

`internal/assistant/http.go:54-64` + `store.go:256-265`：每个列表请求都跑一条 `UPDATE agent_sessions SET assistant_id=..., updated_at=now()`（即便 0 行受影响），并把「无 assistant_id 的会话」永久挂到系统助手名下（含通用 agent-sessions API 建的 `assistantId:""` 会话）。写放大 + 会话被静默重新归属，系统助手 primary session 可能指向任意「新建对话」。

**建议**：改为「确保有 orphan 且尚未挂到系统助手时才写」或延迟到创建/打开会话时处理；`updated_at` 只在真正改动时更新。

#### D3. 系统助手仅拦截 disable，仍可被用户改名/改能力

`internal/assistant/assistant.go:112-117` 的 `validateDisable` 只阻止 `status=disabled`；PATCH 仍允许改系统助手 name/bio/capabilities/directory grants。既然目标是「常驻不可删入口」，前端隐藏删除按钮的约束可被直接调 API 绕过（属自伤，非越权）。

**建议**：系统助手的 name/bio/identityMode 设为不可变；删除/禁用双通道统一拦截。

---

## 已核对、防御有效（未列为问题）

- 工作区文件路径约束：`internal/workspace/local/local.go:108-203` 的 `resolve`/`confinePath` 拒绝 `..` 逃逸并做 symlink 检查；`internal/acp/sysagent/tools/path.go` 的 `ResolveWorkspacePath` 正确；`internal/acp/client/client.go` 的 `guestToRel` 虽可返回越界 rel，被 workspace 层兜底。
- Bash/Glob/Grep 均以 argv + `--` 传参（`shell.go:167`、`search.go:89/132`），无 shell 注入；Glob/Grep 的 `$PATTERN` 经位置参数传入。
- 密码 bcrypt cost 12（`internal/api/auth/password.go:14`）；session token 哈希入库（`middleware.go:141`）。
- 前端 `credentials: include` cookie，无 localStorage 明文密钥；全 `web/src/` 无 `innerHTML` / `dangerouslySetInnerHTML`，工具输出经 React 文本节点渲染，无存储型 XSS。
- 会话/助手所有权检查存在：`loadOwnedSession`（`agentapi/browser.go:21-42`）、`ownedAssistant`（`assistant/http.go:250-259`）；browsetask、assist-ticket 均按用户名隔离。
- 浏览器 hub 归属按用户（`userenv.go`），未出现跨用户浏览器访问；`loadOwnedSession` 拦截了跨用户 IDOR。

---

## 建议修复顺序

1. 收敛自动批准（S1）：只读工具白名单 + 高风险工具默认请求确认；同步服务端 `autoMode` 默认值。
2. 浏览器 URL 拦截（S2）：统一走 `ValidateStartURL` 并加内网/回环黑名单。
3. 每用户并发门限与配额（S3），配合运行时 TTL 回收（S4）。
4. provider 判断与列表写放大收口（D1、D2）；系统助手不可变（D3）。

## 验证时未做的事

- 未做动态利用（未对运行实例发攻击请求）。
- 未审计 `web/` 依赖漏洞与 Semi 内部渲染器对不可信 markdown 的行为（已确认无 `innerHTML`，未进一步验证 `semi-markdownRender` 是否渲染 raw HTML）。
- 未逐条验证浏览器 VM 的宿主网络可达性（QEMU 网关假设基于默认 user-mode 网络）。
- 修复后建议补：Agent 权限确认的端到端测试、浏览器导航内网拦截测试、多会话共享浏览器的行为测试。
