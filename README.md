# Roundpen

**轻量级、可私有化部署的 Agent 固定环境基础设施。**

Roundpen（驯马圈）为 AI Agent 提供隔离的执行环境、持久工作区、记忆与策略控制，并可在 NAS 与 Linux 服务器上自托管。控制面使用 Go 实现，提供原生 REST API 与 Web 控制台。

> 安全隔离的「驯马圈」，加上记忆与工具的「草料」、策略与审计的「缰绳」——在本地或私有环境里驯服 Agent。

## 特性

- **轻量自托管**：单二进制控制面，面向 NAS、笔记本与单机服务器
- **固定环境槽位**：登录即可用 **Cloud Agent**、**Browser**（及后续 Mobile），一槽位一环境；不是多开沙箱 SDK
- **后端钉死**：Agent 与 Browser 固定 **Docker**（官方 OCI 镜像 pull / 离线 load）；Desktop / Mobile 预留 **QEMU**
- **可定制镜像**：镜像由 CI 构建推送；控制面启动时按 `ROUNDPEN_AGENT_IMAGE` / `ROUNDPEN_BROWSER_IMAGE` 播种镜像目录（`slot=agent|browser`），均为 OCI 镜像
- **来源可配**：Browser 默认用 Roundpen 托管的 browserless 容器，也可指向局域网 / 商业云 / 本机 Chrome（Playwright 引擎）
- **统一存储**：短期与长期记忆均使用 PostgreSQL（含 `pgvector`）
- **用户体系**：用户名/邮箱+密码（Cookie session）与每用户 API Key（入库为哈希）；沙箱/记忆按属主隔离
- **开源核心**：Apache 2.0；企业能力走 Open Core

## 产品模型

| 槽位 | 本期 | 形态 |
|------|------|------|
| Cloud Agent | Docker | coding / stdio ACP；官方 `code-agent` OCI 镜像 |
| Browser | Docker | browserless/chrome 容器；CDP + 自带 debugger 实时视图（来源可配：托管/局域网/云/本机） |
| Mobile | 预留 | 后续独立 VM（QEMU） |

> `Mobile` 槽位是给 Agent 用的手机环境（QEMU，预留），和客户端没关系。
> 手机上用的客户端叫 **移动端控制台**（`mobile/`，Flutter），见「本地开发（贡献者）」。

## 架构（摘要）

```
User → Agent env (OCI) + Browser env (browserless/chrome 容器)
Browser: 控制面拨入容器 CDP :3000；实时视图 = 容器内 browserless debugger 经 /v1/me/environments/browser/live/ 反代
```

| 层级 | 说明 |
|------|------|
| 控制面 | 网关、属主授权、最小审计、记忆、LLM 网关、策略边界（`policy`：网络 / 能力 / 目录 + 软拒绝）、`/v1/me/environments` |
| 环境抽象 | Sandbox Manager + 用户槽位映射（`user_environments`） |
| 后端 | **Agent / Browser → Docker**；**Desktop / Mobile 预留 QEMU**；`multi` 按 slot 路由 |
| 镜像 | CI 构建（`images/code-agent/` → ghcr）+ `internal/template` 镜像目录（启动时按 env 播种）+ `ghcr.io/browserless/chrome`（Browser，pull） |

仓库布局见 [docs/architecture/project-layout.md](docs/architecture/project-layout.md)。Browser 环境见 [docs/architecture/browser-env.md](docs/architecture/browser-env.md)。Agent 安全见 [docs/security.md](docs/security.md)。

## 核心能力

1. **固定环境**：`EnsureBrowser` / `EnsureAgent`；API `/v1/me/environments`；按属主隔离，admin 可看全部
2. **记忆与文件**：PostgreSQL + `/workspace` 挂载；长期记忆绑定登录身份
3. **LLM 网关**：Anthropic / OpenAI 透传；内部 Virtual Key；请求流水进 PG（默认不记 body）
4. **Web 控制台**：助手优先（`/a`）/ 设置（个人 + 系统管理）；浏览器入口为高级页
5. **ACP Agent 网关**：助手绑定会话 UI；Browser CDP 绑用户 Browser 环境
6. **最小审计**：创建 / 删除 / exec / settings 写 slog
7. **议题与任务跟踪**：对话中产生的议题落库（`ISS-n`），澄清边界/方向/决策后写版本化 Spec / Plan（`DOC-n`），拆成任务清单（`TSK-n`）逐个实现；控制台 `/issues` 可读可改

助手产品模型见 [docs/superpowers/specs/2026-09-12-assistant-first-ui-design.md](docs/superpowers/specs/2026-09-12-assistant-first-ui-design.md)。对话默认安静执行；进度在助手详情「此刻」；卡壳时通过协助单升级人类。策略软拒绝（网络/目录/能力）可查询且可申请，不会自动刷单。

## 快速开始

见下文 Docker Compose / `make dev`。Browser 环境是一个 Docker 容器（browserless/chrome），首次使用自动 pull；引擎的 Playwright driver 已烘焙在官方镜像内（`/opt/playwright`），首次使用无需下载：

