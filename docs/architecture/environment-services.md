# Environment Services（沙箱之上的 Agent 环境）

Roundpen 的 `Sandbox` 提供隔离计算与生命周期；**Environment Services** 叠在沙箱之上，给 Agent 常用的操作面与资源面。本文锁定 P0 能力边界、核心接口，以及相对 ai-sandbox（Hikari）**明确不搬运**的清单，避免实现时被多主机 / daemon / 产品绑定拖偏。

相关文档：[project-layout.md](./project-layout.md)、[../auth.md](../auth.md)。

## 分层

```
Agent / Framework
        │
   Environment Services     ← 本文
        │  Terminal · Workspace · Ports  (P0)
        │  Browser / Computer / Mobile   (后续 Surface)
        │
   Sandbox Manager          ← 已有
        │
   Backend + WorkspaceFS    ← 引擎与宿主文件
```

| 概念 | 含义 | Roundpen 落点 |
|------|------|----------------|
| **Sandbox** | 隔离计算单元（创建/停/exec） | `internal/sandbox` |
| **Surface** | Agent 如何「看见并操作」环境 | Terminal；日后 Browser/Computer/Mobile |
| **Resource** | 环境附着的资源 | Workspace、Ports、Network、Secrets |
| **Profile** | 启动时装配的服务包 | 例如 `shell` / `dev` / `browser`（后续） |

P0 只落地三个 Resource/Surface：**Workspace**、**Ports**、**Terminal**。它们是 coding agent 的地基，也是后续 Browser Use 的前置（尤其 `Dial`）。

## 设计原则

1. **控制面薄、引擎可插拔**：协议与鉴权在 API；PTY / Dial / 挂载在 `Backend`；宿主文件在 `workspace.FS`。
2. **Host-side 文件优先**：读写以 `workspace.FS` 为准，不要求沙箱进程存活；容器内视角为可选增强。
3. **先接口、后 sidecar**：Terminal 以 `AttachPTY`（docker exec / kern pty）起步；不默认引入 in-sandbox daemon。
4. **安全默认**：预览流量默认需短时令牌或会话；不采用「知道短 ID 即可访问」。
5. **可参考 ai-sandbox 的协议形状，不平移其控制面拓扑**（见文末清单）。

## P0-1 Workspace

### 目标

- 沙箱内外一致的工作区目录，挂载为容器内 `/workspace`。
- 控制面可在沙箱停止时仍 list/read/write。
- HTTP 形状对齐常见 Agent 文件 API，便于框架接入。

### 接口（扩展现有 `workspace.FS`）

现状（已有）：

```go
type FS interface {
    Create(ctx context.Context, id string, ephemeral bool) (*Info, error)
    Get(ctx context.Context, id string) (*Info, error)
    Remove(ctx context.Context, id string) error
    Open(ctx context.Context, id, relPath string) (io.ReadCloser, error)
    Write(ctx context.Context, id, relPath string, r io.Reader) error
    Stat(ctx context.Context, id, relPath string) (os.FileInfo, error)
}
```

P0 增量：

```go
// List 返回目录项；relPath 为空或 "." 表示工作区根。
List(ctx context.Context, id, relPath string) ([]DirEntry, error)

// RemovePath 删除文件或空目录（非 Remove 整个 workspace）。
RemovePath(ctx context.Context, id, relPath string) error

type DirEntry struct {
    Name  string
    IsDir bool
    Size  int64
    // ModTime optional
}
```

约束：

- 所有 `relPath` 必须经 path clean，禁止 `..` 逃逸出工作区根。
- 实现：`local` / `sshfs` 同步补齐；宿主机布局 `{data_root}/sandboxes/{id}/{workspace,home}`（guest 内分别为 `/workspace`、`/home`）；兼容旧路径 `{data_root}/workspaces/{id}`

### HTTP（native）

挂在已认证的控制面（Cookie 或 API Key）：

| Method | Path | 说明 |
|--------|------|------|
| GET | `/v1/sandboxes/{id}/files?path=` | list |
| GET | `/v1/sandboxes/{id}/files/content?path=` | 读文件（octet-stream） |
| PUT / POST | `/v1/sandboxes/{id}/files?path=` | 写文件（body 流） |
| DELETE | `/v1/sandboxes/{id}/files?path=` | 删文件/空目录 |
| POST | `/v1/sandboxes/{id}/workspace/upload` | 可选：zip/tar.gz 解压到 `/workspace` |

实现：`api/httpapi`（或 `api/files`）→ `sandbox.Manager` 解析 workspace id → `workspace.FS`。  
**不**经容器 `exec` 做默认读写。

### 明确不做（P0）

- 独立 `hk_workspaces` 目录服务、sticky host、checkpoint、ZFS clone
- fsnotify / watch API
- 以「容器内 docker exec 文件 API」为唯一路径

---

## P0-2 Ports / Preview

### 目标

- Agent 在沙箱内起的 HTTP 服务（如 `:3000`）可被人类或框架打开预览。
- 不依赖 Docker `-p` 发布；控制面拨入容器网络命名空间 / bridge IP。

### 接口（扩展 `backend.Backend`）

