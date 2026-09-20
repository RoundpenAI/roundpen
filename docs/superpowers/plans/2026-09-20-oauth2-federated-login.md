# OAuth2 联合登录（GitHub / Gitea）实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: 用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务执行。步骤用 `- [x]` 复选框跟踪。

**Goal:** GitHub 与自建 Gitea 可用 OAuth2（授权码 + PKCE）登录控制面；登录得到的 token 作为该用户的 git 凭据注入沙箱，Agent 以登录身份 clone/push/开 PR；同 host 手填 PAT 优先。

**Architecture:** 新包 `internal/oauth` 管 provider 配置（DB 表 `oauth_providers`）、远端身份（`user_identities`）与授权流程（`oauth_states`，服务端 state 单次使用）；回调成功后复用 `auth.IssueSession` 下发会话。git 注入侧 `internal/gitcred` 提供纯函数 `MergeCreds`（PAT 优先）/`TokenCred`/`InstallScript`，`userenv.injectGit` 合并手填 PAT 与身份凭据；后台刷新器按需刷新 Gitea（默认 1h）token 并重注入运行中的沙箱。

**Tech Stack:** Go 1.26（`net/http` patterns + stdlib OAuth2 客户端，无新依赖）、PostgreSQL、React 19 + Semi `@douyinfe/semi-ui-19`、Playwright。

**Spec:** `docs/superpowers/specs/2026-09-20-oauth2-federated-login-design.md`

---

## File map

| Path | Responsibility |
|------|----------------|
| `internal/storage/schema/postgres.sql` | 三张新表：`oauth_providers` / `user_identities` / `oauth_states`（权威 schema） |
| `migrations/0019_oauth.sql` | 同一份 DDL 的历史镜像 |
| `internal/oauth/provider.go` | `Provider` 结构、kind 校验、按 kind+host 推导 auth/token/api URL、默认 scope、`CallbackURL`（由请求推导） |
| `internal/oauth/provider_test.go` | 端点/scope 推导表驱动测试 |
| `internal/oauth/store.go` | providers / identities / states 的 PG CRUD 与唯一约束 |
| `internal/oauth/store_test.go` | PG 门控存储测试（`DATABASE_URL`） |
| `internal/oauth/client.go` | token 交换 / refresh / 拉 profile（GitHub `Accept: application/vnd.github+json`、Gitea JSON） |
| `internal/oauth/client_test.go` | `httptest` 假远端 |
| `internal/oauth/flow.go` | `StartAuth` / `HandleCallback`（账号映射四步）、`LinkURL`、`CredsForUser`（按需刷新）、`RefreshExpiring` |
| `internal/oauth/flow_test.go` | 登录/关联/建号/拒绝/绑定/state 生命周期/刷新 |
| `internal/oauth/http.go` | 公开、`/v1/me/identities`、`/v1/admin/oauth/providers` 三组路由 |
| `internal/oauth/http_test.go` | 处理器测试（含 admin 校验、脱敏、回调处理） |
| `internal/api/auth/session.go` | 导出 `IssueSession`（回调复用） |
| `internal/api/auth/session_handler.go` | `issueSession` 改为调用导出版；导出 `UniqueUsername` |
| `internal/api/auth/middleware.go` | `isPublicPath` 放行 GET `/v1/auth/oauth/` |
| `internal/api/auth/middleware_test.go` | 公开路径用例 |
| `internal/gitcred/cred.go` | 新增 `TokenCred`（用户名默认值复用 `authUsername`） |
| `internal/gitcred/merge.go` | 新增 `MergeCreds`（PAT 优先、host 归一化去重、排序） |
| `internal/gitcred/inject.go` | `Store.GuestInstallScript(ctx,userID)` → 纯函数 `InstallScript(creds)` |
| `internal/gitcred/cred_test.go` | 合并/投影/脚本用例 |
| `internal/userenv/userenv.go` | `IdentityTokens` 窄接口 + `gitCredentials` 合并两源；新增 `ReinjectGit(username)` |
| `internal/userenv/userenv_test.go` | 合并路径用例（fake Sandboxes 记录 Exec 脚本） |
| `cmd/roundpend/main.go` | 装配 oauth 服务/处理器、后台刷新器 |
| `web/src/api.ts` | `oauth` / `identities` / `oauthAdmin` 客户端与类型 |
| `web/src/pages/LoginPage.tsx` | provider 按钮 + `oauth_error` 提示 |
| `web/src/lib/appNav.ts` | 新增 `accounts`（用户）与 `oauth`（admin）两个 section |
| `web/src/pages/SettingsPage.tsx` | 挂载两个新 panel |
| `web/src/components/LinkedAccountsPanel.tsx` | 关联账号列表 / Connect / 解绑 |
| `web/src/components/OAuthProvidersPanel.tsx` | admin：provider 表单（secret 脱敏、回调地址展示） |
| `web/src/i18n/en.ts`、`web/src/i18n/zh_CN.ts` | 新 key（两语言同步） |
| `tests/uismoke/main.go` | `/v1/auth/oauth/providers`、`/v1/me/identities`、`/v1/admin/oauth/providers` stub |
| `web/e2e/oauth-login.spec.ts` | 登录按钮、关联账号、admin 表单 |
| `docs/auth.md`、`docs/git-credentials.md` | 接口表与优先级说明 |

