# Authentication

Roundpen 支持三种验证方式（对齐 ai-sandbox）：

1. **用户名/邮箱 + 密码**：登录后下发 HttpOnly `roundpen_session` Cookie（7 天滑动过期）；
   原生客户端（移动端控制台）可在登录时要求把会话 token 一并返回，之后以
   `Authorization: Bearer <sessionToken>` 携带——服务端存的只有 token 的哈希，
   token 每次登录新发一条、可单独吊销（见下「移动端登录」）
2. **API Key**（`rp-...`）：CLI 与自动化，通过 `X-API-Key` 或 `Authorization: Bearer`。
   数据库只存哈希，明文仅在生成/轮换时返回一次
3. **GitHub / Gitea OAuth2**：授权码 + PKCE 登录；token 同时作为该用户的 git 凭据注入沙箱，
   见 [git-credentials.md](git-credentials.md)

首次启动会创建 `admin` 用户。若未设置 `ROUNDPEN_API_KEY`，会生成一把 API Key 并只打印一次；若 admin 没有密码，会生成初始密码并打印一次。

## HTTP

| Method | Path | Auth | Notes |
|--------|------|------|--------|
| POST | `/v1/auth/register` | public（开关） | `{ email?, password, username?, fullname? }` → 201 + Set-Cookie。需 `ROUNDPEN_ALLOW_PUBLIC_REGISTRATION=true` |
| POST | `/v1/auth/login` | public | `{ user, password, returnSessionToken? }`（`user` 可为 username 或 email；也接受 `username` / `email`）→ 200 `{ user, sessionToken? }` + Set-Cookie。`sessionToken` 只在显式要求时返回（见「移动端登录」） |
| POST | `/v1/auth/logout` | session | 204，吊销当前会话（Cookie 或 Bearer 会话 token）并清除 Cookie |
| GET | `/v1/auth/user` | Cookie 或 API key | 当前用户（API key 脱敏） |
| GET | `/v1/auth/oauth/providers` | public | 已启用的联合登录 provider（登录页按钮用） |
| GET | `/v1/auth/oauth/{provider}/start` | public | 302 到远端授权页；可选 `?redirect=` 站内回跳路径 |
| GET | `/v1/auth/oauth/{provider}/callback` | public | 回调：建会话/绑定身份后 302 回 SPA，失败回 `?oauth_error=<code>` |
| GET | `/v1/me/identities` | session 或 API key | 当前用户已关联的远端身份（token 不外泄） |
| POST | `/v1/me/identities/link/{provider}` | session | 已登录时绑定：返回 `{ authorizeUrl }` |
| DELETE | `/v1/me/identities/{id}` | session | 解绑（不影响手填 PAT） |
| GET/PUT | `/v1/me/git-credentials` | session 或 API key | 当前用户 git token（GET 脱敏）。见 [git-credentials.md](git-credentials.md) |
| DELETE | `/v1/me/git-credentials/{id}` | session 或 API key | 删除一条 git token |
| GET/PUT | `/v1/admin/oauth/providers` | admin | OAuth 应用配置（secret 脱敏；PUT 按 id upsert） |
| DELETE | `/v1/admin/oauth/providers/{id}` | admin | 删除 provider 并级联解绑其身份 |
| POST | `/v1/auth/password` | session | `{ current_password, new_password }` |
| POST | `/v1/auth/apikey/rotate` | session | 返回新明文 key 一次 |
| GET | `/v1/admin/users` | admin | 用户列表 |
| POST | `/v1/admin/users` | admin | `{ username, email?, fullname?, orgName?, role?, password? }` → 201，明文 `apiKey` 与 `password` 只返回一次 |
| PUT | `/v1/admin/users/{username}` | admin | 更新 email / fullname / orgName / role |
| DELETE | `/v1/admin/users/{username}` | admin | 不可删除 `admin` |
| POST | `/v1/admin/users/{username}/password` | admin | 可选 `{ password }` → `{ username, password }` 一次 |
| POST | `/v1/admin/users/{username}/apikey` | admin | 返回新明文 API key 一次 |

公开路径（无需登录）：`GET /health`、`GET /v1/ready`、登录/注册、`GET /v1/auth/oauth/`（联合登录的
发现与回调）、以及 LLM 网关 `/llmgw/`（使用 virtual key）。

登录按 IP 限流（15 分钟内 10 次失败）；`returnSessionToken` 在口令校验之后才生效，无法绕过限流。
OAuth 回调不经过该限流器（不涉及凭据猜测，state 单次使用且 10 分钟过期）。

## 移动端登录

原生客户端没有浏览器 Cookie 罐，流程是：

1. `POST /v1/auth/login` `{ user, password, returnSessionToken: true }` → 响应体里的 `sessionToken`
   （64 位十六进制，明文只此一次；服务端存的是 SHA-256）连同 `Set-Cookie` 一起下发，客户端忽略 Cookie。
2. 之后所有 REST 与 WebSocket 升级请求都带 `Authorization: Bearer <sessionToken>`
   （`X-API-Key` 不接受会话 token，只认 `rp-` 开头的 API Key）。
3. token 进系统钥匙串（Keychain / Keystore）。同一条 token 同时只有一个设备持有；
   会话 7 天滑动过期，期间任意一次请求都会续期。
4. 退出登录调 `POST /v1/auth/logout`（带同一个 Bearer）即可在服务端吊销该会话。
5. 改密码（`POST /v1/auth/password`）会吊销该用户名下的**所有**会话，客户端会收到 401，需重新登录。

为什么不用 API Key：数据库只存哈希，登录接口拿不到明文；`/v1/auth/apikey/rotate` 虽能返回明文，
但会作废其它设备（CLI、另一台手机）手里的 key，不能拿来当登录凭据。

## 联合登录（GitHub / Gitea）

- provider（client id/secret、Gitea 实例地址、scope）在 **设置 → OAuth 登录**（admin）里配置，
  存 `oauth_providers` 表；多个 Gitea 实例 = 多行。界面展示的回调地址由当前请求推导，复制到
  远端平台登记：`{scheme}://{host}/v1/auth/oauth/{id}/callback`。
- 首次登录的账号映射：已绑定 → 直接登录；远端**已验证邮箱**命中本地用户 → 自动关联；否则仅当
  `ROUNDPEN_ALLOW_PUBLIC_REGISTRATION=true` 时新建账号（无密码，`auth_provider` 记 provider 类型）；
  都不满足则回登录页提示"先本地登录再到设置里绑定"。
- 已登录用户可在 **设置 → 关联账号** 绑定/解绑；解绑不影响手填的 Git 个人令牌。
- 默认 scope `read:user user:email repo`（GitHub 与 Gitea 通用）。

## Bootstrap

首次启动若 `admin` 没有密码，日志会打印 `admin initial password (change immediately)`。请立刻用 `POST /v1/auth/password` 改密。

`ROUNDPEN_API_KEY` 若已设置，会作为 admin 的 API Key（与旧的全局 key 行为兼容：同一把 key 仍可调用控制面）。

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `ROUNDPEN_API_KEY` | 空（生成） | 种子 admin API Key |
| `ROUNDPEN_BOOTSTRAP_ADMIN` | `true` | 设为 `false` 跳过自动创建 admin |
| `ROUNDPEN_ALLOW_PUBLIC_REGISTRATION` | `false` | 允许 `POST /v1/auth/register` |
