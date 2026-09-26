# Agent 安全与 Roundpen 设计

本文从**使用者视角**说明当前 AI Agent 的主要安全风险，阐述 Roundpen 的安全设计立场、架构原则与能力边界，并列出规划中的能力。

对外宣传稿（更易读、少技术细节）：[agent-security.md](agent-security.md)。  
相关文档：[architecture/project-layout.md](architecture/project-layout.md)、[architecture/environment-services.md](architecture/environment-services.md)、[auth.md](auth.md)。  
实现走查（安全缺口与设计债，2026-09-08）：[reviews/2026-09-08-security-and-design.md](reviews/2026-09-08-security-and-design.md)。

## 背景：使用者真正担心什么

不同于传统 Web 应用，Agent 的安全边界更模糊。部署者关心的往往不是抽象的漏洞分类，而是以下几类切身问题：

| 关切 | 典型场景 |
|------|----------|
| 执行边界 | Agent 在宿主机上跑 shell、读写文件，误操作或注入后可能破坏源码与配置 |
| 凭证泄露 | LLM API Key、GitHub Token、数据库连接串出现在配置、日志或记忆中 |
| 网络滥用 | 装依赖、读文档、调 webhook 等隐式出站，可能被诱导外传数据或扫描内网 |
| 暴露面 | 沙箱内启动的本地服务通过预览链接对外，若弱鉴权则等同公开内网服务 |
| 不可追溯 | 缺乏执行与调用记录，事故后难以复盘 |
| 数据主权 | 云沙箱便捷，但数据路径不可见，无法满足自托管与合规要求 |
| 记忆风险 | 长期记忆被污染，或在多用户场景下产生意外信息交叉 |

这些担忧叠加，常使团队陷入「Demo 惊艳、生产不敢开」的困境。Roundpen 的定位，是为 Agent 提供**默认安全、可自托管、可演进**的执行底座，而非替代 Agent 框架本身。

## 设计前提

在实现之前，我们确立了以下判断，作为架构与产品决策的约束：

1. **Agent 需要执行环境，但执行环境不应等于宿主机。** 隔离是底座，不是可选项。
2. **真实凭证不应由 Agent 进程持有。** 模型与框架会出错、会被注入；密钥应留在控制面，对外仅暴露可吊销的代理凭证。
3. **安全默认开启。** 预览、终端、管理接口等对外能力默认需要身份验证；放宽策略应显式配置，而非反过来补救。
4. **自托管与开放协议并重。** 数据应能留在用户自己的环境中，同时能接入主流 Agent 框架，避免厂商锁定。
5. **安全是分层防御。** 基础设施负责执行边界与访问控制；框架负责意图与交互；使用者仍须落实备份、最小权限与密钥轮换。没有任何单层能单独兜底。

## 架构概览

Roundpen 按「控制面 → 沙箱抽象 → 后端引擎 → 运行时」分层：

```
Agent / 框架
      │
Environment Services   ← 终端、工作区、端口预览（及后续 Browser 等）
      │
Sandbox Manager        ← 生命周期、执行、超时
      │
Backend                ← Agent / Browser: Docker；Desktop/Mobile: QEMU（规划：Kubernetes）
      │
OCI Runtime            ← runc / gVisor / Kata 等（Agent 容器按部署选择）
```

| 层级 | 职责 |
|------|------|
| 控制面 | 鉴权、策略、审计、记忆、LLM 与工具网关 |
| Environment Services | Agent 操作面：文件、终端、预览等 |
| Sandbox 抽象 | 统一生命周期与 exec，不绑定具体引擎 |
| Backend | 槽位固定后端：**Agent / Browser → Docker**；**Desktop / Mobile → QEMU**（规划：Kubernetes） |
| OCI Runtime | Agent 容器由后端选用 runc / gVisor / Kata 等 |

控制面与执行面职责分离：安全策略集中在控制面执行，更换后端引擎时不必重写规则。

## 设计原则

### 1. 槽位后端钉死

用户不应被要求选择运行时。Roundpen 按槽位固定后端，减少错误配置面：

- **Agent → Docker**：容器默认丢弃特权（`CapDrop: ALL`）、禁止提权（`no-new-privileges`），可按需选用 `runc`、`crun`、`gVisor`、`Kata` 等 OCI 运行时。镜像从官方注册表 pull 或离线 load。
- **Browser → Docker**：browserless/chrome 容器提供 CDP 与实时调试画面（不发布端口，控制面经容器网络拨入；容器内开鉴权 token）；与 Agent 容器同为 Docker 引擎、按属主隔离。
- **Desktop / Mobile → QEMU**（预留）：qemu 虚拟机提供画面。
- **Kubernetes**（规划中）：面向集群扩展，预留 Backend 接口。

同一套 API、按槽位固定后端——不再提供 QEMU/Kern/Docker 三选一。

