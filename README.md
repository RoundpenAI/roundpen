# Roundpen

**轻量级、可私有化部署的 Agent 沙箱基础设施。**

Roundpen（驯马圈）为 AI Agent 提供隔离的执行环境、持久工作区、记忆与策略控制，并可在个人电脑、NAS 与服务器上自托管。控制面使用 Go 实现，默认兼容 [E2B](https://e2b.dev/) 协议，便于接入 DeepSeek Harness、AgentScope 等主流 Agent 框架。

> 安全隔离的「驯马圈」，加上记忆与工具的「草料」、策略与审计的「缰绳」——在本地或私有环境里驯服 Agent。

## 特性

- **轻量自托管**：单二进制控制面，面向低配 NAS、笔记本与单机服务器
- **跨平台**：Mac / Windows / Linux 统一构建与分发
- **可插拔后端**：默认 `Kern`（免守护、本地进程）；亦可 Docker，以及未来的 Kubernetes
- **可选 OCI 运行时**：`runc` / `crun` / `gVisor` / `Kata`，按安全与性能需求配置
- **统一存储**：短期与长期记忆均使用 PostgreSQL（含 `pgvector`）；文件落本地盘，元数据进库
- **生态友好**：E2B 兼容 API；REST / gRPC 供控制面集成
- **开源核心**：Apache 2.0；企业能力（SSO、多租户、合规审计等）走 Open Core

## 架构

```
┌─────────────────────────────────────────────────────┐
│                   控制面 (Go)                        │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐          │
│  │ 网关层   │ │ 策略引擎 │ │ 监控审计 │          │
│  └──────────┘ └──────────┘ └──────────┘          │
│  ┌──────────┐ ┌──────────┐ ┌──────────┐ ┌──────┐│
│  │ 记忆存储 │ │ 工具网关 │ │ LLM 网关 │ │E2B适配││
│  └──────────┘ └──────────┘ └──────────┘ └──────┘│
├─────────────────────────────────────────────────────┤
│              沙箱抽象层 (Sandbox Interface)          │
├─────────────────────────────────────────────────────┤
│              可插拔后端 (引擎 / 编排)                │
│  ┌───────┐ ┌──────────┐ ┌──────┐ ┌───────┐ ┌──────┐│
│  │Docker │ │Containerd│ │Podman│ │ Kern  │ │ K8s  ││
│  │Daemon │ │          │ │      │ │(免守护)│ │(未来)││
│  └───────┘ └──────────┘ └──────┘ └───────┘ └──────┘│
├─────────────────────────────────────────────────────┤
│         OCI 运行时层（由后端调用；Kern 可绕过）       │
│  ┌─────┐ ┌─────┐ ┌─────┐ ┌─────┐               │
│  │runc │ │crun │ │gVisor│ │Kata │               │
│  └─────┘ └─────┘ └─────┘ └─────┘               │
└─────────────────────────────────────────────────────┘
```

| 层级 | 说明 |
|------|------|
| 控制面 | 网关、策略、审计、记忆、工具 / LLM 网关、E2B 适配 |
| Sandbox 抽象 | 统一生命周期与 exec，不绑定具体引擎 |
| 可插拔后端 | Docker / Containerd / Podman / Kern / K8s |
| OCI 运行时 | 由引擎调用；`Kern` 作为免守护后端可绕过该层 |

仓库布局与包边界见 [docs/architecture/project-layout.md](docs/architecture/project-layout.md)。

## 核心能力

1. **沙箱执行**：`Sandbox` 接口抽象后端与运行时；本地默认 `Kern`，生产可用 Docker + `runc`
2. **记忆与文件**：PostgreSQL 承载短期（JSONB + TTL）与长期（`pgvector`）记忆；工作区目录挂载为沙箱内 `/workspace`。长期记忆 Agent API 对齐 mem0（`/v1/memories/add|search`），写入/检索时经 llmgw 自动 embedding
3. **策略引擎**：Token 预算、工具白名单、敏感内容过滤等动态围栏
4. **工具网关**：统一注册与调用，凭证隔离
5. **LLM 网关**：兼容 model-relay 的 Anthropic / OpenAI 透传；自动种子内部 Virtual Key（`vk-roundpen-internal`）与 embedding 别名（`roundpen-embed`）；请求流水进 PG
6. **监控审计**：执行轨迹、异常检测与强制终止

## 部署方式

| 模式 | 适用场景 |
|------|----------|
| `./roundpend` 单进程 | 本地开发、个人调试 |
| Docker Compose | NAS / 小团队一键私有化 |
| Kubernetes（规划中） | 企业集群扩展 |

本地开发推荐使用 [pg0](https://github.com/vectorize-io/pg0) 拉起带 `pgvector` 的 PostgreSQL，无需单独安装数据库。

```bash
make build
./bin/roundpen version

# 需要 PostgreSQL（推荐 pg0）
# pg0 start && createdb / 按 .env.example 设置 DATABASE_URL
export DATABASE_URL='postgres://postgres:postgres@127.0.0.1:5432/postgres?sslmode=disable'
export ROUNDPEN_BACKEND=kern
./bin/roundpend

# 另开终端：
curl -s localhost:9527/health
SID=$(curl -s -X POST localhost:9527/sandboxes -H 'Content-Type: application/json' \
  -d '{"templateID":"host","timeout":600}' | python3 -c 'import sys,json; print(json.load(sys.stdin)["sandboxID"])')
curl -s -X POST localhost:9527/v1/sandboxes/$SID/exec -H 'Content-Type: application/json' \
  -d '{"command":["/bin/sh","-c","echo hi > note.txt && cat note.txt"]}'
curl -s -X DELETE localhost:9527/sandboxes/$SID -o /dev/null -w '%{http_code}\n'
```

环境变量示例见 [.env.example](.env.example)。

## 技术选型

| 维度 | 选择 |
|------|------|
| 语言 | Go（跨平台单二进制） |
| 协议 | E2B 兼容；REST + gRPC |
| 默认后端 | `Kern`（免守护主机进程，适合无 Docker 本地开发） |
| 生产常用后端 | Docker Daemon（`runc`） |
| 轻量 / 加固运行时 | `crun`；`gVisor`；`Kata`（需 KVM，Linux 优先） |
| 免守护后端 | `Kern`（与 Docker 同层抽象） |
| 记忆 | PostgreSQL + `pgvector`（MVP 不引入 Redis / 专用向量库） |
| 文件 | 本地目录或 SSH 远端目录（`WorkspaceFS`）；元数据在 PG；后期可接 S3 |

| 许可 | Apache 2.0（Open Core） |

**平台说明**：Mac 推荐 OrbStack + Docker；Windows 使用 Docker Desktop + WSL2；`gVisor` / `Kata` 以 Linux 为主，其他平台按能力降级或禁用。

**集成**：可作为 DeepSeek Harness、AgentScope 等的底层沙箱执行器；K8s 预留适配接口，但不做成 Operator。

## 路线图

- **近期**：单机与 Compose 私有化可用，跑通主流 Agent 框架对接
- **中期**：更丰富的隔离与后端选择，覆盖从个人调试到小团队自托管
- **远期**：集群扩展与企业能力（多租户、SSO、合规审计），以及可选的托管服务

## 二进制与模块

- CLI：`roundpen`
- 守护进程：`roundpend`
- Go module：`github.com/RoundpenAI/roundpen`

## License

Apache License 2.0. 详见 [LICENSE](LICENSE)。
