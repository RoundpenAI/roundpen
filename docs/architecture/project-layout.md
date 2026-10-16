# Roundpen 项目结构

单 Go module，按「控制面 → 环境抽象 → 执行引擎 → 存储」分层。在 Linux / NAS 上，Agent 和 Browser 用 Docker 跑（官方 `code-agent` 镜像 / `browserless/chrome`）；手机/桌面虚拟机槽位以后用 QEMU。

两个前端互不引用：`web/`（控制台，npm）与 `mobile/`（手机 App，Flutter）。后者和上面的「手机虚拟机槽位」只是名字像，文档里写全称「移动端控制台」。

## 目录树

```
roundpen/
├── cmd/roundpend/             # 控制面守护进程（嵌入 UI）
├── web/                       # 控制台 SPA（Chats / Browser / Images）
├── mobile/                    # 移动端控制台（Flutter 客户端），非 Mobile 槽位
├── images/                    # 镜像配方：code-agent（OCI）；Browser 用上游 browserless/chrome
├── internal/
│   ├── api/
│   │   ├── platform/          # /v1/sandboxes + 镜像目录列表（内部生命周期）
│   │   ├── envapi/            # /v1/me/environments（固定槽位）
│   │   ├── httpapi/           # exec / files / terminal
│   │   ├── agentapi/          # Agent sessions + browser CDP UI
│   │   └── auth/
│   ├── authz/                 # 请求 Actor（属主 / admin）
│   ├── oauth/                 # GitHub / Gitea 联合登录（provider 配置、授权流程、身份→git 凭据）
│   ├── gitcred/               # 用户 git 凭据（手填 PAT + OAuth token 合并）与 guest 注入
│   ├── httpx/                 # 可信代理、ClientIP / Scheme
│   ├── userenv/               # 用户 → agent/browser 槽位映射
│   ├── template/              # 槽位镜像目录（启动按 env 播种，slot=agent|browser|mobile）
│   ├── sandbox/               # 环境生命周期 Manager（按属主隔离）
│   ├── backend/
│   │   ├── docker/            # Agent / Browser 容器
│   │   ├── qemu/              # Desktop/Mobile VM 落点（Browser 已迁 Docker）
│   │   └── multi/             # 按 slot 路由（agent/browser→docker，mobile→qemu）
│   ├── browser/               # Playwright 引擎 + CDP Hub（托管容器 / 外部来源）
│   ├── acp/                   # ACP / sysagent
│   ├── audit/                 # 最小 slog 审计
│   └── …
├── migrations/
└── docs/architecture/
    ├── browser-env.md
    └── …
```

## 包边界

| 包 | 职责 |
|----|------|
| `api/platform` | 内部沙箱/镜像目录 HTTP（非对外多开 SDK）；沙箱写操作需 admin |
| `api/envapi` | 固定环境 Ensure + Browser 实时视图反代 |
| `authz` / `httpx` | 属主上下文；可信代理下的 IP / HTTPS 判定 |
| `oauth` | 联合登录：`oauth_providers` / `user_identities` / `oauth_states` 三表，回调复用 `auth.IssueSession` |
| `gitcred` | `MergeCreds` 合并 PAT（优先）与 OAuth token，`InstallScript` 写入 guest `$HOME` |
| `settingitems` | 多 item 设置的通用机制：kind/item/slot/binding、原子快照解析、密钥逐字段密封（spec: `docs/superpowers/specs/2026-09-21-multi-item-settings-design.md`） |
| `search` / `browser`（item） | 搜索后端与 CDP 来源作为 item 暴露给槽位（`search.Resolve`、`browser.Resolve`） |
| `userenv` | PG `user_environments` |
| `template` | slot 镜像目录（env 播种的 artifact 与资源规格，创建沙箱时解析） |
| `backend/qemu` | qcow2 生命周期、CDP hostfwd、VNC unix sock（Desktop/Mobile 落点） |
| `backend/multi` | agent/browser→docker，mobile→qemu |

## 装配

`roundpend`：config → 可信代理 → PG migrate → settings bootstrap → setting items（迁移旧配置为 item/binding）→ seed templates → docker + optional qemu → multi → sandbox Manager → platform / envapi / agentapi / browser Hub。

集成类配置（LLM provider、出口代理、搜索后端、浏览器来源）是 `setting_items` 里的条目，使用点（槽位）通过 `setting_bindings` 选择：解析链为「用户覆盖 → 管理员全局默认 → 槽位默认 → 回退槽位」。LLM relay 按 `/llmgw/<item id>/...` 路由，`openai`/`anthropic` 是迁移时播种的默认 id。

## 演进

- 已做：去掉 E2B 兼容与 Kern；Agent / Browser 都走 Docker（browserless/chrome + Playwright）
- 下一步：Browser 实时视图；手机虚拟机槽位（QEMU）
