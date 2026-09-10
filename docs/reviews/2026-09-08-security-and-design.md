# 代码走查：安全问题与设计问题

> 日期：2026-09-08  
> 范围：当前 `master` 全库（控制面、鉴权、沙箱后端、预览、LLM 网关、记忆、模板构建、Browser、Compose、Web 控制台）  
> 方法：对照已宣称安全模型（`docs/security.md`、`docs/auth.md`、`docs/architecture/*`）读实现，按可利用性与架构一致性归类  
> 本文不是对外宣传稿。产品立场见 [../security.md](../security.md)。

## 结论

Roundpen 在**单用户、本机/内网、部署者即管理员**的前提下，有一批扎实的默认：密码 bcrypt、session 哈希入库、Docker `CapDrop: ALL` + `no-new-privileges`、工作区路径 `..` 拦截、预览不裸奔、登录失败限流。

但代码已经实现了**多用户账号**（`user` / `admin`、公开注册开关），数据面却几乎没有属主。再叠加默认后端 Kern 的弱隔离，以及策略 / 审计 / 工具网关三个空包，当前实现与文档中的「默认安全、分层围栏」之间有明显落差。

若部署面暴露到局域网或公网，且存在第二个账号（公开注册、共享 API Key、或泄露的 session），下列 Critical / High 项可直接变成越权或宿主机侧信道。

| 严重度 | 数量 | 含义 |
|--------|------|------|
| Critical | 4 | 已认证普通用户即可越过宣称边界，或预览/网关可打到控制面本机 |
| High | 10 | 密钥明文、跨用户读写、隔离缺口、模板构建等同宿主机执行 |
| Medium | 9 | 加固缺失、信息泄露、文档与实现对不上 |
| Design | 8 | 架构债：空包、无属主模型、双协议无统一授权层 |

---

## 威胁模型（本文采用）

| 角色 | 能力假设 |
|------|----------|
| 部署者 / admin | 控制面、宿主机、Docker socket、数据库 |
| 已认证普通用户 | Cookie session 或 `rp-` API Key；不能改 settings |
| 持有预览 token 的人 | 能打开 `/p/{id}/{port}/` |
| 沙箱内 Agent | 在 Kern jail 或 Docker 容器里执行命令、读写 `/workspace` |
| 未认证攻击者 | 能打到 HTTP 端口（含被错误暴露的 Compose Postgres） |

单用户本机开发：多数 High 项降为「自己打自己」。一旦 `ROUNDPEN_ALLOW_PUBLIC_REGISTRATION=true`，或把控制面挂到不可信网络，模型立刻变成多租户。

---

## 安全问题

### Critical

#### S1. 资源无属主：任意已认证用户可操作全部沙箱、文件、终端、预览、Browser

沙箱表没有 `owner` / `user_id`（`migrations/0001_init.sql`）。`sandbox.Service` 的 Get / List / Exec / Delete / Dial / AttachTerminal 只按 sandbox id 查库，从不读 `auth.GetUser`。

HTTP 层同样如此：`internal/api/e2b`、`internal/api/httpapi`、`internal/preview`、`internal/browser` 只要求「已登录」，不要求「是这个沙箱的主人」。`RequireAdmin` 只罩住用户管理与 settings。

**后果**：用户 A 创建沙箱后，用户 B 可以 `GET /sandboxes` 列出全部，再 `POST /v1/sandboxes/{id}/exec`、读文件、开终端、签发预览、操作 Browser。这是经典 IDOR，且覆盖面是整个 Environment Services。

**建议**：`sandboxes.owner_username`（或 `user_id`）；Manager 所有读路径强制 `owner == ctx.User`，admin 可绕过；List 默认只返回自己的。

#### S2. 记忆按客户端自报 `user_id` 过滤，不绑定登录身份

`internal/memory/http.go` 的 list / search / add 把请求体或 query 里的 `user_id` / `agent_id` / `run_id` 原样交给 store。任意已认证用户可：

- `GET /v1/memories?user_id=alice` 读别人的长期记忆
- `GET /v1/memories/{id}` 按 id 直读
- `PUT` / `DELETE` 改删任意一条
- `GET /v1/sessions/{sid}/memory` 读任意短时会话

`user_id` 在库里只是索引字段，不是授权边界。这与 `docs/security.md`「记忆按 agent_id、user_id 等维度索引」的表述一致于存储、不一致于访问控制。

