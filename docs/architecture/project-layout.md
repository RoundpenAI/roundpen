# Roundpen 项目结构

单 Go module，按「控制面 → Sandbox 抽象 → 后端 → 存储」分层。当前默认后端是 Kern（开发档弱隔离）；Docker 是生产档。`policy` / `toolgw` 仍是空包。

## 目录树

```
roundpen/                      # 仓库根
├── cmd/
│   ├── roundpend/             # 控制面守护进程（主入口；嵌入 UI）
│   └── roundpen/              # CLI（创建沙箱、exec、日志等）
├── web/                       # 控制台 SPA 源码（Vite + React；产物 → internal/ui/dist）
├── compose.yaml               # 一键私有化（postgres + roundpend）
├── internal/
│   ├── config/                # 环境变量 / 配置加载
│   ├── api/
│   │   ├── httpapi/           # 原生 REST（管理、就绪检查）
│   │   ├── e2b/               # E2B 兼容 HTTP 适配
│   │   └── auth/              # 用户体系：密码 session + 每用户 API key
│   ├── ui/                    # //go:embed dist — 控制台静态资源
│   ├── sandbox/               # Sandbox 领域模型 + Manager（不绑具体后端）
│   ├── backend/               # 可插拔引擎接口
│   │   ├── docker/            # 生产档：Docker Daemon
│   │   ├── kern/              # 默认开发档：免守护本机进程（弱隔离）
│   │   └── k8s/               # 占位
│   ├── memory/                # 短期 JSONB + 长期 pgvector；mem0 风格 Agent API + llmgw 自动 embed
│   ├── workspace/             # WorkspaceFS 接口 + local / sshfs
│   │   ├── local/             # 本机目录（Kern / 本地 Docker）
│   │   └── sshfs/             # SSH 远端目录（DOCKER_HOST=ssh://…）
│   ├── storage/               # PG 连接、迁移辅助、通用 store
│   ├── policy/                # 空包（未接入）
│   ├── toolgw/                # 空包（未接入）
│   ├── llmgw/                 # LLM 网关（内部 vkey + roundpen-embed 别名；PG 流水）
│   ├── audit/                 # 最小 slog 审计（创建 / 删除 / exec / settings）
│   └── observability/         # 日志 / metrics 钩子
├── migrations/                # SQL 迁移（与 schema 折叠策略后续定）
├── deploy/
│   ├── Dockerfile             # 多阶段：Node 编 UI → Go embed → Alpine 运行
│   └── compose/               # Compose 说明
├── scripts/                   # 开发辅助（pg0、本地启动）
├── docs/                      # 设计与规格
├── tests/                     # 集成 / e2e（按需）
├── go.mod
├── Makefile
├── LICENSE                    # Apache-2.0
└── README.md
```

## 包边界

| 包 | 职责 | 依赖方向 |
|----|------|----------|
| `sandbox` | 生命周期、领域类型、`Manager` | → `backend`, `workspace`, `storage` |
| `backend` | `Backend` 接口；创建/停/exec/日志 | 不依赖 `api` |
| `backend/docker` | Docker Engine API | → `backend` |
| `workspace` | `FS` 接口；挂载路径约定 | 无反向依赖 |
| `memory` | 短期 JSONB + 长期向量 | → `storage` |
| `api/e2b` | 协议适配，无业务逻辑 | → `sandbox` |
| `api/auth` | 登录 session、API key、admin 用户 | → `storage` |
| `llmgw` / `audit` | LLM 网关与最小审计 | → `storage` / slog；被 `api` / `sandbox` 调用 |
| `policy` / `toolgw` | 空包，不是围栏 | 无调用方 |

禁止：`backend` 依赖 `api`；`storage` 依赖 `sandbox`。

## 二进制

- `roundpend`：加载 config → 连 PG → 装配 Backend / WorkspaceFS / Manager → 挂 HTTP（含 E2B）
- `roundpen`：轻量 CLI，调 `roundpend` 的 HTTP API

## Module path

`github.com/RoundpenAI/roundpen`（与 GitHub 仓库 [RoundpenAI/roundpen](https://github.com/RoundpenAI/roundpen) 一致）。

## Phase 对照

| Phase | 结构变化 |
|-------|----------|
| 1 MVP | `backend/docker`、`api/e2b`、`storage`、`workspace/local`、`memory` 骨架 |
| 2 | 充实 `Sandbox` 接口；加 `backend/kern`、OCI runtime 选项 |
| 2.5 | Environment P0：Workspace HTTP、Ports/`Dial`、Terminal PTY（见 [environment-services.md](./environment-services.md)） |
| 3 | gVisor / Kata 作为 docker/runtime 配置，不必新顶层包 |
| 4 | `backend/k8s` |
| 5 | 商业能力可放 `internal/enterprise/`（仍 Apache 边界清晰）或独立私有 module |

## 相关设计

- [environment-services.md](./environment-services.md) — 沙箱之上的 Terminal / Workspace / Ports（及后续 Browser 等 Surface）
- [../security.md](../security.md) — Agent 安全设计、能力边界与规划（技术向）
- [../agent-security.md](../agent-security.md) — Agent 安全对外宣传稿