```go
// Dial 建立到沙箱内 destPort 的 TCP 连接。
// Docker：经容器 IP（本机 bridge 直连；远端 SSH/已有 docker 上下文另议）。
// Kern：不支持 Ports（拒绝 Dial，避免打到控制面回环）。
Dial(ctx context.Context, engineID string, destPort int) (net.Conn, error)
```

`sandbox.Manager` 提供 `Dial(ctx, sandboxID, port)`：查库 → engineID → `Backend.Dial`。

### URL 与反代

| 部署 | 方案 | 说明 |
|------|------|------|
| 单机 / NAS（默认） | Path：`/p/{sandboxID}/{port}/...` | 少依赖 DNS/TLS |
| 有通配域名 | Host：`{id}-{port}.{ROUNDPEN_PREVIEW_DOMAIN}` | 可选；对齐 ai-sandbox vhost 思路 |

反代行为：

1. 解析 sandbox + port → `Manager.Dial`
2. HTTP/WebSocket hijack，双向拷贝
3. 改写 `Host` 为 `127.0.0.1:{port}`（或保留上游需要的 Host，可配置）
4. 成功流量可 debounce 续期沙箱 TTL（防预览中被回收）

### HTTP

| Method | Path | Auth | 说明 |
|--------|------|------|------|
| GET | `/v1/sandboxes/{id}/preview-link?port=&path=` | 是 | 返回可打开的 preview URL；可附带短时 `token` |
| ANY | `/p/{id}/{port}/...` | **短时 token 或会话** | 实际预览流量 |
| — | vhost（可选） | 同上 | `ROUNDPEN_PREVIEW_DOMAIN` 非空时启用 |

`preview-link` 响应示例：

```json
{
  "url": "http://127.0.0.1:9527/p/{id}/3000/?token=...",
  "port": 3000,
  "expires_at": "..."
}
```

### 明确不做（P0）

- 默认无鉴权的公开 vhost（ai-sandbox 现状不适合 Roundpen 自托管默认）
- JD / Taro / openapp 深链
- host-agent `RELAY` 协议与全机 iptables egress 编排
- 「扫描沙箱内正在监听的端口」API（调用方声明 port；后续再加探测）

---

## P0-3 Terminal

### 目标

- 交互式 PTY（xterm / Agent shell），不是一次性 `Exec`。
- 协议简单、可测；与 Backend 解耦。

### 接口（扩展 `backend.Backend`）

```go
type PTYOpts struct {
    Cmd     []string // 默认 ["/bin/bash", "-l"]；Kern/无 bash 镜像可 ["/bin/sh", "-l"]
    WorkDir string   // 默认 /workspace
    Env     []string
    Rows    uint16
    Cols    uint16
}

// AttachPTY 阻塞直到会话结束。stdin→PTY，PTY→stdout；ctx 取消则关闭。
AttachPTY(ctx context.Context, engineID string, opts PTYOpts, stdin io.Reader, stdout io.Writer) error

// ResizePTY 调整已附着会话尺寸；sessionKey 由 Manager 分配。
ResizePTY(ctx context.Context, engineID, sessionKey string, rows, cols uint16) error
```

实现指引：

| Backend | P0 实现 |
|---------|---------|
| Docker | `docker exec -it` + resize API（对齐 ai-sandbox **extension 兜底路径**） |
| Kern | 本机 `bubblewrap`：guest `/workspace` + `/home` 平行挂载；`cd /` 不可见宿主机根 |

**不**在 P0 引入 in-sandbox ConnectRPC daemon。

### WebSocket 协议

- 路由：`GET /v1/sandboxes/{id}/terminal`（需认证；可用现有 session / API key；浏览器可后续加 ws-token）
- 连接后：
  - **Client → Server**：二进制 = stdin；文本且前缀为 `{"rows":` 的 JSON = resize
  - **Server → Client**：二进制 = PTY 输出
- 可选：首帧 JSON 指定初始 `rows`/`cols`（默认 24×80）
- Ping/Pong 保活；读超时可配置

`sandbox.Manager.AttachTerminal`：校验沙箱 running → 分配 sessionKey → `Backend.AttachPTY`。

### 与 Exec 的关系

| | Exec（已有） | Terminal（P0） |
|--|--------------|----------------|
| 用途 | 一次性命令 | 交互 shell |
| API | `POST /v1/sandboxes/{id}/exec` | WS terminal |
| 输出 | 聚合 stdout/stderr | 流式 PTY |

异步 `exec_id` + 日志流可在 P0.5 再加；形状可参考 ai-sandbox，但落在 Roundpen 认证模型内。

### 明确不做（P0）

- hikari-daemon / StartTerminal ConnectRPC / 固定 :49200
- host-agent RELAY 专为 PTY
- session 持久化 / tmux 恢复（ai-sandbox 文档承诺与实现也不一致）
- 未鉴权的 public terminal / exec-log WS

---

## Backend / Manager 汇总（目标形状）