**建议**：写入时强制 `UserID = auth.GetUser().Username`；读取时忽略客户端伪造的 user_id，或仅允许 admin 跨用户查询。

#### S3. Kern 预览 Dial 打到控制面 `127.0.0.1`，可代理本机任意端口

```234:248:internal/backend/kern/kern.go
func (b *Backend) Dial(ctx context.Context, sandboxID string, destPort int) (net.Conn, error) {
	// ...
	var d net.Dialer
	return d.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", destPort))
}
```

`preview-link` 只检查沙箱存在，不检查端口是否由该沙箱监听。`/p/{id}/{port}/` 拿到 token 后原样 Dial。

默认 Compose 是 `ROUNDPEN_BACKEND=kern`，且 Postgres 在同一 Docker 网络。更危险的是**宿主机直接跑 `./roundpend`**：已认证用户可签发 `port=5432`（或 22、2375、其他本机服务）的预览链接，把控制面本机端口反代出去。

文档写「控制面拨入容器网络命名空间」，Kern 路径没有命名空间，实现是本机回环。

**建议**：Kern 必须 `unshare-net` 或只 Dial 该 jail 的 veth；预览端口白名单；禁止 Dial 回环 / 链路本地 / 私网（除非显式允许）；签发前探测「该沙箱是否在听这个端口」。

#### S4. 模板构建 = 任意已认证用户在构建主机上执行 RUN

`POST /v3/templates`、`POST /v2/templates/{id}/builds` 等 E2B 模板路由**没有** `RequireAdmin`。`builder.Dockerfile` 把 `RUN` / `APT_INSTALL` / `PIP_INSTALL` 步直接写进 Dockerfile，再交给本机 Docker daemon 或 Kaniko。

Docker 构建默认在宿主机引擎上跑，构建时的 `RUN` 能读 Docker 环境、打内部网络、把结果推进本地镜像库。这不是「沙箱内执行」，而是**控制面构建面 RCE**。

`StartCmd` 拼进 `printf` 的 shell 转义只防了单引号，构建步本身仍是任意 shell。

**建议**：模板 CRUD / build 仅 admin；或独立 builder 队列 + 无特权 Kaniko，禁止访问控制面网络与 Docker socket。

---

### High

#### S5. API Key、Virtual Key、上游 LLM Key 均明文落库

| 秘密 | 存储 | 读取面 |
|------|------|--------|
| 用户 `rp-` API Key | `users.api_key` 明文 | `GetByAPIKey` 等值查询；登录用户上下文里带着完整 key |
| LLM Virtual Key | `llmgw_virtual_keys.key` 明文主键 | `GET /v1/llmgw/virtual-keys`、`GET /v1/llmgw/setup` 对**任意已认证用户**回显全文 |
| 上游 OpenAI / Anthropic Key | `llmgw_upstreams.api_key` 明文；`app_settings.payload` JSON 明文 | settings GET 有掩码；virtual keys 字符串 `llmgwVirtualKeys` **未掩码** |

Session token 做了 SHA-256 再入库，API Key 没有。数据库备份、SQL 注入、或任何能读 PG 的人等于拿到全部控制面与上游凭证。

`handleVirtualKeys` / `handleSetup` 也不是 admin-only，普通用户能列出内部 `vk-roundpen-internal`。

**建议**：API Key / Virtual Key 只存哈希（前缀保留用于识别）；上游 key 用进程级或 KMS 封装；列表接口改 admin，且永不回显全文。

#### S6. LLM 网关把请求 / 响应正文写入 PG，任意用户可查

默认 `LogBodyMaxBytes = 64KiB`。`llmgw_transactions` 存 `request_body`、`response_body`、`upstream_request_body`。`GET /v1/llmgw/logs` 与 `GET /v1/llmgw/logs/{id}` 无 admin 限制、无按 virtual key 属主过滤。

正文里会出现用户 prompt、系统提示、工具参数、偶尔被模型回显的密钥。这与「真实密钥不进 Agent」并不矛盾——密钥进了控制面日志，再被每个登录用户读走。

**建议**：默认不记 body，或只记 metadata；日志接口 RequireAdmin；按 vk / 用户隔离。

#### S7. `/llmgw/` 转发会把 Cookie 等头带到上游

`copyHeaders` 只剥离 `Authorization`、`x-api-key`、`Host`。浏览器若带着 `roundpen_session` 打网关（同源、`credentials: include`），Cookie 会进 OpenAI / Anthropic 请求。Hop-by-hop 头（`Connection`、`Transfer-Encoding`）也未过滤。