---

## Phase 1 — 后端：数据模型与 oauth 包

### Task 1: 三张表

- [x] `internal/storage/schema/postgres.sql` 追加 `oauth_providers` / `user_identities` / `oauth_states`
      （含 `UNIQUE (kind, host)`、`UNIQUE (provider_id, subject)`、`UNIQUE (user_id, provider_id)`、索引）；
      `migrations/0019_oauth.sql` 写同一份 DDL。
- [x] `go build ./...` 通过（schema 是 embed，编译期即校验）。

### Task 2: `internal/oauth` provider 推导（纯逻辑，先测后写）

- [x] 写 `internal/oauth/provider_test.go`：github.com / gitea 自建+端口 / GitHub Enterprise(api/v3) /
      显式 URL 覆盖 / 非法 kind / 空 host 报错 / 默认 scope / `CallbackURL` 拼接。
      运行：`go test ./internal/oauth/ -run TestProvider` → 期望 FAIL（包不存在）。
- [x] 写 `internal/oauth/provider.go` 使其通过。

### Task 3: 存储层

- [x] 写 `internal/oauth/store_test.go`（`DATABASE_URL`/`ROUNDPEN_TEST_DATABASE_URL` 门控，参考
      `internal/storage/user_test.go` 的 skip 模式）：providers upsert-by-id、identities 唯一约束
      （同远端身份不能绑两家、同用户同 provider 不能绑两个）、states 过期清理、`ListForUser`。
- [x] 写 `internal/oauth/store.go`（`*sql.DB`，风格对齐 `internal/gitcred/store.go`）。

### Task 4: 远端客户端

- [x] 写 `internal/oauth/client_test.go`：`httptest` 假 GitHub 与假 Gitea —— token 交换（表单编码、
      `Accept: application/json`、PKCE verifier 透传、client_secret）、refresh 交换（轮换 refresh_token）、
      profile 拉取（GitHub `/user`+`/user/emails` 取 verified primary；Gitea `/user`+`/user/emails`；
      emails 失败降级为空邮箱）。
- [x] 写 `internal/oauth/client.go` 使其通过。

## Phase 2 — 后端：流程与 HTTP

### Task 5: 流程服务

- [x] 写 `internal/oauth/flow_test.go`：覆盖 spec §3.1 四步映射（已绑定 / 邮箱自动关联 / 建号开关两态 /
      拒绝）、绑定流程会话不匹配报 `ErrStateUserMismatch`、state 过期与复用、
      `CredsForUser` 的过期→刷新→持久化、刷新失败沿用旧 token。
- [x] 写 `internal/oauth/flow.go`：`StartAuth`、`HandleCallback`、`LinkURL`、`CredsForUser`、
      `RefreshExpiring`；建号复用 `auth.UniqueUsername`，会话复用 `auth.IssueSession`。
- [x] `internal/api/auth/session.go` 导出 `IssueSession`；`session_handler.go` 的 `issueSession`/`uniqueUsername`
      改为薄封装调用导出版（行为不变，现有测试保持通过）。

### Task 6: HTTP 路由与公开路径

- [x] 写 `internal/oauth/http_test.go`：`providers` 只列 enabled；`start` 302 到 authorize 且带
      `state`/`code_challenge`；`callback` 成功 302 + `Set-Cookie`；错误码回 `/login?oauth_error=`；
      `/v1/me/identities` 脱敏；`DELETE` 解绑；admin 路由非 admin 403。
