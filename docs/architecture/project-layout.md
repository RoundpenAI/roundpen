# Roundpen 项目结构

单 Go module，按「控制面 → 环境抽象 → 后端 → 存储」分层。私有化 Linux / NAS：Agent 槽位 Docker/Kern（Kern 为开发档弱隔离），Browser 槽位 QEMU。`policy` / `toolgw` 仍是空包。

## 目录树

```
roundpen/
├── cmd/roundpend/             # 控制面守护进程（嵌入 UI）
├── web/                       # 控制台 SPA（Chats / Browser / Images）
├── images/browser-qemu/       # 默认 Browser qcow2 配方
├── internal/
│   ├── api/
│   │   ├── platform/          # /v1/sandboxes + /v1/templates（内部生命周期）
│   │   ├── envapi/            # /v1/me/environments（固定槽位）
│   │   ├── httpapi/           # exec / files / terminal
│   │   ├── agentapi/          # Agent sessions + browser CDP UI
│   │   └── auth/
│   ├── authz/                 # 请求 Actor（属主 / admin）
│   ├── httpx/                 # 可信代理、ClientIP / Scheme
│   ├── userenv/               # 用户 → agent/browser 槽位映射
│   ├── template/              # 槽位镜像（slot=agent|browser|mobile）
│   ├── sandbox/               # 环境生命周期 Manager（按属主隔离）
│   ├── backend/
│   │   ├── docker/            # Agent OCI
│   │   ├── kern/              # 本地免守护（弱隔离）
│   │   ├── qemu/              # Browser/Agent VM（通用）
│   │   └── multi/             # 按 slot 路由
│   ├── browser/               # CDP Hub（Dial 进 Browser env）
│   ├── acp/                   # ACP / sysagent
│   ├── audit/                 # 最小 slog 审计
│   └── …
├── migrations/
└── docs/architecture/
    ├── qemu-browser.md
    └── …
```

## 包边界

| 包 | 职责 |
|----|------|
| `api/platform` | 内部沙箱/模板 HTTP（非对外多开 SDK）；模板写操作需 admin |
| `api/envapi` | 固定环境 Ensure + 桌面 WS |
| `authz` / `httpx` | 属主上下文；可信代理下的 IP / HTTPS 判定 |
| `userenv` | PG `user_environments` |
| `template` | slot 镜像配方与构建产物 |
| `backend/qemu` | qcow2 生命周期、CDP hostfwd、VNC unix sock |
| `backend/multi` | agent→primary，browser→qemu |

## 装配

`roundpend`：config → 可信代理 → PG migrate → seed templates → docker|kern + optional qemu → multi → sandbox Manager → platform / envapi / agentapi / browser Hub。

## 演进

- 本期：删除 E2B 兼容；Browser QEMU；Agent 仍 Docker
- 二期：独立 Agent QEMU；Mobile 槽位
