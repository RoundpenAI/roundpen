# Roundpen 项目结构

单 Go module，按「控制面 → Sandbox 抽象 → 后端 → 存储」分层。Phase 1 只实现 Docker 后端 + E2B HTTP + PG + 本地工作区；其余目录预留，不提前写死实现。

## 目录树

```
roundpen/                      # 仓库根
├── cmd/
│   ├── roundpend/             # 控制面守护进程（主入口）
│   └── roundpen/              # CLI（创建沙箱、exec、日志等）
├── internal/
│   ├── config/                # 环境变量 / 配置加载
│   ├── api/
│   │   ├── httpapi/           # 原生 REST（管理、就绪检查）
│   │   └── e2b/               # E2B 兼容 HTTP 适配
│   ├── sandbox/               # Sandbox 领域模型 + Manager（不绑具体后端）
│   ├── backend/               # 可插拔引擎接口
│   │   ├── docker/            # Phase 1：Docker Daemon
│   │   ├── kern/              # Phase 2：免守护后端（占位）
│   │   └── k8s/               # Phase 4：占位
│   ├── memory/                # 短期 / 长期记忆（PG + pgvector）
│   ├── workspace/             # WorkspaceFS 接口 + local 实现
│   ├── storage/               # PG 连接、迁移辅助、通用 store
│   ├── policy/                # 策略引擎（Token / 工具白名单等）
│   ├── toolgw/                # 工具网关
│   ├── llmgw/                 # LLM 网关（API Key 保险柜）
│   ├── audit/                 # 审计与轨迹
│   └── observability/         # 日志 / metrics 钩子
├── migrations/                # SQL 迁移（与 schema 折叠策略后续定）
├── deploy/
│   └── compose/               # Docker Compose 私有化
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
| `policy` / `toolgw` / `llmgw` / `audit` | 控制面横切能力 | → `storage`；被 `api` / `sandbox` 调用 |

禁止：`backend` 依赖 `api`；`storage` 依赖 `sandbox`。

## 二进制

- `roundpend`：加载 config → 连 PG → 装配 Backend / WorkspaceFS / Manager → 挂 HTTP（含 E2B）
- `roundpen`：轻量 CLI，调 `roundpend` 的 HTTP/gRPC（MVP 可先 HTTP）

## Module path

`github.com/RoundpenAI/roundpen`（与 GitHub 仓库 [RoundpenAI/roundpen](https://github.com/RoundpenAI/roundpen) 一致）。

## Phase 对照

| Phase | 结构变化 |
|-------|----------|
| 1 MVP | `backend/docker`、`api/e2b`、`storage`、`workspace/local`、`memory` 骨架 |
| 2 | 充实 `Sandbox` 接口；加 `backend/kern`、OCI runtime 选项 |
| 3 | gVisor / Kata 作为 docker/runtime 配置，不必新顶层包 |
| 4 | `backend/k8s` |
| 5 | 商业能力可放 `internal/enterprise/`（仍 Apache 边界清晰）或独立私有 module |
