# 设计：GitHub / Gitea OAuth2 联合登录与登录身份操作远程仓库

> 日期：2026-09-20
> 状态：已实施（2026-09-20）
> 范围：`internal/oauth`（新包）/ `internal/api/auth`（会话复用、公开路径、建号）/ `internal/gitcred`（注入合并）/ `internal/userenv`（注入接线）/ `internal/storage/schema`、`migrations`（三张表）/ `cmd/roundpend`（装配）/ `web/src`（登录页、设置）/ `tests/uismoke`、`web/e2e`

## 背景与目标

现在的登录方式是用户名/邮箱 + 密码，或 API Key（`docs/auth.md`）。远程仓库操作靠用户在
Settings → Git personal tokens 里手填 PAT，由 `EnsureAgent` 注入 guest `$HOME/.roundpen/git`
（`docs/git-credentials.md`）。用户在一台机器上要维护两份东西：控制面账号，和各 git 实例的 token。

本轮做 **OAuth2 联合登录**：GitHub 与自建 Gitea 走授权码 + PKCE 登录控制面；登录得到的
access token 同时作为该用户的 git 凭据注入沙箱，Agent 在沙箱里的 `git clone/push`、`gh`、`tea`
就是"以登录身份"操作远程仓库，不必再手填 PAT。

目标：

1. 登录页出现 `Sign in with GitHub` / `Sign in with Gitea` 按钮，授权后拿到会话 Cookie。
2. 已登录用户可在设置里把远端账号**绑定**到当前本地账号（也支持解绑）。
3. 绑定后的 token 进入现有注入链路；**同一 host 若用户手填过 PAT，则 PAT 优先**。

## 需求结论（澄清结果）

1. **范围**：OAuth token 只注入沙箱（复用 `internal/gitcred` 注入链路），控制面自身不调远端 API。
   后续议题/PR 同步若要复用，直接读 `internal/oauth` 的 store 即可。
2. **账号映射**：已绑定过该远端身份 → 直接登录；未绑定且远端**已验证邮箱**命中本地用户 →
   自动关联并登录；再否则仅当 `ROUNDPEN_ALLOW_PUBLIC_REGISTRATION=true` 时新建本地账号；
   都不满足 → 拒绝，提示"先用本地账号登录，再到设置 → 关联账号里绑定"。
3. **Provider 配置**：存数据库（`oauth_providers` 表），admin 界面/接口管理，支持**多个 Gitea 实例**
   （每个实例一行），`client_secret` 响应脱敏。
4. **凭据优先级**：同一 host 上手填 PAT 优先；OAuth token 只在与该 host 无 PAT 时注入。
   解绑 OAuth 不影响 PAT。
5. **协议**：只做 OAuth2 授权码 + PKCE(S256)，不校验 ID Token（不引 OIDC）；不做 GitHub App、
   不做 GitLab（provider 表结构可扩展，本期只实现 github/gitea 的端点推导）。
6. **Token 存储**：与现有 `user_git_credentials.token` 一致，明文存 PG（见"非目标"与"风险"）。
7. **Gitea token 会过期**：默认 1 小时。靠 `refresh_token` 刷新：注入时按需刷新 + 后台定时刷新，
   刷新后重注入正在运行的 agent 沙箱。

## 方案选择

**方案 A（采用）：独立 `internal/oauth` 包 + 身份表，凭据在注入时合并。**

- 新包用标准库 `net/http` 手写 OAuth2 客户端（token 交换、刷新、拉 profile），**不引入新依赖**
  ——Gitea 的端点是非标准的，`golang.org/x/oauth2` 能省下的只有两个 POST；私有化部署的
  GOPROXY 未必可达，新增依赖是实打实的风险。
- 登录身份（`user_identities`）与 git 凭据（`user_git_credentials`）**分开存**，注入时按 host 合并，
  "PAT 优先"规则只在一个地方实现一次；刷新只改身份表，不碰 PAT 行。

**方案 B（引入 `golang.org/x/oauth2`）被否**：收益小、依赖多，且 `Endpoint` 仍需手工填 Gitea 地址。

**方案 C（OAuth token 直接写进 `user_git_credentials`）被否**：该表 `UNIQUE (user_id, host)`，
OAuth token 会覆盖同 host 的手填 PAT；刷新与解绑还要同步两份数据，"PAT 优先"表达不出来。