`/llmgw/` 还是中间件公钥路径，不走控制面鉴权——这是设计如此，但转发面应做最小头集合。

**建议**：允许列表（`content-type`、`accept`、anthropic 版本头等）；禁止转发 `Cookie`、`Cookie2`、`X-Forwarded-*`。

#### S8. 登录限流信任客户端 `X-Forwarded-For`

```381:389:internal/api/auth/session_handler.go
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	// ...
}
```

没有「可信反代」配置。未认证攻击者可每换一个伪造 XFF 就重置 15 分钟 / 10 次窗口，也可把计数打到受害者 IP 上造成拒绝登录。限流还是进程内存，多副本不共享。

Cookie 的 `Secure` 同样只看 `X-Forwarded-Proto`，伪造 https 会让控制面以为该下发 Secure cookie（反向：真 TLS 终结但不传该头时，cookie 变成非 Secure）。

**建议**：`ROUNDPEN_TRUSTED_PROXIES`；默认只用 `RemoteAddr`；限流改按 username + IP，并落到 Redis/PG。

#### S9. Kern jail 不是文档所写的「宿主机根不可见、最小挂载」

`wrapBwrap` 只尝试 `--unshare-uts`。没有 `--unshare-net`、`--unshare-pid`、`--unshare-user`、`--unshare-ipc`。只读绑定：

- `/usr` `/bin` `/sbin` `/lib*` `/etc`
- `/run`（常见含 `docker.sock`、systemd、其他套接字）

Guest 与宿主机共享网络命名空间，可扫内网、打元数据、连回控制面。`/etc` 含 `passwd`、`shadow`（若权限允许）、kubeconfig、云凭证等。`/proc` 是宿主机 proc。进程以 **roundpend 同 UID** 运行，不是 drop 到 nobody。

`bwrap` 探测失败时还会去掉 UTS，jail 进一步变薄。

这与 README / `docs/security.md`「宿主机根文件系统对 Agent 不可见」部分成立（`/` 被重建），但「隔离执行环境」在网络、进程、身份上不成立。默认后端还是 Kern。

**建议**：产品上把 Kern 标成「开发用弱隔离」；默认生产档改为 Docker + 非默认 bridge + 无出站（或显式白名单）；Kern 至少 unshare net/pid/user，不要 bind `/run`，`/etc` 改为最小副本。

#### S10. Docker 后端隔离不完整

已有：`CapDrop: ALL`、`no-new-privileges`、内存 / CPU 限制。

没有：`NetworkMode`（默认 bridge，容器可打宿主机网关与内网）、非 root `User`、`PidsLimit`、`ReadonlyRootfs`、`Tmpfs`、seccomp/AppArmor 配置文件、磁盘配额（`DiskSizeMB` 只写入记录，Create 不用）。任意用户可指定/解析到任意镜像，`ensureImage` 会从上游 pull。

`StrictHostKeyChecking=accept-new` 的 SSH Dial 有首次 MITM 窗口。

**建议**：默认 `internal` network 或自定义 bridge + egress 策略；以非 root 跑；补 pids / 只读根；镜像允许列表或摘要钉扎。

#### S11. 工作区 Files API 跟随符号链接，可读出根外文件

`local.resolve` 用 `filepath.Clean` + `Rel` 挡住 `../`，但 `Open` / `Write` / `RemovePath` 走 `os.Open` / `os.Create` / `os.RemoveAll`，**跟随 symlink**。沙箱内 Agent（或已认证用户）可：

```text
ln -s /etc/passwd /workspace/x
GET /v1/sandboxes/{id}/files/content?path=x
```

Kern 还能 ln 到 `/run/docker.sock` 等。`RemovePath` 对 symlink 指向的目录做 `RemoveAll` 有删宿主机数据的风险。

sshfs 默认目录模式 `0777`，远端工作区对任何 UID 可写。

**建议**：`OpenFile` + `O_NOFOLLOW`，或 resolve 后 `EvalSymlinks` 再检查仍在 base 下；sshfs 默认 `0750`。

#### S12. 预览 token 是不记名能力，且实现窄于文档

文档：`/p/` 校验「短时 token **或会话**」。  
实现：`/p/` 在中间件里整段公钥；只认 query / `roundpen_preview` cookie 里的 token。登录 session **不能**打开预览。

