# 飞牛（fnOS）统一网关用户认证 —— 实施方案

分支：`feat/fn-native-build`
状态：决策已定稿 / 待实施（Momus 已评审通过文件引用；本次决策更新后可在实施前再做一次复核）
工作量：Large（3 天+）

## 目标（Goal）

让 **飞牛 fnOS 打包版**（`.fpk`）支持「用飞牛账号登录」：不同飞牛用户登录时，**自动映射为本项目的不同用户**，并继承本项目已有的**逐用户数据隔离**。其他渠道（Docker Compose、裸机单二进制、移动端）**行为逐字节不变**，理想情况下连这段代码都**不编译进去**。

## 约束（Constraints）

- 飞牛认证**只在飞牛打包时生效**；其他渠道零影响、零编译产物变化。
- 不破坏现有：密码登录、API Key、移动端 Bearer session、预览子域、WebSocket。
- 复用现有鉴权栈（`auth.IssueSession` / `auth.Middleware` / `storage.UserStore`），session 层不改。
- **API 面（`:9527`）保持按 Bearer/API Key 鉴权**，与网关（按飞牛会话）正交；飞牛用户在 API 面用 API Key 登录。

## 已锁定决策（Locked Decisions）

1. **用户名映射**：`本地 username = "fnos-" + sanitize(X-Trim-Userid)`（例 `fnos-1000`）。以数字 uid 为稳定键，天然唯一、永不碰撞（消除隔离缺口 G2）；`FullName = X-Trim-Username` 承载可读名。
2. **管理员**：`X-Trim-Isadmin == "true"` → 本项目 `role=admin`，逐请求 reconcile。
3. **门禁机制**：Go build tag `fnos`（编译期排除）+ 运行期 env 激活。
4. **拓扑（双监听）**：Web 管理页走**统一网关**（`/app/roundpen`，Unix Socket，SSO，不给控制台配独立端口式入口）；**API**（移动端 App / CLI / 外部 LLM 客户端 / API Key 自动化）走**独立端口 `:9527`**。依据官方：管理页走网关、非 Web 管理的 API 允许独立端口。
5. **密码登录（含内置 `admin`）：不隐藏**。保留 Web 登录页与登出；支持**通过独立端口 `:9527` 用 `admin`+密码登录 Web 控制台**。网关路径下 fnOS 用户仍自动 SSO，但登录页对 `admin` 依然可用。（该做法与飞牛「优先网关」的政策不完全一致，但飞牛并未绝对禁止；用户明确要求保留。）
6. **移动端 / CLI 登录**：飞牛用户在本地无密码（`password_hash` 为空 → 密码登录恒失败）→ 改用 **API Key**（`rp-...`）当 Bearer。用户在网关 SSO 登录后的管理页生成 API Key，填入移动端/CLI。移动端登录页需支持「填 API Key」。（后续如需调整再说。）
7. **网关自动开户不遮蔽显式登录**：`gatewayIdentity` 仅对非鉴权端点自动 provisioning；对 `POST /v1/auth/login`、`POST /v1/auth/register`、`POST /v1/auth/logout` 等公共鉴权端点**不拦截**，避免遮蔽 `admin`+密码等显式登录。

## 背景事实（Background Facts）

- 飞牛「用户认证」**不是 OIDC/OAuth2/JWT**。统一网关在把请求转发到应用 **Unix Socket** 时注入 3 个头：`X-Trim-Userid`（数字 uid）、`X-Trim-Isadmin`（`true`/`false`）、`X-Trim-Username`。无 token、无 JWKS、无 discovery、无 claims（无 email/roles）。
- 网关入口由 `app/ui/config` 声明 `gatewayPrefix`（`/app/roundpen`）+ `gatewaySocket`（`app.sock`，位于 `${TRIM_APPDEST}/target/`）；网关条目下 `protocol`/`port` 被忽略。
- 权威文档：https://developer.fnnas.com/llms-full.txt （AGENTS.md 指定）。
- 隔离审计结论：现有代码逐用户隔离**基本完备**（沙箱/工作区/记忆/会话/助手/事项/APIKey/预览/IM/Push/连接器/设置分层全部按 username）。仅 2 个接入前需处理的缺口：
  - **G1**：`internal/sandbox/manager.go` 的 `AttachExec(:688)` / `ResizeTerminal(:684)` 未走 `authorize`。
  - **G2**：`internal/workspace/userid.go` 的 `sanitizeWorkspaceUser` 归一化到 `[a-z0-9-]` 并截断 40 字符 → 不同用户名可能碰撞共享 workspace。**由决策 1 的 uid 派生名消除**（uid 为纯数字）。

