# OAuth 中转（Cloudflare Worker）

官方共用一套 OAuth 应用。部署者不用自己去 GitHub / Google 建应用。

现在登记的供应商：

| id | 浏览器回调（登记在上游） | NAS 回跳 |
|---|---|---|
| `github` | `https://auth.eaxi.com/github/callback` | `/v1/auth/oauth/github/cloud/callback` |
| `google` | `https://auth.eaxi.com/oauth/google/callback` | `/v1/connectors/google/cloud/callback` |

`/oauth/github/start` 与 `/github/start` 等价。Google 只走 `/oauth/google/start`。

- 浏览器：上游授权页 → Worker 回调 → 回到这台 NAS
- NAS：出站 `POST /oauth/{provider}/start`、`GET /tickets/:id?state=`、`POST /oauth/{provider}/refresh`（Worker **不**回连内网）
- 票存在 Durable Object 里，60 秒、领一次就删。领票必须带发起时的 NAS `state`，并且 `rp1` 的 `install_id` 必须与建票时一致。对不上不删票。
- refresh token 的 SHA-256 绑在这台安装上。别的部署拿着抄来的 refresh token 刷不了。

## 部署

```bash
cd workers/github-oauth-relay
npm install
npx wrangler login
```

1. GitHub：建 **一个** GitHub App 或 OAuth App
   - Callback：`https://auth.eaxi.com/github/callback`
   - 权限：`repo read:user user:email workflow`（用户授权自己的仓库，不是登录 Roundpen）
2. Google：建 **一个** Web 客户端
   - 已授权的重定向 URI：`https://auth.eaxi.com/oauth/google/callback`
   - 同意屏幕勾上 Gmail 只读、Calendar 只读、openid、email
3. `wrangler.toml` 里把 `PUBLIC_BASE` 改成同一 origin
4. 写入密钥（不要进 git）：

```bash
npx wrangler secret put GITHUB_CLIENT_ID
npx wrangler secret put GITHUB_CLIENT_SECRET
npx wrangler secret put GOOGLE_CLIENT_ID
npx wrangler secret put GOOGLE_CLIENT_SECRET
npx wrangler secret put STATE_SECRET   # 长随机串，给 OAuth state HMAC
```

某个供应商的密钥没配时，只有这个供应商的 start/callback/refresh 会失败，另一个不受影响。

5. 自定义域绑到这个 Worker，然后：

```bash
npx wrangler deploy
```

`GET https://auth.eaxi.com/health` 应返回 `{"ok":true,"llm":true}`（`llm` 表示 FREE 上游密钥已配）。

### FREE LLM

与 OAuth 共用 `rp1` / TOFU。NAS 默认 `baseUrl` 为 `https://auth.eaxi.com/llm`：

```http
POST /llm/v1/chat/completions
Authorization: Bearer rp1.…
X-Roundpen-Install-Pub: <64 hex>
Content-Type: application/json

{"model":"roundpen-free","messages":[{"role":"user","content":"hi"}],"stream":true}
```

Worker 把 `roundpen-free` 映射成上游模型（默认 `openrouter/free`），按 `install_id` 限流（默认 120 次/小时），超限 HTTP 429。密钥：

```bash
npx wrangler secret put LLM_UPSTREAM_API_KEY   # OpenRouter 等
```

可选 vars：`LLM_UPSTREAM_BASE`、`LLM_UPSTREAM_MODEL`、`LLM_REQUESTS_PER_HOUR`、`LLM_REGISTER_PER_IP_PER_DAY`。

控制面用 `ROUNDPEN_OAUTH_RELAY` 指向这个 origin（未设置时默认 `https://auth.eaxi.com`，`off` 关闭）。`ROUNDPEN_GITHUB_OAUTH_RELAY` 仍作为旧别名。这不是登录方式：已登录用户授权之后，令牌进该用户自己的身份或 Google grant。用户自己配了 GitHub App 或 Google Client 时，官方中转让路。