- [x] 写 `internal/oauth/http.go` 使其通过；`cmd/roundpend/main.go` 装配（store → service → handler → Mount）。
- [x] `internal/api/auth/middleware_test.go` 加公开路径用例；`middleware.go` 放行
      GET `/v1/auth/oauth/providers`、`/v1/auth/oauth/{id}/start`、`/v1/auth/oauth/{id}/callback`。

## Phase 3 — 后端：git 凭据合并与刷新

### Task 7: gitcred 纯函数

- [x] 写 `internal/gitcred/cred_test.go` 用例：`MergeCreds` 同 host PAT 优先、host 归一化
      （大小写/端口/URL）、输出排序、空输入；`TokenCred` 的 username 默认值；`InstallScript`
      只含 OAuth 凭据时 config 不含 token 且有 include。
- [x] `internal/gitcred/merge.go` + `cred.go` 的 `TokenCred` + `inject.go` 改 `InstallScript(creds)`；
      更新受影响的既有测试与调用点。

### Task 8: 注入接线与后台刷新

- [x] `internal/userenv/userenv.go`：`Service` 新增窄接口 `IdentityTokens`（`CredsForUser`）；
      `injectGit` 合并；新增 `ReinjectGit(username)`（仅 running 的 agent 沙箱）。
- [x] `internal/userenv/userenv_test.go`：fake 记录 Exec 脚本，断言 OAuth 凭据进脚本、PAT 覆盖同 host。
- [x] `cmd/roundpend/main.go`：10 分钟 ticker 调 `RefreshExpiring` + `ReinjectGit`（失败仅记日志）。

## Phase 4 — 前端

### Task 9: API 客户端与 i18n

- [x] `web/src/api.ts`：`oauthProviders.list/startUrl`（public）、`identities.list/remove/linkUrl`、
      `oauthAdmin.list/save/remove` 与类型。
- [x] `web/src/i18n/en.ts` + `zh_CN.ts`：`settings.section.accounts`、`settings.section.oauth`、
      `accounts.*`、`oauthAdmin.*`；`node --test --experimental-strip-types src/i18n/index.test.ts` 通过。

### Task 10: 登录页与设置面板

- [x] `LoginPage.tsx`：provider 按钮（无 provider 不渲染）、`?oauth_error=` Banner、点击跳 start。
- [x] `LinkedAccountsPanel.tsx` + `appNav.ts`（`accounts`）+ `SettingsPage.tsx` 挂载。
- [x] `OAuthProvidersPanel.tsx` + `appNav.ts`（`oauth`, admin）+ `SettingsPage.tsx` 挂载
      （含回调地址复制、secret 掩码语义）。
- [x] `cd web && npm ci && npx tsc -b && npm run lint`。

## Phase 5 — 测试与验收

### Task 11: uismoke + Playwright

- [x] `tests/uismoke/main.go`：三个 stub（enabled provider 一条、身份两条、admin provider 一条）。
- [x] `web/e2e/oauth-login.spec.ts`：登录页按钮渲染 + 点击后 URL 指向 stub authorize；
      关联账号列表展示与解绑；admin 表单保存。
- [x] `cd web && npm run test:e2e -- oauth-login.spec.ts` 通过；`login.spec.ts`、`settings-*.spec.ts` 不回归。

### Task 12: 文档与全量验收（实施完成后逐条跑）

- [x] `docs/auth.md` 补接口表与环境说明；`docs/git-credentials.md` 补 OAuth 身份与 PAT 优先级一节。
- [x] `go build ./... && go vet ./... && gofmt -l internal cmd tests`
- [x] `go test ./internal/oauth/ ./internal/gitcred/ ./internal/api/auth/ ./internal/userenv/ ./internal/storage/ -count=1`
- [x] `go test ./... -count=1`（全量，含既有用例不回归）
- [x] `cd web && npm ci && npx tsc -b && npm run lint && npm run test:e2e`
- [ ] 手工（需真实凭据，写进 PR 说明由维护者执行）：`make dev` → admin 配 GitHub OAuth App 与
      Gitea OAuth2 应用 → 登录 → 沙箱内 `git clone`/`git push`、`gh auth status`、`tea login list`
      验证身份 → 同 host 填一条 PAT 后确认 PAT 生效。