**方案 D（OIDC + ID Token 校验收敛）被否**：本期只要登录 + token，多一层 JWKS/签名校验没有收益。

## 设计

### 1. 数据模型

三张新表（`internal/storage/schema/postgres.sql` 为权威，`migrations/0019_oauth.sql` 同步一份）：

```sql
-- 远端 OAuth 应用（一个 Gitea 实例 = 一行）
CREATE TABLE IF NOT EXISTS oauth_providers (
    id            TEXT PRIMARY KEY,              -- slug，如 github、gitea-git-eaxi-com
    kind          TEXT NOT NULL,                 -- github | gitea
    host          TEXT NOT NULL,                 -- github.com | git.eaxi.com（可带端口）
    label         TEXT NOT NULL DEFAULT '',      -- 登录按钮/列表展示名，空则用 kind+host
    client_id     TEXT NOT NULL DEFAULT '',
    client_secret TEXT NOT NULL DEFAULT '',
    scopes        TEXT NOT NULL DEFAULT '',
    auth_url      TEXT NOT NULL DEFAULT '',      -- 空 = 按 kind+host 推导
    token_url     TEXT NOT NULL DEFAULT '',
    api_url       TEXT NOT NULL DEFAULT '',
    enabled       BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (kind, host)
);

-- 远端身份 → 本地用户；token 放这里，注入时投影成 git 凭据
CREATE TABLE IF NOT EXISTS user_identities (
    id               TEXT PRIMARY KEY,
    user_id          TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    provider_id      TEXT NOT NULL REFERENCES oauth_providers (id) ON DELETE CASCADE,
    subject          TEXT NOT NULL,              -- 远端用户 id（稳定，非 login）
    login            TEXT NOT NULL DEFAULT '',   -- 远端用户名（会变，仅展示）
    name             TEXT NOT NULL DEFAULT '',
    email            TEXT NOT NULL DEFAULT '',
    access_token     TEXT NOT NULL DEFAULT '',
    refresh_token    TEXT NOT NULL DEFAULT '',
    token_expires_at TIMESTAMPTZ,                -- NULL = 不过期（GitHub 默认）
    scopes           TEXT NOT NULL DEFAULT '',
    last_login_at    TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (provider_id, subject),               -- 一个远端身份只能绑一个本地账号
    UNIQUE (user_id, provider_id)                -- 一个本地账号每个 provider 只绑一个
);

-- 授权流程的 state（服务端随机、单次使用）
CREATE TABLE IF NOT EXISTS oauth_states (
    state       TEXT PRIMARY KEY,
    provider_id TEXT NOT NULL,
    verifier    TEXT NOT NULL DEFAULT '',        -- PKCE code_verifier
    link_user   TEXT NOT NULL DEFAULT '',        -- 非空 = 绑定流程，回调须匹配会话用户
    redirect_to TEXT NOT NULL DEFAULT '',        -- 回跳的站内路径
    expires_at  TIMESTAMPTZ NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

`UNIQUE (user_id, provider_id)` 是有意的限制：一个本地账号在一个 Gitea 实例上只联一个远端账号，
注入脚本按 host 生成一份凭据，多账号无法表达（见非目标）。

### 2. Provider 端点推导与默认 scope

`internal/oauth/provider.go` 按 `kind` + `host` 推导默认值，表里三项 URL 非空则覆盖：

| kind | auth_url | token_url | api_url | 默认 scope | git 用户名 |
|------|----------|-----------|---------|-----------|-----------|
| `github` | `https://{host}/login/oauth/authorize` | `https://{host}/login/oauth/access_token` | `https://api.github.com` | `read:user user:email repo` | `x-access-token` |
| `gitea` | `https://{host}/login/oauth/authorize` | `https://{host}/login/oauth/access_token` | `https://{host}/api/v1` | `read:user user:email repo` | `git` |

- `github` 的 host 固定 `github.com` 时 api_url 才是 `api.github.com`；企业版（host 非 github.com）用
  `https://{host}/api/v3`。
- git 用户名沿用 `internal/gitcred` 的 `authUsername`（github `x-access-token`、gitea `git`），
  不在这里另立一份。
