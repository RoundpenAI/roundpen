# Authentication

Roundpen 支持两种验证方式（对齐 ai-sandbox）：

1. **用户名/邮箱 + 密码**：登录后下发 HttpOnly `roundpen_session` Cookie（7 天滑动过期）
2. **API Key**（`rp-...`）：CLI 与自动化，通过 `X-API-Key` 或 `Authorization: Bearer`

首次启动会创建 `admin` 用户。若未设置 `ROUNDPEN_API_KEY`，会生成一把 API Key 并只打印一次；若 admin 没有密码，会生成初始密码并打印一次。

## HTTP

| Method | Path | Auth | Notes |
|--------|------|------|--------|
| POST | `/v1/auth/register` | public（开关） | `{ email?, password, username?, fullname? }` → 201 + Set-Cookie。需 `ROUNDPEN_ALLOW_PUBLIC_REGISTRATION=true` |
| POST | `/v1/auth/login` | public | `{ user, password }`（`user` 可为 username 或 email；也接受 `username` / `email`）→ 200 `{ user }` + Set-Cookie |
| POST | `/v1/auth/logout` | session | 204，清除 Cookie |
| GET | `/v1/auth/user` | Cookie 或 API key | 当前用户（API key 脱敏） |
| POST | `/v1/auth/password` | session | `{ current_password, new_password }` |
| POST | `/v1/auth/apikey/rotate` | session | 返回新明文 key 一次 |
| GET | `/v1/admin/users` | admin | 用户列表 |
| POST | `/v1/admin/users` | admin | `{ username, email?, fullname?, orgName?, role?, password? }` → 201，明文 `apiKey` 与 `password` 只返回一次 |
| PUT | `/v1/admin/users/{username}` | admin | 更新 email / fullname / orgName / role |
| DELETE | `/v1/admin/users/{username}` | admin | 不可删除 `admin` |
| POST | `/v1/admin/users/{username}/password` | admin | 可选 `{ password }` → `{ username, password }` 一次 |
| POST | `/v1/admin/users/{username}/apikey` | admin | 返回新明文 API key 一次 |

公开路径（无需登录）：`GET /health`、`GET /v1/ready`、登录/注册、以及 LLM 网关 `/llmgw/`（使用 virtual key）。

登录按 IP 限流（15 分钟内 10 次失败）。

## Bootstrap

首次启动若 `admin` 没有密码，日志会打印 `admin initial password (change immediately)`。请立刻用 `POST /v1/auth/password` 改密。

`ROUNDPEN_API_KEY` 若已设置，会作为 admin 的 API Key（与旧的全局 key 行为兼容：同一把 key 仍可调用控制面）。

## 环境变量

| 变量 | 默认 | 说明 |
|------|------|------|
| `ROUNDPEN_API_KEY` | 空（生成） | 种子 admin API Key |
| `ROUNDPEN_BOOTSTRAP_ADMIN` | `true` | 设为 `false` 跳过自动创建 admin |
| `ROUNDPEN_ALLOW_PUBLIC_REGISTRATION` | `false` | 允许 `POST /v1/auth/register` |