```go
// backend.Backend — 在现有 Create/Start/Stop/Remove/Exec/Logs 之上：
Dial(ctx context.Context, engineID string, destPort int) (net.Conn, error)
AttachPTY(ctx context.Context, engineID string, opts PTYOpts, stdin io.Reader, stdout io.Writer) error
ResizePTY(ctx context.Context, engineID, sessionKey string, rows, cols uint16) error

// workspace.FS — 在现有 CRUD 文件之上：
List(ctx context.Context, id, relPath string) ([]DirEntry, error)
RemovePath(ctx context.Context, id, relPath string) error

// sandbox.Manager — 对 API 暴露：
// Files via workspace id on sandbox record
// Dial(ctx, sandboxID, port)
// AttachTerminal(ctx, sandboxID, opts, stdin, stdout)
```

包边界不变：`api` → `sandbox` → `backend` / `workspace`；`backend` 不依赖 `api`。

可选后续包（不必 P0 建目录）：

- `internal/preview` — path/vhost 反代与 token
- Terminal WS 可暂放 `api/httpapi` 或 `api/terminal`

## Environment Profile（预告，不实现）

| Profile | 装配 |
|---------|------|
| `shell` | Workspace + Terminal |
| `dev` | shell + Ports |
| `browser` | dev + Browser Use（CDP sidecar，依赖 Dial） |
| `desktop` | Computer Use |
| `android` | Mobile Use |

创建沙箱时 `templateID` / metadata 可映射到 Profile；P0 默认行为等同 `dev` 能力逐步点亮（先有 API，再谈模板名）。

## 实现顺序

1. **Workspace**：`FS.List` / `RemovePath` + files HTTP  
2. **Ports**：`Backend.Dial` + path 反代 + `preview-link`（带 token）  
3. **Terminal**：`AttachPTY` + 认证 WS  

每步可独立合并；Ports 的 `Dial` 是后续 Browser CDP 的底座，优先于 Computer/Mobile。

## 相对 ai-sandbox：参考 vs 禁止搬运

### 可参考（概念 / 协议）

| 主题 | 参考点 |
|------|--------|
| Terminal WS | 二进制 stdin/out + JSON resize；handler 用 pipe 桥接 |
| Terminal 兜底 | Docker `exec -it` PTY（非 daemon 路径） |
| Workspace 目录 | `{root}/workspaces/{id}`、`/workspace` bind |
| Files REST | list/content/write/delete 路径形状；archive upload |
| Ports | `Dial(container, port)` + hijack 反代；preview-link 签发 |
| Runner 切分 | AttachTerminal / Dial / 文件 与生命周期分离的思路 |

### 禁止整包搬运

| 禁止项 | 原因 |
|--------|------|
| hikari-daemon + ConnectRPC + h2c | 镜像与控制面过重；Roundpen Backend 已覆盖 exec |
| host-agent RELAY / 多主机 SSH 舰队拓扑 | 自托管 P0 以单机或已有 `DOCKER_HOST` 为准 |
| ZFS driver + sticky host + checkpoint + `hk_workspace_*` | 平台级持久卷，超出当前产品目标 |
| pause / revive / warm pool / ensureRunning 全家桶 | 生命周期复杂度与 Roundpen 轻量定位不符 |
| 容器 exec 作为唯一文件 API | 与 Roundpen host-side `FS` 冲突 |
| 默认无鉴权 vhost + 短 ID 前缀猜测 | 自托管安全模型不可接受 |
| JD openapp / Taro / panel 证书矩阵 | 产品绑定 |
| CFS exec-log + 复杂 blob reaper | 存储平面不同 |
| 未实现的「PTY session 持久化」承诺 | 避免文档与实现再次分叉 |

### 借鉴时的改写规则

1. 鉴权一律走 Roundpen `api/auth`（Cookie / `rp-` API Key），不引入第二套 ws 身份除非浏览器强制需要。  
2. 多主机能力若出现，以 `workspace/sshfs` + `DOCKER_HOST=ssh://` 为扩展点，而不是复制 host-agent。  
3. 任何「可选企业能力」进 `internal/enterprise/` 或独立 module，不堵 P0 主路径。

## 配置预留

| 变量 | 用途 |
|------|------|
| `ROUNDPEN_PREVIEW_DOMAIN` | 空 = 仅 path 预览；非空 = 启用 vhost |
| `ROUNDPEN_PREVIEW_TOKEN_TTL` | preview token 有效期（默认数分钟） |
| `ROUNDPEN_PREVIEW_LISTEN` | 可选独立预览监听地址 |

## 验收（P0 done 的定义）

- [x] 认证后可对沙箱 workspace 做 list/read/write/delete（沙箱 stopped 时 host-side 仍可读）— `GET/POST/DELETE /v1/sandboxes/{id}/files*`
- [x] `Backend.Dial` + path 预览 `/p/{id}/{port}/` + `GET /v1/sandboxes/{id}/preview-link`（短时 token）
- [x] 认证 Terminal WS：`GET /v1/sandboxes/{id}/terminal`（JSON resize + 二进制 PTY）
- [x] Docker（`exec -it` / bridge Dial）与 Kern（`creack/pty` / localhost Dial）均实现接口
- [x] 默认路径不依赖 hikari-daemon / 无鉴权 vhost