---

## 待改动文件（Files to Change）

### 新增
- `internal/fnosgateway/doc.go`（**无 tag**，仅 `// Package fnosgateway ...` 注释 + `package fnosgateway`，防止 `go vet` 报「build constraints exclude all Go files」）
- `internal/fnosgateway/identity.go`（`//go:build fnos`）：`gatewayIdentity` 中间件、用户名派生、role reconcile、fail-closed 抢占防护
- `internal/fnosgateway/listener.go`（`//go:build fnos`）：`stripGatewayPrefix`、handler 链组装、Unix `net.Listen("unix", socket)` + `http.Server.Serve`
- `internal/fnosgateway/identity_test.go`（`//go:build fnos`）
- `cmd/roundpend/gateway_on.go`（`//go:build fnos`）：`installFnosGateway(cfg, users, sessions, mux, logger) (io.Closer, error)`
- `cmd/roundpend/gateway_off.go`（`//go:build !fnos`）：同签名，恒返回 `(nil, nil)`

### 修改
- `internal/config/config.go`：新增 `FnosGatewaySocket`（`ROUNDPEN_FNOS_GATEWAY_SOCKET`）、`FnosGatewayPrefix`（`ROUNDPEN_FNOS_GATEWAY_PREFIX`，默认 `/app/roundpen`）。字段始终编译、非飞牛渠道恒为空。
- `cmd/roundpend/main.go`（约 :334 `srv := &http.Server{...}` 后）：加一处 `gwCloser, err := installFnosGateway(...)`，`Shutdown` 后关闭。
- `internal/api/auth/session_handler.go`：`BootstrapHint`（:146）响应新增 `authMode` 字段（`"fnos-gateway"` / 默认）。
- TCP 监听器 handler 链：加无 tag 的 `stripGatewayHeaders`（删除所有 `X-Trim-*`）。
- **前端 base path**：
  - `web/vite.config.ts`：`base: process.env.VITE_BASE || '/'`
  - `web/src/api/client.ts`：导出 `apiUrl(path)` 并用 `import.meta.env.BASE_URL`；`api()` 内部改用它
  - `web/src/lib/pushNotifications.ts`、`web/src/api/agents.ts`（`browserScreenshotBlob`）：直连 `fetch('/v1/...')` 改用 `apiUrl`
  - `web/src/api/agents.ts`（`sessionWsUrl`）、`web/src/api/sandboxes.ts`（`terminalWsUrl`）、`web/src/pages/BrowserPage.tsx`：WS/URL 带 base
  - `web/src/App.tsx`：`BrowserRouter` 加 `basename={import.meta.env.BASE_URL.replace(/\/$/,'')}`
  - `web/index.html`、`web/public/manifest.json`（`start_url`/`scope`/`id`）、`sw.js`：路径适配 base（PWA 子路径需实测）
  - **不隐藏**登录/登出/密码/注册 UI（见决策 5）；`authMode` 仅作信息展示（可选），不用来隐藏 `admin` 登录入口
