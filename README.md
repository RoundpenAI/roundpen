# Roundpen

**轻量级、可私有化部署的 Agent 固定环境基础设施。**

Roundpen（驯马圈）为 AI Agent 提供隔离的执行环境、持久工作区、记忆与策略控制，并可在 NAS 与 Linux 服务器上自托管。控制面使用 Go 实现，提供原生 REST API 与 Web 控制台。

> 安全隔离的「驯马圈」，加上记忆与工具的「草料」、策略与审计的「缰绳」——在本地或私有环境里驯服 Agent。

## 特性

- **轻量自托管**：单二进制控制面，面向 NAS、笔记本与单机服务器
- **固定环境槽位**：登录即可用 **Cloud Agent**、**Browser**（及后续 Mobile），一槽位一机器；不是多开沙箱 SDK
- **后端钉死**：Agent 槽位固定 **Docker**（官方 OCI 镜像 pull / 离线 load）；Browser / Desktop / Mobile 固定 **QEMU**（qcow2 + CDP hostfwd + VNC unix sock）
- **可定制镜像**：Templates = 槽位镜像配方（`slot=agent|browser`）；Agent→OCI，Browser→qcow2
- **统一存储**：短期与长期记忆均使用 PostgreSQL（含 `pgvector`）
- **用户体系**：用户名/邮箱+密码（Cookie session）与每用户 API Key（入库为哈希）；沙箱/记忆按属主隔离
- **开源核心**：Apache 2.0；企业能力走 Open Core

## 产品模型

| 槽位 | 本期 | 形态 |
|------|------|------|
| Cloud Agent | Docker | coding / stdio ACP；官方 `code-agent` OCI 镜像 |
| Browser | QEMU | XFCE + Chrome；CDP + 主机 VNC→WebSocket |
| Mobile | 预留 | 后续独立 VM（QEMU） |

## 架构（摘要）

```
User → Agent env (OCI) + Browser env (qcow2/QEMU)
Browser: Chrome CDP :9222 via hostfwd；桌面 = QEMU -vnc unix:…/vnc.sock → /v1/me/environments/browser/desktop
```

| 层级 | 说明 |
|------|------|
| 控制面 | 网关、属主授权、最小审计、记忆、LLM 网关、`/v1/me/environments`。`policy` / `toolgw` 仍是空包 |
| 环境抽象 | Sandbox Manager + 用户槽位映射（`user_environments`） |
| 后端 | **Agent → Docker**；**Browser/Desktop/Mobile → QEMU**；`multi` 按 slot 路由 |
| 镜像 | `internal/template`（slot）+ `images/code-agent/`（官方 OCI）+ `images/browser-qemu/` |

仓库布局见 [docs/architecture/project-layout.md](docs/architecture/project-layout.md)。QEMU Browser 部署见 [docs/architecture/qemu-browser.md](docs/architecture/qemu-browser.md)。Agent 安全见 [docs/security.md](docs/security.md)。

## 核心能力

1. **固定环境**：`EnsureBrowser` / `EnsureAgent`；API `/v1/me/environments`；按属主隔离，admin 可看全部
2. **记忆与文件**：PostgreSQL + `/workspace` 挂载；长期记忆绑定登录身份
3. **LLM 网关**：Anthropic / OpenAI 透传；内部 Virtual Key；请求流水进 PG（默认不记 body）
4. **Web 控制台**：助手优先（`/a`）/ 设置；浏览器与镜像入口为高级/管理员
5. **ACP Agent 网关**：助手绑定会话 UI；Browser CDP 绑用户 Browser 环境
6. **最小审计**：创建 / 删除 / exec / settings 写 slog

助手产品模型见 [docs/superpowers/specs/2026-09-12-assistant-first-ui-design.md](docs/superpowers/specs/2026-09-12-assistant-first-ui-design.md)。对话默认安静执行；进度在助手详情「此刻」；卡壳时通过协助单升级人类。策略软拒绝（网络/目录/能力）可查询且可申请，不会自动刷单。

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

### 安装面（Agent 镜像）

Agent 固定使用 Docker，镜像通过以下方式获取（无需本机持有 Dockerfile）：

| 方式 | 命令 |
|------|------|
| 注册表 pull（默认） | `docker pull ghcr.io/roundpenai/code-agent:0.1.0`（首次 Ensure 自动 pull） |
| 离线安装 | Release 附 OCI tar：`docker load -i code-agent.tar` |
| 开发者本地构建 | `make code-agent-image`（`roundpen-code-agent:local`，用 `ROUNDPEN_AGENT_IMAGE` 覆盖） |

单二进制部署只需 Docker + 可达的注册表；`ROUNDPEN_AGENT_IMAGE` 可指向私有镜像。

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
| Agent 后端 | Docker（官方 `code-agent` OCI 镜像） |
| Browser 后端 | QEMU（qcow2 + VNC unix + CDP hostfwd） |
| 记忆 | PostgreSQL + `pgvector` |
| 文件 | 本地目录或 SSH 远端（`WorkspaceFS`） |

## 路线图

- **近期**：固定环境模型 + Browser QEMU；删除多开沙箱 / E2B 兼容；删除 Kern 与 Agent-QEMU 默认路径
- **中期**：镜像可视化定制加深；Agent 容器工作区增强
- **远期**：Mobile 槽位、集群扩展与企业能力

## 二进制与模块

- CLI：`roundpen`
- 守护进程：`roundpend`
- Go module：`github.com/RoundpenAI/roundpen`

## License

Apache License 2.0. 详见 [LICENSE](LICENSE)。
