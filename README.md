# Roundpen

**轻量级、可私有化部署的 Agent 固定环境基础设施。**

Roundpen（驯马圈）为 AI Agent 提供隔离的执行环境、持久工作区、记忆与策略控制，并可在 NAS 与 Linux 服务器上自托管。控制面使用 Go 实现，提供原生 REST API 与 Web 控制台。

> 安全隔离的「驯马圈」，加上记忆与工具的「草料」、策略与审计的「缰绳」——在本地或私有环境里驯服 Agent。

## 特性

- **轻量自托管**：单二进制控制面，面向 NAS、笔记本与单机服务器
- **固定环境槽位**：登录即可用 **Cloud Agent**、**Browser**（及后续 Mobile），一槽位一机器；不是多开沙箱 SDK
- **可插拔后端**：Agent 槽位默认 Docker / Kern；Browser 槽位使用 **QEMU**（qcow2 + CDP hostfwd + VNC unix sock）
- **可定制镜像**：Templates = 槽位镜像配方（`slot=agent|browser`）；Agent→OCI，Browser→qcow2
- **统一存储**：短期与长期记忆均使用 PostgreSQL（含 `pgvector`）
- **用户体系**：用户名/邮箱+密码（Cookie session）与每用户 API Key
- **开源核心**：Apache 2.0；企业能力走 Open Core

## 产品模型

| 槽位 | 本期 | 形态 |
|------|------|------|
| Cloud Agent | Docker / Kern | coding / stdio ACP |
| Browser | QEMU | XFCE + Chrome；CDP + 主机 VNC→WebSocket |
| Mobile | 预留 | 后续独立 VM |

## 架构（摘要）

```
User → Agent env (OCI) + Browser env (qcow2/QEMU)
Browser: Chrome CDP :9222 via hostfwd；桌面 = QEMU -vnc unix:…/vnc.sock → /v1/me/environments/browser/desktop
```

| 层级 | 说明 |
|------|------|
| 控制面 | 网关、策略、审计、记忆、工具 / LLM 网关、`/v1/me/environments` |
| 环境抽象 | Sandbox Manager + 用户槽位映射（`user_environments`） |
| 后端 | Docker / Kern（agent）+ QEMU（browser）；`multi` 按 slot 路由 |
| 镜像 | `internal/template`（slot）+ `images/browser-qemu/` |

仓库布局见 [docs/architecture/project-layout.md](docs/architecture/project-layout.md)。QEMU Browser 部署见 [docs/architecture/qemu-browser.md](docs/architecture/qemu-browser.md)。Agent 安全见 [docs/security.md](docs/security.md)。

## 核心能力

1. **固定环境**：`EnsureBrowser` / `EnsureAgent`；API `/v1/me/environments`
2. **记忆与文件**：PostgreSQL + `/workspace` 挂载
3. **LLM 网关**：Anthropic / OpenAI 透传；内部 Virtual Key
4. **Web 控制台**：Chats / Browser / Images（镜像）/ Settings
5. **ACP Agent 网关**：会话 UI；Browser CDP 绑用户 Browser 环境

## 快速开始

见下文 Docker Compose / `make dev`。构建 Browser 盘：

```bash
./images/browser-qemu/build.sh   # docker；打包盘可用 virt-make-fs 或 privileged docker
```

环境变量示例见 `.env.example`（`ROUNDPEN_QEMU_ENABLED`、`ROUNDPEN_BROWSER_IMAGE`）。

## 部署方式

| 模式 | 适用场景 |
|------|----------|
| **Docker Compose（推荐）** | NAS / 小团队一键私有化 |
| `./roundpend` 单二进制 | 本地开发、已有 PG |
| Kubernetes（规划中） | 企业集群 |

### 一键私有化（最终用户）

控制台已嵌入二进制，宿主机**不必安装 Node / Go**：

```bash
cp .env.compose.example .env   # 可选
docker compose up -d --build
# 浏览器打开 http://127.0.0.1:9527
# 首次启动：docker compose logs roundpend | head   # admin 密码与 API Key 各打印一次
```

Browser 槽位需要宿主机 `qemu-system-x86_64`、`qemu-img`，以及 `make browser-image` 产出的 `out/browser.qcow2` + `vmlinuz`/`initrd.img`（见 [docs/architecture/qemu-browser.md](docs/architecture/qemu-browser.md)）。

详见 [deploy/compose/README.md](deploy/compose/README.md)。

### 本地开发（贡献者）

```bash
make setup          # 首次：.env、go mod、npm
make dev            # pg0 → roundpend :19001 + UI :19000
# 浏览器打开 http://127.0.0.1:19000/
```

环境变量示例见 [.env.example](.env.example)。

## 技术选型

| 维度 | 选择 |
|------|------|
| 语言 | Go（跨平台单二进制） |
| API | 原生 REST（`/v1/...`） |
| Agent 后端 | Docker Daemon 或 `Kern` |
| Browser 后端 | QEMU（qcow2 + VNC unix + CDP hostfwd） |
| 记忆 | PostgreSQL + `pgvector` |
| 文件 | 本地目录或 SSH 远端（`WorkspaceFS`） |

## 路线图

- **近期**：固定环境模型 + Browser QEMU；删除多开沙箱 / E2B 兼容
- **中期**：Agent 独立 QEMU；镜像可视化定制加深
- **远期**：Mobile 槽位、集群扩展与企业能力

## 二进制与模块

- CLI：`roundpen`
- 守护进程：`roundpend`
- Go module：`github.com/RoundpenAI/roundpen`

## License

Apache License 2.0. 详见 [LICENSE](LICENSE)。