- `scripts/build-fnos-fpk.sh`：`go build` 加 `-tags fnos`（:94-95）；`npm run build` 前 `export VITE_BASE=/app/roundpen/`（:79-87）
- `deploy/fnos/app/ui/config`：**桌面入口改为网关条目**（`gatewayPrefix:"/app/roundpen"`, `gatewaySocket:"app.sock"`；`protocol`/`port` 留空，`type` 按 spike 结果），**控制台不再配端口式 `url`/`port` 入口**。
- `deploy/fnos/manifest`：**保留** `service_port=9527` + `checkport=true`（供 API 端口探活；网关条目本身不需要 service_port，但 `:9527` API 端口需要）。
- `deploy/fnos/README.md`（约 86-88 行「桌面入口 type 为 url、固定 9527」段）：更新为新的网关入口 + 「Web 走网关、`admin` 可经 `:9527` 密码登录」说明。
- `deploy/fnos/cmd/main`（`do_start`）、`install_callback`、`upgrade_callback`：写 `ROUNDPEN_FNOS_GATEWAY_SOCKET=${TRIM_APPDEST}/app.sock`、`ROUNDPEN_FNOS_GATEWAY_PREFIX=/app/roundpen`（**缺失才追加**，勿覆盖用户已编辑的 env）。
- **移动端支持 API Key 登录**：`mobile/lib/pages/login_page.dart` + `mobile/lib/api/endpoints.dart` + `mobile/lib/session/*`：登录页支持「API Key」方式，使用 `rp-...` 作为 Bearer（后端 middleware 已把 `rp-` 前缀的 Bearer 当 API Key 解析，无需改后端）。
- `.github/workflows/fnos-fpk.yml`：加 `go vet -tags fnos ./...` + `go test -tags fnos ./internal/fnosgateway/...`

---

## 认证中间件设计（核心逻辑）

Socket 监听器 handler 链（仅在飞牛模式、仅 Socket）：

```
stripGatewayPrefix(prefix) → gatewayIdentity(users, sessions) → auth.Middleware(users, sessions)(mux)
```

`gatewayIdentity` 逻辑：

```
0. 若路径是公共鉴权端点（POST /v1/auth/login、/v1/auth/register、/v1/auth/logout）→ 直接 next
   （不自动开户，避免遮蔽 admin+密码等显式登录；见决策 7）
1. uid := r.Header.Get("X-Trim-Userid")
   若为空 → 直接 next（回退现有密码/APIKey/session 登录）
2. 若已有有效 roundpen_session → 直接 next（避免每请求建 session）
3. username := "fnos-" + sanitize(uid)
   user, err := users.GetByUsername(ctx, username)
   ├─ 不存在        → Upsert 新建：AuthProvider="fnos", Role=从 Isadmin, FullName=X-Trim-Username,
   │                   Email="", APIKey=auth.APIKeyPrefix+auth.NewID()
   ├─ 存在且 provider=="fnos" → Isadmin 变化时仅更新 Role
   └─ 存在且 provider!="fnos" → fail-closed 401（防抢占，绝不覆盖本地账号）
4. plain, err := auth.IssueSession(w, r, sessions, user)   // 写库 + Set-Cookie
5. r.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: plain, Path: "/"})
   // 注入本次请求，使下游 auth.Middleware 经 resolveAuth 解析出用户
6. next
```

要点：
- 复用 `auth.IssueSession` / `auth.Middleware` / `storage.UserStore.Upsert` → WebSocket（浏览器自动带 Cookie）、`/v1/*`、bridge 全零改动。
- **只在变化时写库**（role reconcile 先比较）。
- **绝不修改内置 `admin`**（派生名带 `fnos-` 前缀天然隔离）。
- 登出：**不隐藏**。fnOS SSO 用户登出后下次请求会透明重建（等价以飞牛会话为准）；`admin`（通过独立端口密码登录）登出为正常语义。
- 外部/直连访问：Web 控制台在**独立端口 `:9527`** 仍可用，`admin`+密码可在此登录（决策 5）。

---

## 安全边界（Security）

| 场景 | 缓解 |
|---|---|
| TCP 伪造 `X-Trim-*` | `gatewayIdentity` 不在 TCP 链 + `stripGatewayHeaders` 兜底 |
| 同 NAS 其他应用连 Socket | Socket 属应用专用用户、目录 `0700`；信任边界 = Socket 本身 |
| isAdmin 提权 | 仅网关可设（决策 2：直接映射 admin） |
| 用户名抢占 | 保留 `fnos-` 命名空间；飞牛模式下注册拒绝 `fnos-`/`fnos_` 前缀；派生名已存在且非 fnos 提供方则 fail-closed |
| Cookie Secure | Socket 上 `httpx` 可能判为 http（https 下仍可用但弱）→ 在 Socket handler 显式设 scheme（watch-out） |