- 回调地址由请求推导（`httpx.DefaultTrust.Scheme/Host`）：`{scheme}://{host}/v1/auth/oauth/{id}/callback`，
  admin 界面展示给用户去各平台登记（该值不落库，避免反代换域名后失效）。

### 3. 授权流程（登录）

```
浏览器                   控制面                                  远端
  │ GET /v1/auth/oauth/{id}/start?redirect=/settings/accounts
  │──────────────────────▶ 建 state 行（verifier、10min TTL）
  │◀──── 302 authorize?...&code_challenge=...&state=...
  │──────────────────────────────────────────────────────────▶ 用户授权
  │◀──── 302 .../callback?code=...&state=...
  │ GET callback ─────────▶ 校验 state（存在/未过期/单次）、换 token、拉 profile
  │                          ├ 解析本地账号（见 3.1）
  │                          ├ upsert user_identities
  │                          └ auth.IssueSession 下发 Cookie
  │◀──── 302 {redirect_to}（仅站内相对路径，默认 /）
```

远端 profile 拉取（`api_url` 上）：

- GitHub：`GET /user`（`Accept: application/vnd.github+json`）取 `id/login/name/email`；
  `GET /user/emails` 取 `primary && verified` 的邮箱（需要 `user:email` scope，失败仅降级为无邮箱）。
- Gitea：`GET /user` 取 `id/login/full_name/email`；`GET /user/emails` 取 `primary`（失败同样降级）。

任一请求带 `Authorization: Bearer <access_token>`；**已验证邮箱**以 emails 接口为准，`/user.email`
只作兜底且**不参与自动关联**（GitHub 允许用户把任意邮箱设为公开邮箱）。

#### 3.1 本地账号解析

按顺序（每一步命中即停）：

1. `(provider_id, subject)` 已存在身份 → 该身份的用户，更新 token / login / email + `last_login_at`。
2. `state.link_user` 非空（绑定流程）→ 要求当前会话用户 == `link_user`，写入该用户的身份；
   若该 `(provider_id, subject)` 已绑到别人 → 报 `already linked to another account`。
3. 已验证邮箱命中本地用户（`users.email`，大小写不敏感）→ 建身份并登录。
4. `allowRegistration()` 为真 → 建新用户：用户名取远端 `login`（经 `auth.UniqueUsername` 去重），
   `auth_provider` 写 provider 的 kind，`password_hash` 留空（无密码账号），再建身份并登录。
5. 否则 → 302 回 `/login?oauth_error=registration_disabled`。

错误一律走 `?oauth_error=<code>` 回登录页展示，不把远端错误正文透给浏览器。

### 4. 绑定 / 解绑（已登录）

- `POST /v1/me/identities/link/{provider}`（session）→ `{authorizeUrl}`，state 里带 `link_user`；
  前端拿到后 `window.location.assign(url)`。
- 回调复用 3.1 的第 2 步；成功后回跳 `redirect_to`（设置页）并带 `?oauth_linked=1`。
- `DELETE /v1/me/identities/{id}`（session）→ 204。只删身份，**不碰** PAT 行；guest 里的文件由
  下一次 `EnsureAgent` 的注入脚本重写（脚本每次全量重写 credentials/config/env）。

### 5. Git 凭据合并与刷新

`internal/gitcred` 三处小改：

```go
// 纯函数：把身份投影成凭据（用户名默认值复用 authUsername）
func TokenCred(provider, host, token string) Cred

// primary 优先，按归一化 host 去重，输出按 host 排序（脚本稳定）
func MergeCreds(primary, fallback []Cred) []Cred

// 原来的 Store.GuestInstallScript(ctx,userID) 降级成纯函数
func InstallScript(creds []Cred) string
```

`internal/userenv.injectGit` 变成：

```go
creds, _ := s.Git.List(ctx, userID)              // 手填 PAT
if s.Identities != nil {
    if extra, err := s.Identities.CredsForUser(ctx, userID); err == nil {
        creds = gitcred.MergeCreds(creds, extra)  // PAT 优先
    }
}
script := gitcred.InstallScript(creds)
```