Token：16 字节随机、内存 map、不绑定用户、不绑定签发者、重启失效。出现在 URL query（进 Referer、反向代理日志、浏览器历史）。Cookie 无 `Secure`。默认 TTL 15 分钟，期间持有链接的人等同预览该端口。

结合 S1，任何人都能给**别人的沙箱**签发 token。结合 S3，token 可能指向本机端口。

**建议**：token 绑定 `user + sandbox + port`，签名 JWT 或落库；支持 session 回退（与文档一致）；从 URL 改为 fragment 或一次性兑换；cookie 补 Secure。

#### S13. 终端 WebSocket `CheckOrigin: true`

```17:21:internal/api/httpapi/terminal.go
var terminalUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}
```

跨站页面只要受害者带着 session cookie（`SameSite=Lax` 对 WebSocket 的保护因浏览器而异，且 API Key 场景不适用），即可连上 `/v1/sandboxes/{id}/terminal` 拿到交互 shell。再叠加 S1，目标可以是任意沙箱。

**建议**：允许列表（控制台 Origin + `ROUNDPEN_PUBLIC_URL`）；拒绝空 Origin 的浏览器请求。

#### S14. Host Chrome + `evaluate` = 在宿主机浏览器里跑任意 JS

`CDPProvider=host`（或 auto 且本机有 Chrome）时，Hub 在 **roundpend 进程旁**拉起 Chrome，user-data 在 `{dataRoot}/browser/{id}`。`POST /v1/sandboxes/{id}/browser/evaluate` 执行调用方提供的表达式。`navigate` 不限制 `file://` / 内网 URL。

这不是「沙箱里的浏览器」，是控制面宿主机浏览器。任意已认证用户（S1）都能用它读 `file://`、打内网、在有已登录 profile 时碰本地会话（当前每次新 user-data，风险略低，但仍在宿主机网络里）。

**建议**：默认禁止 host provider；evaluate / navigate 仅 docker/kern 内 CDP；禁止 `file:` 与链路本地。

---

### Medium

#### S15. 错误信息回传给客户端

`httpapi.exec`、`e2b.create`、`browser.*`、`llmgw` 多处 `writeErr(..., err.Error())` 或 `"dial failed: "+err.Error()`。路径、镜像 pull、Docker、SSH、上游 URL 会进响应，便于踩点。

#### S16. HTTP 服务器几乎无超时

`http.Server` 只设了 `ReadHeaderTimeout: 10s`。无 `ReadTimeout` / `WriteTimeout` / `IdleTimeout`。llmgw `http.Client{Timeout: 0}` 为了流式，但缺少绝对上限。慢连接 / 大文件 `readFile`（无 `MaxBytes`，与 write 的 50MiB 不对称）可拖住进程。

#### S17. CSRF 面：Cookie session + 无 CSRF token

Cookie：`HttpOnly` + `SameSite=Lax` + 条件 `Secure`。Lax 能挡住多数跨站 POST，挡不住「从外站顶栏 GET 带 cookie」以及部分 WebSocket。状态变化接口（create/delete/exec、改密、rotate key）没有 CSRF token 或 `Origin` 校验。自托管若与不可信站点同站（子域），风险上升。

改密后会 `DeleteByUser` 再发新 session，这点是对的。

#### S18. 启动时明文打出 admin 密码与 API Key

`auth.BootstrapAdmin` 在日志里 `slog.String("password", plain)` / `api_key`。Compose 指引让用户 `docker compose logs | head`。日志采集、多副本重启、调试 dump 都会留下初始凭证。生成密码用 `byte % len(alphabet)`，有轻微模偏差。

#### S19. Compose 默认把 Postgres 打到宿主机 5432，默认口令 `roundpen`，`sslmode=disable`

```26:29:compose.yaml
    ports:
      - "${POSTGRES_PORT:-5432}:5432"
```

注释写「可选给宿主机工具用」，但默认就是发布。同一文件还示范挂载 `/var/run/docker.sock`。这是私有化模板里最常见的事故源。

**建议**：默认不 publish 5432；示例口令改为必填且无默认。

#### S20. 中间件在 `users == nil` 时整段放行

测试或错误装配时，控制面变成无鉴权。生产路径目前总会注入 UserStore，属于防御性缺口。

#### S21. 无密码用户改密不校验 current_password