---

## 实施步骤（ordered）

0. **Spike（先去风险，必须先做）**：`deploy/fnos/app/ui/config` 加网关入口，Socket handler 临时打印 `r.URL.Path`/`r.Host`/`X-Trim-*`/`X-Forwarded-Proto`，真机装 fpk 打开 `/app/roundpen`，确认：① 网关是否剥前缀；② 是否转发 Cookie 与 WS upgrade。→ 剥前缀用容错 `strings.TrimPrefix` 兼容两种情况，但先确认避免 SPA base 与后端路由错配。
1. 修复隔离缺口 **G1**：`AttachExec`/`ResizeTerminal` 复用 `load()`（带 actor）。G2 由决策 1 消除。
2. `internal/config/config.go` 加字段。
3. `internal/fnosgateway/`（doc.go + identity.go + listener.go + test）。
4. `cmd/roundpend/gateway_on.go` / `gateway_off.go`；`main.go` 接一处。
5. TCP 链加 `stripGatewayHeaders`。
6. `BootstrapHint` 加 `authMode`（信息展示，不用于隐藏 UI）；`gatewayIdentity` 对公共鉴权端点放行（决策 7）。
7. 前端 base path（见上）；**不隐藏**登录/登出/密码 UI。
8. 打包与移动端：`build-fnos-fpk.sh`（`-tags fnos` + `VITE_BASE`）；`app/ui/config` 换网关入口；`manifest` 保留 `service_port`/`checkport`；`cmd/main`/`install_callback`/`upgrade_callback` 写新 env；移动端登录页加 API Key 方式。
9. `.github/workflows/fnos-fpk.yml` 补 `-tags fnos` 的 vet/test。
10. 真机验收。

## 验收标准（Verification）

- 不同飞牛用户登录 → 不同本项目用户（`fnos-<uid>`），且数据互相隔离（沙箱/工作区/记忆/会话/助手/事项）。
- 飞牛管理员 → 本项目 admin。
- Web 管理页经 `/app/roundpen` 打开即已登录（fnOS SSO）。
- **`admin` 账号不隐藏**：可通过独立端口 `:9527` 打开 Web 控制台并用 `admin`+密码登录（决策 5）。
- API 端口 `:9527`：内置 `admin` 密码登录、API Key、外部 LLM 客户端正常。
- 移动端/CLI 以 **API Key** 登录，映射到对应 `fnos-<uid>` 用户且隔离正确。
- 预览（路径式 `/p/{token}` 经网关；子域经 `:9527`）、WebSocket 正常。
- 非飞牛渠道：`go build`（无 tag）产物不含 `fnosgateway` 代码；`make build` / Docker 行为不变。
- `go vet ./...`（无 tag）与 `go vet -tags fnos ./...` 均通过。

## 风险与待确认（Risks / Open Questions）

- **前缀是否被剥**（唯一硬未知）→ 步骤 0 spike 确认。
- **Cookie `Secure`/scheme**：Unix socket 上 `httpx` 判定链拿不到受信代理 → 可能需配置化 scheme。
- **PWA 子路径**：`manifest.json` 的 `scope`/`start_url` 与 `sw.js` 注册路径需适配 base 并实测。
- **不要每请求重建 session**：确保「有效 Cookie → 放行」分支先生效。
- **build tag CI 盲区**：默认 `go test ./...` 不含 fnos 代码 → 必须补 `-tags fnos` 步骤。

## 不在范围内（Out of Scope）

- 不做 OIDC/OAuth（飞牛不提供）。
- 不做「真实用户名展示 + 与本地账号预绑定/链接」的映射表（如未来需要再引入 `fnos_uid → username` 表）。
- 不做飞牛「WebView 桥接登录」——移动端/CLI 走 API Key（后续如需再议）。
- 不改 `sanitizeWorkspaceUser`（G2 由派生名规避）。
- 不新增 LLM 用量按用户查询（G3）。