`oauth.Service.CredsForUser` 内做**按需刷新**：`token_expires_at` 在 5 分钟内（或已过期）且有
refresh_token 时先刷新再返回；刷新失败记日志、沿用旧 token（git 侧会明确报认证失败，好过静默丢失）。

**后台刷新器**（`cmd/roundpend` 装配，10 分钟一跳）：刷新所有 30 分钟内过期的身份，并对该用户
**正在运行的 agent 沙箱**重跑注入脚本（`userenv.Service.ReinjectGit(username)`：查到 running 的
agent 沙箱才 exec，绝不拉容器）。Gitea 默认 1 小时 token，没有这一步，跑长任务到一半就 push 不了。

### 6. HTTP 接口一览

| Method | Path | Auth | 说明 |
|--------|------|------|------|
| GET | `/v1/auth/oauth/providers` | public | 登录页按钮用：`[{id,kind,host,label}]`（仅 enabled） |
| GET | `/v1/auth/oauth/{provider}/start` | public | 302 到远端 authorize；可选 `?redirect=` 站内路径 |
| GET | `/v1/auth/oauth/{provider}/callback` | public | 回调；成功下发会话 Cookie 并 302 回 SPA |
| GET | `/v1/me/identities` | session/API key | 已绑定身份（token 脱敏、暴露 expiresAt） |
| POST | `/v1/me/identities/link/{provider}` | session | 返回 `{authorizeUrl}` |
| DELETE | `/v1/me/identities/{id}` | session | 解绑 |
| GET | `/v1/admin/oauth/providers` | admin | 全部 provider（secret 脱敏 + `callbackUrl` 提示） |
| PUT | `/v1/admin/oauth/providers` | admin | 新增/更新（按 id upsert） |
| DELETE | `/v1/admin/oauth/providers/{id}` | admin | 删除（级联删身份） |

公开路径：`internal/api/auth/middleware.go` 的 `isPublicPath` 放行上述三个 GET `/v1/auth/oauth/` 前缀
（只放行 GET；写操作全在 `/v1/me`、`/v1/admin` 下走既有鉴权）。

### 7. 前端

- **登录页**（`web/src/pages/LoginPage.tsx`，文案保持硬编码英文，与现状一致）：密码表单下方渲染
  provider 按钮（`GET /v1/auth/oauth/providers`，为空则不渲染），点击跳 `start`；
  读 `?oauth_error=` 显示错误 Banner。
- **设置 → 关联账号**（新 section `accounts`，非 admin）：`LinkedAccountsPanel` 列出已绑定身份
  （provider 标签、远端 login/email、token 到期时间），可解绑；未绑定的 enabled provider 显示
  "Connect" 按钮走 link 流程。
- **设置 → OAuth 登录**（新 section `oauth`，admin）：provider 列表 + 表单（kind/host/label/
  client_id/client_secret/scopes/enabled/三个 URL 覆盖），展示回调地址供复制；secret 用
  `settings.MaskSecret` 同款脱敏（提交掩码表示不改）。
- i18n：新增 `settings.section.accounts`、`settings.section.oauth`、`accounts.*`、`oauthAdmin.*`
  两组 key，en/zh_CN 同步（`web/src/i18n/index.test.ts` 会卡住缺项）。

### 8. 安全边界

- `state` 32 字节随机、服务端单次使用、10 分钟过期；绑定流程额外要求会话用户匹配。
- PKCE S256 全程开启（GitHub 与 Gitea ≥1.19 都支持；Gitea 老版本忽略该参数，不影响流程）。
- `redirect`/`redirect_to` 只接受站内相对路径（`/` 开头且不以 `//` 开头），防开放重定向。
- token 交换失败、profile 拉取失败、账号映射失败分别记 slog，浏览器只看到错误码。
- 回调不经过登录限流器（不涉及凭据猜测），但 state 单次 + 10 分钟窗口使爆破无收益。

### 9. 与现有实现的关系

| 点 | 现有 | 本轮 |
|----|------|------|
| 会话下发 | `SessionHandler.issueSession`（未导出） | 抽成导出的 `auth.IssueSession`，OAuth 回调复用 |
| 用户名去重 | `session_handler.go` 的 `uniqueUsername`（未导出） | 导出为 `auth.UniqueUsername`，注册与 OAuth 建号共用 |
| git 凭据 | `user_git_credentials`（手填 PAT） | 不变；注入时与身份凭据合并，PAT 优先 |
| 用户表 | `auth_provider` 恒为 `local` | OAuth 建号写 `github`/`gitea`；密码登录对无密码账号已有明确报错 |
| 密码 | 无密码账号不能密码登录 | 不变；`hasPassword=false` 时 `ChangePassword` 已允许免当前密码设置 |