```bash
make browser-driver   # 裸机 / 开发环境可选：预装 Playwright driver（只装 driver，不下载浏览器）
```

环境变量示例见 `.env.example`（`ROUNDPEN_CDP_PROVIDER`、`ROUNDPEN_BROWSER_IMAGE`）。

## 部署方式

| 模式 | 适用场景 |
|------|----------|
| **Docker Compose（推荐）** | NAS / 小团队一键私有化 |
| 飞牛 fnOS 应用包（`.fpk`） | 飞牛 NAS：应用中心一键安装，见 [deploy/fnos/README.md](deploy/fnos/README.md) |
| `./roundpend` 单二进制 | 本地开发、已有 PG |
| Kubernetes（规划中） | 企业集群 |

两个二进制（`roundpend` / `roundpen`）启动时会自动加载**当前工作目录**下的 `.env`；已存在的环境变量优先，文件缺失则静默跳过。systemd 可用 `EnvironmentFile=` 注入同样的变量。

### 一键私有化（最终用户）

控制台已嵌入二进制，宿主机**不必安装 Node / Go**：

```bash
cp .env.compose.example .env
echo "POSTGRES_PASSWORD=$(openssl rand -hex 24)" >> .env   # 必填：数据库口令（无默认弱口令）
docker compose up -d --build
# 浏览器打开 http://127.0.0.1:9527
# 首次启动：docker compose logs roundpend | head   # admin 密码与 API Key 各打印一次
```

Browser 槽位需要 Docker，首次使用会拉取 `ghcr.io/browserless/chrome:v2.56.7`（可用 `ROUNDPEN_BROWSER_IMAGE` 覆盖）；官方镜像已内置 Playwright driver，裸机 / 开发机首次跑引擎会自动安装（`make browser-driver`）。来源可切到局域网 / 商业云 browserless 或本机 Chrome，见 [docs/architecture/browser-env.md](docs/architecture/browser-env.md)。

### 安装面（Agent 镜像）

Agent 固定使用 Docker，镜像通过以下方式获取（无需本机持有 Dockerfile）：

| 方式 | 命令 |
|------|------|
| 注册表 pull（默认） | `docker pull ghcr.io/roundpenai/code-agent:0.1.0`（首次 Ensure 自动 pull） |
| 离线安装 | Release 附 OCI tar：`docker load -i code-agent.tar` |
| 开发者本地构建 | `make code-agent-image`（`roundpen-code-agent:local`，用 `ROUNDPEN_AGENT_IMAGE` 覆盖） |

单二进制部署只需 Docker + 可达的注册表；`ROUNDPEN_AGENT_IMAGE` 可指向私有镜像。

运行时也能改：**系统管理 → 通用 → Agent 镜像**手填镜像引用（留空跟随模板，模板仍决定 CPU / 内存 / 磁盘），
保存后对新建与重建的 Agent 容器生效（不用重启进程）。

详见 [deploy/compose/README.md](deploy/compose/README.md)。

### 本地开发（贡献者）

```bash
make setup          # 首次：.env、go mod、npm
make dev            # pg0 → roundpend :19001 + UI :19000
# 浏览器打开 http://127.0.0.1:19000/
```

**移动端控制台**（可选，Flutter，需要本机装 Flutter SDK；控制面本身不需要 Dart）：

```bash
make mobile-setup   # 首次：cd mobile && flutter pub get
make mobile-dev     # 模拟器/真机运行；先起 make dev，App 里填 http://<本机局域网IP>:19001
```

注意：**不要用 `flutter run -d chrome` 调试**——WebSocket 鉴权走原生握手 header，浏览器会丢弃它。
鼠标键盘调试用模拟器，真机验收用 dev build；iOS 首次访问局域网地址会弹「本地网络」权限，需要允许。

环境变量示例见 [.env.example](.env.example)。

## 技术选型

| 维度 | 选择 |
|------|------|
| 语言 | Go（跨平台单二进制） |
| API | 原生 REST（`/v1/...`） |
| Agent 后端 | Docker（官方 `code-agent` OCI 镜像） |
| Browser 后端 | Docker（browserless/chrome 容器；Playwright 引擎，来源可配） |
| 记忆 | PostgreSQL + `pgvector` |
| 文件 | 本地目录或 SSH 远端（`WorkspaceFS`） |

## 路线图

- **近期**：固定环境模型 + Browser 迁 Docker / Playwright（多来源：托管/局域网/云/本机）；删除多开沙箱 / E2B 兼容；删除 Kern 与 Agent-QEMU 默认路径
- **移动端**：移动端控制台（`mobile/`，Flutter）——对话核心优先，后续接 slash 命令、议题等（与 `Mobile` 槽位是两件事）
- **中期**：Agent 容器工作区增强
- **远期**：Mobile 槽位、集群扩展与企业能力

## 二进制与模块

- CLI：`roundpen`
- 守护进程：`roundpend`
- Go module：`github.com/RoundpenAI/roundpen`

## License

Apache License 2.0. 详见 [LICENSE](LICENSE)。