`ChangePassword`：`PasswordHash != ""` 才检查旧密码。与 bootstrap「无密码则生成」组合后问题不大，但 admin 重置后的空哈希窗口、或未来 SSO 用户，可被已盗 session 直接设密。

公开注册的用户名枚举（conflict 区分「已存在」）是预期的管理成本，记录即可。

#### S22. 文档宣称已交付、实现未落地或更弱

| 文档说法 | 实现 |
|----------|------|
| 预览校验 token **或会话** | 只校验 token |
| Kern：宿主机根不可见、适合默认本地 | 共享 net/pid/uid，bind `/etc` `/run` |
| 策略引擎 / 工具网关 / 审计「架构预留」 | 三个包只有 package 注释，无类型、无调用点 |
| `project-layout.md`：Kern「占位」、Phase 1 只做 Docker | Kern 已是默认后端；Browser、settings、模板构建均已存在 |
| README：REST **+ gRPC** | 无 gRPC |
| 长期记忆按用户隔离 | 过滤可选，不强制 |

对外稿 `agent-security.md` 把规划能力写得很满，技术稿相对克制，但「已交付」表仍偏乐观。

#### S23. 预览 / setup URL 信任 `X-Forwarded-Host`

`previewLink` 在 `PublicURL` 为空时用 `r.Host`；`publicBaseURL` 优先 `X-Forwarded-Host`。未配受信代理时，可把签发的预览 URL 或 llmgw setup 指到攻击者域名（钓鱼、token 进对方日志）。

---

## 设计问题

### D1. 先做了多用户，没有做多租户

用户表、角色、公开注册、admin CRUD 已经是多账号产品。沙箱、记忆、模板、预览、llmgw 日志、virtual key 仍是全局池。`template.Public` 有字段，没有按 namespace / owner 执行。沙箱 `name` 全局唯一，用户之间会 `ErrConflict`。

这是当前最大的产品/架构裂缝：**认证是多用户的，授权是单租户的**。继续加功能（Browser、记忆、模板）会放大 S1/S2，而不是自然长出隔离。

**建议**：先定威胁模型——「单租户家庭 NAS」就关掉公开注册、文档写明所有登录用户等价 admin；「小团队」就补 owner 列并做强制过滤。不要停在中间态。

### D2. 策略 / 工具网关 / 审计是空包，但出现在主架构图

`internal/policy`、`internal/toolgw`、`internal/audit` 各三行注释。`sandbox.Service` 创建、exec、Dial 不经过它们。README 架构图和「核心能力」把策略、审计、工具网关写成已有层。

空包的成本：贡献者以为有拦截点；安全评审以为有围栏；真正的 exec 路径是直通 Backend。

**建议**：从主图拿掉，或先做最小 `Authorizer` 接口并挂上 exec / files / preview / build。不要用空目录占位表示「已预留能力」。

### D3. 没有统一的授权层，每个 HTTP 包自己假设「登录即可」

路由装配在 `cmd/roundpend/main.go` 里平铺：e2b、httpapi、preview、browser、memory、llmgw、settings。鉴权只有一层 Middleware + 少量 `RequireAdmin`。

结果：新增 handler 很容易忘记属主检查（Browser、记忆已经发生）。E2B 兼容面与 native 面重复暴露同一 Manager，授权规则无法单点修改。

**建议**：`Manager` 不直接给 HTTP；中间加 `ACLManager` 或把 `User` 传入每个 Manager 方法。E2B 与 native 只做协议转换。

### D4. 两个后端的安全档位差一个数量级，API 却相同

同一套 `POST /sandboxes` + `exec`，Docker 与 Kern 对调用方不可区分。默认还是弱的那个。Environment Services（尤其 Ports）在 Kern 上语义都变了（S3）。

**建议**：沙箱记录里写 `isolation: kern|docker|gvisor`；控制台与 API 标明档位；预览 / Browser 在 Kern 上降级或拒绝。

### D5. Host-side 文件模型与「Agent 可写工作区」冲突未收口

设计选择 host-side `WorkspaceFS` 是对的（沙箱停了还能管文件）。但 Agent 在工作区里创建的 symlink / device / hardlink 会回到控制面进程的文件系统视图。Files API 目前只防路径字符串，不防 inode 类型。sshfs 用 `cat` / `rm` 拼远程 shell，多了一层命令注入面（现有 `shellQuote` 看起来够用，但仍是「SSH 上跑 shell」而不是 sftp）。