## 非目标（本期不做）

- OIDC / ID Token 校验、GitHub App、GitLab provider（表结构留了扩展位，端点推导只写 github/gitea）。
- 控制面直接调远端 API（议题/PR 双向同步）；token 已落在 `user_identities`，后续可直接复用。
- token 静态加密（与现有 PAT 同为明文；单独一轮做密钥管理更合适）。
- 组织/租户级 provider 授权策略（哪个用户能用哪个 provider）；本期所有 enabled provider 对所有人可用。
- 同一本地账号在同一 Gitea 实例绑定多个远端账号（注入脚本按 host 生成一份凭据）。

## 测试计划

- `internal/oauth`（新）：provider 端点/scope 推导的表驱动测试；`httptest` 假 GitHub/Gitea
  （authorize → token → /user → /user/emails）覆盖：登录成功、邮箱自动关联、建号（开关两态）、
  拒绝、绑定流程（会话不匹配报错）、state 过期/复用报错、refresh 流程（过期→刷新→持久化）、
  `CredsForUser` 的 PAT 无关性。**TDD：先写测试再实现。**
- `internal/gitcred`：`MergeCreds`（同 host PAT 优先、host 归一化、排序）、`TokenCred` 用户默认值、
  `InstallScript` 在只剩 OAuth 凭据时的脚本内容（断言 token 不出现在 config、`.gitconfig` include 正确）。
- `internal/api/auth`：`isPublicPath` 新增路径的放行与写方法不放行。
- `internal/oauth`（PG，`DATABASE_URL` 门控）：providers/identities/states 三表 CRUD 与唯一约束。
- 前端：`tests/uismoke` 增加 `/v1/auth/oauth/providers`、`/v1/me/identities`、`/v1/admin/oauth/providers`
  三个 stub；Playwright 新 spec 覆盖登录页按钮渲染 + 点击跳转、关联账号列表与解绑、
  admin provider 表单保存。
- 手工验收：`make dev`，用真实 GitHub OAuth App 与 Gitea 实例各跑一遍登录 → 沙箱内
  `git clone/push`、`gh auth status` / `tea` 确认身份；再验证同 host 填 PAT 后 PAT 优先生效。

## 依赖与前置

- **GitHub**：注册 OAuth App，Authorization callback URL 填 `{base}/v1/auth/oauth/github/callback`，
  scope 用默认 `read:user user:email repo`。
- **Gitea**：≥1.18（scope 语义）、建议 ≥1.19（PKCE）；注册 OAuth2 应用（confidential client），
  回调同上。`[oauth2] ACCESS_TOKEN_EXPIRATION_TIME` 默认 3600s，refresh token 默认有效。
- Go 依赖无新增；前端无新增依赖。

## 风险与边界

- **明文存储**：`user_identities` 里的 access/refresh token 与现有 PAT 一样是明文（PG 访问即等价于
  token 泄露）。缓解：与 PAT 同策略，只注入 guest `$HOME`，永不进 `/workspace`；加密作为独立轮次。
- **刷新窗口**：若 Gitea 实例关闭 refresh token 或把过期时间设得极短，长任务中途仍可能失败；
  此时回退到 PAT（优先级本来就高于 OAuth）。
- **回调地址漂移**：反代改域名后旧登记的 callback 失效，需要 admin 重新登记——admin 界面始终展示
  由当前请求推导的地址，避免"抄了一个过期的"。
- **账号接管**：邮箱自动关联依赖远端"已验证邮箱"，GitHub/Gitea 都由平台保证；`/user.email` 兜底值
  不参与关联，避免用户用公开邮箱字段认领他人账号。
- **provider 删除**：`DELETE /v1/admin/oauth/providers/{id}` 级联删除身份（沙箱里残留的 token 要等
  下次 `EnsureAgent` 重写文件或容器重建才消失）。
