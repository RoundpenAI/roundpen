# GitHub OAuth 中转（Cloudflare Worker）

官方共用一个 GitHub App。部署者不用去 GitHub 建应用。

- 浏览器：GitHub 授权页 → Worker 回调 → 回到这台 NAS
- NAS：出站 `POST /github/start` 和 `GET /tickets/:id`（Worker **不**回连内网）
- 票存在 Durable Object 里，60 秒、领一次就删（不用 KV）

## 部署

```bash
cd workers/github-oauth-relay
npm install
npx wrangler login
```

1. 在 GitHub 建 **一个** GitHub App 或 OAuth App  
   - Callback / redirect：`https://auth.roundpen.ai/github/callback`（改成你的 Worker 域名）  
   - 权限只要登录：`Account permissions → Email addresses: Read`，不要给 `contents: write`
2. `wrangler.toml` 里把 `PUBLIC_BASE` 改成同一 origin
3. 写入密钥（不要进 git）：

```bash
npx wrangler secret put GITHUB_CLIENT_ID
npx wrangler secret put GITHUB_CLIENT_SECRET
npx wrangler secret put STATE_SECRET   # 长随机串，给 OAuth state HMAC
```

4. 自定义域绑到这个 Worker，然后：

```bash
npx wrangler deploy
```

`GET https://auth.roundpen.ai/health` 应返回 `{"ok":true}`。

## NAS 合同（控制面尚未接线）

安装身份与 FREE 模型相同：`rp1.<install_id>.<exp>.<sig>`，公钥头 `X-Roundpen-Install-Pub`。首次见该 `install_id` 会 TOFU 记下公钥。

**1. 用户点「使用 GitHub 登录」—— NAS 出站：**

```http
POST /github/start
Authorization: Bearer rp1.…
X-Roundpen-Install-Pub: <64 hex>
Content-Type: application/json

{"return":"http://192.168.1.8:9527","state":"<NAS 自己的 CSRF>"}
```

`return` 只取 origin。`http` 仅允许 localhost / 局域网 / `.local`；公网必须 `https`。

响应：`{"authorizeUrl":"https://github.com/login/oauth/authorize?…"}`  
NAS **302 浏览器到这个 URL**（不要把 rp1 放进浏览器地址栏）。

**2. GitHub → Worker → 浏览器回到 NAS：**

```
{return}/v1/auth/oauth/github/cloud/callback?ticket=<32 hex>&state=<原 state>
```

失败：`?error=denied|failed&state=`。

**3. NAS 出站领票（不要让浏览器打这条）：**

```http
GET /tickets/<ticket>
Authorization: Bearer rp1.…
X-Roundpen-Install-Pub: <64 hex>
```

```json
{
  "login": "octocat",
  "name": "…",
  "email": "…",
  "accessToken": "ghu_…",
  "tokenType": "bearer",
  "scope": "read:user,user:email",
  "refreshToken": "…",
  "expiresIn": 28800
}
```

404：过期、已领过、或不是这台安装的票。拿到后按现有联合登录建 session；token 只进 `user_identities`，不要写进对话或 memory。

## 本地

```bash
npx wrangler dev
npm test
```

`wrangler dev` 时把 `PUBLIC_BASE` 临时改成 `http://127.0.0.1:8787`，GitHub App 也要加一条对应 callback，否则换 code 会失败。