**建议**：把「工作区是不可信文件系统」写成不变量：NOFOLLOW、拒 fifo/device、配额、病毒扫描位可后加。

### D6. Settings 是可变全局单例，热更新面过宽

一份 `app_settings` JSON：注册开关、默认镜像、Kaniko 参数（含 `KanikoExtraArgs`、`Insecure`、`SkipTLSVerify`）、全部 LLM 密钥与 virtual keys、CDP token。admin PUT 即热应用到进程。

没有变更审计、没有密钥独立轮换 API、没有「哪些字段需要重启」。`KanikoExtraArgs` 进 executor 命令行，admin XSS/CSRF 或被盗 session 可以直接加 Kaniko 参数。

**建议**：拆成：部署配置（环境变量、需重启）、运营配置（注册、TTL）、密钥保险箱。密钥变更写 audit。

### D7. 可观测性与强制终止名存实亡

没有 exec / 终端 / 预览 / 模板构建的审计表。异常检测、强制 kill、token 预算都不存在。出事后只能翻 Docker 日志和 llmgw 交易表（还泄露正文）。

TTL 回收依赖 `expires_at`（需确认是否有后台 reap；本次走查未把 reap 列为已验证能力）。Kern `Stop` 只改内存状态，**不杀已启动的 jail 进程**——长期 `StartCmd` 后台进程与 PTY 子进程可在「已停止」后仍占着宿主机。

**建议**：先做 append-only audit（who / action / sandbox / code）；Kern Stop 必须杀进程组。

### D8. 架构文档与仓库不同步，Phase 叙事过期

`docs/architecture/project-layout.md` 仍写 Phase 1 只做 Docker、Kern 占位、k8s Phase 4。实际已有：Kern 默认、Browser CDP、settings 热更新、模板 Docker/Kaniko、记忆 mem0 API、嵌入式 SPA。

过期文档会让后续走查和贡献者误判攻击面。

**建议**：layout 改成「现状 + 明确未做」；Phase 表要么更新要么删除。

---

## 做得对的地方（避免误报）

- Session token 32 字节随机、SHA-256 入库、滑动过期、改密吊销全部 session。
- 密码 bcrypt cost 12、最短 8 位；登录失败不区分用户是否存在（除「未设密码」分支）。
- 公开注册默认关闭；admin 路由有 `RequireAdmin`；不能删 `admin` 用户。
- Docker 创建丢特权、禁提权；工作区 `..` 字符串逃逸有单测。
- 预览 token 用 `crypto/rand`，不是短 id 猜测模型；`/p/` 未认证会被 401（在有合法 token 之前）。
- llmgw 用 virtual key 换上游 key，`setUpstreamAuth` 会覆盖客户端带来的鉴权头。
- 设置页对 OpenAI / Anthropic / CDP token 回显掩码（virtual keys 除外）。
- 文件写入有 50MiB 上限。

这些说明问题主要在**授权模型与隔离默认值**，而不是「完全没有安全意识」。

---

## 建议修复顺序

按「先堵住已认证越权，再收默认后端，再补密钥与构建面」：

1. **属主列 + Manager 强制过滤**（S1、S2、D1、D3）。没有这一层，后面的预览/终端加固只是给攻击者少几个入口。
2. **Kern Dial / 预览端口**（S3、S12）：禁止回环与私网，或 Kern 直接关掉 Ports。
3. **模板构建仅 admin + 构建隔离**（S4）。
4. **密钥哈希与日志降级**（S5、S6、S7）。
5. **Kern 降级为开发后端的产品与文档**（S9、D4、S22）；Compose 默认不要 publish PG（S19）。
6. **symlink NOFOLLOW、WS Origin、Host Chrome 默认关**（S11、S13、S14）。
7. **受信代理、HTTP 超时、错误对外脱敏**（S8、S15、S16、S23）。
8. **audit 最小实现，删或填空包**（D2、D7）。

---

## 验证时未做的事

- 未做动态利用（未对运行实例发攻击请求）。
- 未审计 `web/` 的 XSS / 依赖漏洞（控制台信任同源 API）。
- 未逐条验证沙箱 TTL 回收是否在跑。
- 未评估 gVisor / Kata 配置路径（代码仅把 runtime 字符串传给 Docker）。

发现基于当前仓库静态阅读；修复后应补：跨用户 IDOR 集成测试、Kern 预览不得 Dial `127.0.0.1:5432`、symlink 逃逸测试、模板路由的角色测试。