## NAS 合同

安装身份与 FREE 模型相同：`rp1.<install_id>.<exp>.<sig>`，公钥头 `X-Roundpen-Install-Pub`。首次见该 `install_id` 会记下公钥；之后同一 `install_id` 换了新公钥（重装）会自动轮换并累加 `reinstallCount`。

**1. 已登录用户点连接 —— NAS 出站：**

```http
POST /oauth/google/start
Authorization: Bearer rp1.…
X-Roundpen-Install-Pub: <64 hex>
Content-Type: application/json

{"return":"http://192.168.1.8:9527","state":"<NAS 自己的 CSRF>"}
```

`scopes` 可选，只能是该供应商允许列表的子集；省略则用默认 scope。`return` 取 origin，**可带路径前缀**（飞牛统一网关必须带，如 `https://nas.example/app/roundpen`，否则回跳 `/v1/...` 会落到别的应用）。`http` 仅允许 localhost / 局域网 / `.local`；公网必须 `https`。

响应：`{"authorizeUrl":"https://accounts.google.com/o/oauth2/v2/auth?…"}`  
NAS **302 浏览器到这个 URL**（不要把 rp1 放进浏览器地址栏）。

**2. 上游 → Worker → 浏览器回到 NAS：**

```
{return}/v1/connectors/google/cloud/callback?ticket=<32 hex>&state=<原 state>
```

GitHub 则是 `{return}/v1/auth/oauth/github/cloud/callback?ticket=&state=`。若 `return` 带前缀（如 `…/app/roundpen`），回调为 `{return}/v1/auth/...`。

失败：`?error=denied|failed&state=`。

**3. NAS 出站领票（不要让浏览器打这条）：**

```http
GET /tickets/<ticket>?state=<同一个 NAS state>
Authorization: Bearer rp1.…
X-Roundpen-Install-Pub: <64 hex>
```

```json
{
  "provider": "google",
  "subject": "123",
  "login": "a@gmail.com",
  "name": "…",
  "email": "a@gmail.com",
  "accessToken": "ya29.…",
  "tokenType": "bearer",
  "scope": "openid email https://www.googleapis.com/auth/gmail.readonly …",
  "refreshToken": "1//…",
  "expiresIn": 3599
}
```

404：过期、已领过、`state` 或安装对不上。GitHub 的 `subject` / `login` 都是 GitHub login。

**4. 刷新（secret 留在 Worker）：**

```http
POST /oauth/google/refresh
Authorization: Bearer rp1.…
X-Roundpen-Install-Pub: <64 hex>

{"refreshToken":"1//…"}
```

只有当初领到这枚 refresh token 的 `install_id` 能刷。轮换后旧绑定删掉、新 token 绑到同一台安装。

## 重装与公钥轮换（TOFU）

飞牛卸装/清数据会重生 Ed25519 密钥，但 `install_id` 仍来自 `/etc/device_id`。Worker 发现同一 `install_id` 带着新公钥时会**自动接受**并累加 `reinstallCount`，用户无需找维护者清 TOFU；FREE / OAuth 在重装后点一次即可继续用。

维护者仍可用 admin 接口强制清掉已存公钥（重装次数保留），下一请求会当首次登记：

```bash
# STATE_SECRET 与 wrangler secret 里的相同；install_id 在 NAS 的
# /vol1/@appdata/roundpen/data/install-identity/install_id
curl -fsS -X POST https://auth.eaxi.com/admin/installs/reset-tofu \
  -H "Authorization: Bearer $STATE_SECRET" \
  -H 'Content-Type: application/json' \
  -d '{"installId":"<install_id>"}'
```

## 本地

```bash
npx wrangler dev
npm test
```

`wrangler dev` 时把 `PUBLIC_BASE` 临时改成 `http://127.0.0.1:8787`，各家应用也要加一条对应 callback，否则换 code 会失败。