### 2. 工作区有边界，文件访问可管

- 工作区挂载为沙箱内 `/workspace`，与宿主机目录一一对应。
- 所有相对路径经规范化校验，**禁止 `..` 逃逸**出工作区根目录；指向工作区外的 symlink 不可读、不可写、不可 `RemoveAll` 跟随。
- 文件读写优先经控制面 `WorkspaceFS` API，而非依赖容器内 shell；沙箱停止后，经认证用户仍可管理工作区，同时减少容器内攻击面。

详见 [architecture/environment-services.md](architecture/environment-services.md) 中 Workspace 一节。

### 3. 虚拟凭证，真实密钥不进 Agent

**LLM 网关（llmgw）** 采用 Virtual Key 模型：Agent 与内部服务仅持有虚拟密钥；网关在转发时替换为真实上游 Key，并将请求流水写入 PostgreSQL。轮换、吊销与审计集中在控制面。

**工具网关（toolgw）**（规划中，见下文「规划能力」）将采用相同思路：工具调用经统一入口，凭证与 Agent 进程隔离。

设置接口对密钥字段回显掩码，避免通过 API 响应意外泄露。

落库密钥经应用层 AES-256-GCM 加密（`enc:v1:` 前缀）：上游 LLM Key、CDP Token、Web 搜索密钥与旧版虚拟密钥串在 PostgreSQL 中不以明文存储。主密钥来自 `ROUNDPEN_SECRET_KEY`（hex/base64，32 字节）；未配置时自动生成并持久化到 `$ROUNDPEN_DATA_ROOT/secret.key`（0600）——**务必备份该文件**，丢失后已加密的值不可恢复（设置项会被置空并告警，需在设置页重新录入）。历史明文行保持可读，并在下次保存时自动转为密文。内部虚拟密钥同样随机生成（`data/llmgw-internal.key`），旧版固定常量 `vk-roundpen-internal` 在启动时从库中清除。

### 4. 对外能力默认需认证

自托管场景下，不能假设「知晓短 ID 即可访问」。Roundpen 的约束包括：

- 管理 API、终端 WebSocket：需 Cookie 会话或 `rp-...` API Key（见 [auth.md](auth.md)）。
- 端口预览：须由已认证用户通过 `preview-link` 签发**短时令牌**（默认 15 分钟）。同源部署（默认）下令牌经路径限定（`/p/{id}/{port}`）的 `HttpOnly` Cookie 下发，不出现在 URL 中，避免经浏览器历史、访问日志与 Referer 泄露，预览应用的子资源请求自动携带；配置独立预览域名（`ROUNDPEN_PREVIEW_PUBLIC_URL` 指向其他主机）时，首次导航仍经 `?token=` 传递（控制台域的 Cookie 无法送达预览域），代理随后在预览域自行种下 Cookie（跨站 iframe 场景为 `SameSite=None; Secure`，要求 HTTPS）。配 `ROUNDPEN_PREVIEW_DOMAIN` 时每个端口独占一个子域（`{id}-{port}.{域名}`），该 Cookie 不带 `Domain`，只随对应子域发送，兄弟预览互不可见；控制台会话 Cookie 同样不带 `Domain`，因此预览子域上只有短时令牌这一条通路，域名通配区间内无法解析成 `{id}-{port}` 或已登记名字的主机直接 404，不会回落到控制台。子域还可由用户/Agent 通过 `POST /v1/preview-domains` **登记名字**（`{name}.{域名}`，全局先到先得，绑到自己的沙箱端口）：登记只决定"名字指向谁"，访问仍要目标端口的短时令牌，其他用户即使知道名字也拿不到内容；已被他人占用的名字返回 409，本人重复登记等于改绑，管理员可释放任何名字。
- 登录接口按 IP 限流，减缓暴力尝试。`X-Forwarded-*` 仅在 `ROUNDPEN_TRUSTED_PROXIES`（CIDR 列表）命中时生效。

预览与终端**不**提供默认无鉴权访问——这是自托管安全模型的底线。

进一步加固：默认部署中预览与控制台**同源**，沙箱内任意 Web 应用的脚本运行在控制台 Origin 上，理论上可携用户会话调用控制台 API。若要运行不可信项目，建议将 `ROUNDPEN_PREVIEW_PUBLIC_URL` 指向独立域名，或直接启用 `ROUNDPEN_PREVIEW_DOMAIN` 子域预览（两者都由同一 roundpend 实例服务，经 DNS / 反代路由），使预览内容与控制台会话跨站隔离；该模式要求 HTTPS。

### 5. 记忆分层，数据路径清晰

- 短期记忆（JSONB + TTL）与长期向量记忆（`pgvector`）均存 PostgreSQL；文件落本地盘或用户配置的远端目录（`WorkspaceFS`）。
- 记忆按 `agent_id`、`user_id` 等维度索引；非 admin 的读写被强制绑定登录身份，不能靠客户端自报 `user_id` 跨用户。长期记忆的 embedding 经内部 LLM 网关别名完成，Agent 不直接接触 embedding 上游密钥。
- 数据路径透明，便于备份、迁移与合规审查。

### 6. 开放协议，降低接入成本

Roundpen 提供原生 REST API 与 Web 控制台；用户登录后获得固定 Agent / Browser 环境槽位，便于私有化部署与策略审计。

## 已交付能力

| 能力 | 说明 |
|------|------|
| 沙箱隔离与生命周期 | 创建、执行、停止、超时；按属主隔离；Agent / Browser 用 Docker（Desktop / Mobile 预留 QEMU） |
| 工作区与文件 API | 路径与 symlink 边界校验；Agent 工作区经容器读写（与 Agent 同身份） |
| 终端与端口预览 | 认证终端 WebSocket（同源 Origin）；预览须短时令牌（同源经 HttpOnly Cookie 下发，不入 URL） |
| 用户体系 | 密码登录 + 每用户 API Key；登录限流 |
| LLM 网关 | Virtual Key 代理、多上游、请求流水；上游密钥加密落库（AES-GCM） |
| 记忆服务 | 短期 JSONB + 长期向量；mem0 风格 Agent API |
| 私有化部署 | 单二进制或 Docker Compose；适合 NAS、单机、小团队 |
| Web 控制台 | 沙箱、文件、终端、预览管理 |
| 镜像 | 应用内不再构建镜像；镜像由 CI 构建推送，平台按 `ROUNDPEN_AGENT_IMAGE` / `ROUNDPEN_BROWSER_IMAGE` 拉取 |

## 规划能力

以下能力已列入产品路线图，**尚未完整落地**（部分曾以占位包预留，现已移除）。列出它们是为了说明方向连贯，并界定当前版本边界。

| 能力 | 目标 | 状态 |
|------|------|------|
| 策略引擎（`policy`） | 网络出站 / 能力开关 / 目录授权边界，软拒绝可申请开通；Token 预算与工具白名单待补 | 边界检查已接入控制面（assistant）；Token 预算未实现 |
| 工具网关（`toolgw`） | 统一注册与调用、凭证隔离、与策略联动 | 规划中（占位包已移除） |
| 审计与可观测（`audit`） | 执行轨迹、异常检测、强制终止 | 最小 slog 记录（创建 / 删除 / exec / settings）；非围栏 |
| 出站网络策略 | 细粒度控制沙箱可访问的域名与地址 | 路线图 |
| 加固运行时 | gVisor、Kata 等的生产级配置与文档 | 部分可配置，文档与默认方案完善中 |
| Kubernetes 后端（`backend/k8s`） | 集群环境下的沙箱调度 | 规划中（占位包已移除） |
| 企业能力 | 多租户、SSO、合规级审计 | Open Core / 远期 |
| 更多环境形态 | Desktop、Mobile 等 Agent 操作面（Browser 已交付） | Environment Services 扩展 |

路线图阶段划分见仓库 [README.md](../README.md#路线图)。

## 刻意约束：我们不做什么

设计同样体现在边界上。当前阶段**不**引入以下复杂度：

| 约束 | 原因 |
|------|------|
| 不在沙箱内默认运行重型守护进程 | 控制面保持薄，镜像与攻击面更小 |
| 不默认做 idmapped / PUID 写路径 | Agent 工作区读写统一经容器（同身份），避免宿主 UID 与 guest UID 不一致 |
| 不提供默认无鉴权的预览或终端 | 自托管威胁模型不允许 |
| 不做云厂商锁定方案 | 自托管与开放协议是产品核心 |

## 使用者责任：分层防御

Roundpen 主要解决**执行边界、访问控制、凭证代理与部署主权**。以下风险仍需在上层与流程中处理：

- **Prompt injection**：需结合工具限制、人工确认、输入审查等。
- **模型行为**：幻觉、误删——Git、备份、快照仍是必需品。
- **密钥与权限**：代理与审计由基础设施提供；轮换策略与最小权限由使用者落实。

## 小结

Roundpen 的安全立场可概括为：

- **用沙箱隔离执行** — Agent 在可控环境中运行，而非直接在宿主机上操作。
- **用网关隔离凭证** — 真实密钥留在控制面，对外仅暴露可管理的虚拟凭证。
- **用属主与最小审计收紧默认** — 沙箱 / 记忆按登录身份隔离；`policy` 已接入网络 / 能力 / 目录边界与软拒绝，但 Token 预算与工具网关（`toolgw`）仍未交付，不能当成完整围栏。
- **用开放协议守住自由** — 数据留在用户环境，框架集成不必从零开始。

安全是持续演进的能力，而非一次性开关。本文将随实现进展更新「已交付」与「规划能力」各节。
