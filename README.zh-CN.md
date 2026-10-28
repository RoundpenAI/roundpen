# Roundpen

[English](README.md)

**跑在自己机器上的 AI 工作台。** 助手能对话、在 Docker 沙箱里写代码、浏览网页、跟进事项，还能按日程自己醒来干活。Go 单二进制；笔记本、Linux 服务器、家里的 NAS（含 [飞牛 fnOS](deploy/fnos/README.md)）都能装。

数据和密钥留在你装的那台机器上。需要时接自己的 LLM Key；装好就有内置免费模型，不用先填 Key 也能开聊。

## 现在能做什么

- **多助手** — 各自设定、会话、技能与能力授权
- **隔离的 Agent + Browser** — 代码与 shell 在 Docker Agent 沙箱里跑；浏览用托管浏览器（或自备 CDP）
- **项目** — 绑定仓库，Coordinator 协调多个 Worker 做较大的活
- **事项与常驻任务** — 一次性议题，加上不用盯着对话框也会到点执行的常驻任务
- **LLM 网关** — 安装即有 FREE；需要更强模型时接 OpenAI / Anthropic 兼容上游
- **Web 控制台 + 手机 App** — 日常用浏览器；Flutter 客户端连同一控制面
- **连接器** — Git / OAuth、MCP、IM 频道、网页搜索等，在设置里接

## 跑起来

### Docker Compose（推荐）

不用本机装 Node / Go，控制台已打进镜像：

```bash
cp .env.compose.example .env
echo "POSTGRES_PASSWORD=$(openssl rand -hex 24)" >> .env
docker compose up -d --build
# 打开 http://127.0.0.1:9527
# 首次 admin 密码：docker compose logs roundpend | head
```

细节见 [deploy/compose/README.md](deploy/compose/README.md)。

### 飞牛 fnOS

装 `.fpk`，见 [deploy/fnos/README.md](deploy/fnos/README.md)。飞牛登录后可直接用内置免费模型；额度用尽再到 **系统管理 → LLM 网关** 填自己的 Key。

### 本机开发

```bash
make setup && make dev
# API :19001 · Vite 控制台 http://127.0.0.1:19000/
```

环境变量见 [.env.example](.env.example) / [.env.compose.example](.env.compose.example)。

## 装好后你会用到的

| 你想做的事 | 去哪 |
| --- | --- |
| 对话、建助手 | 控制台 `/a` |
| 免费模型 / 自己的 API Key | **系统管理 → LLM 网关** |
| 换 Agent 镜像 | **系统管理 → 通用**（或 `ROUNDPEN_AGENT_IMAGE`） |
| 个人 Git / 关联账号 | **设置** |
| 事项 / 常驻任务 / 项目 | `/issues`、助手详情、`/projects` |

Agent、Browser 都跑在 Docker 里。首次用会拉：

- Agent：`ghcr.io/roundpenai/code-agent:0.1.0`（可用 `ROUNDPEN_AGENT_IMAGE` 或 `docker load`）
- Browser：`ghcr.io/browserless/chrome:v2.56.7`（可用 `ROUNDPEN_BROWSER_IMAGE`；安装/启动时后台预拉）

`ghcr.io` 拉不动时会自动试国内代理镜像（见 `ROUNDPEN_IMAGE_MIRRORS`），成功后打回原 tag。离线或内网也可 `docker load`，不必本机留 Dockerfile。

## 二进制

| 命令 | 用途 |
| --- | --- |
| `roundpend` | 控制面（可嵌入 Web UI） |
| `roundpen` | CLI |

两者都会读**当前工作目录**的 `.env`；已有环境变量优先。systemd 可用 `EnvironmentFile=`。

## 我们往哪走

Roundpen 正从「本机上和一个 Agent 聊天」演进成能扛事的**个人 / 小团队 AI 工作台**：

- 可分段续跑的长时间任务，不必单次 Prompt 挂十几个小时
- 更强的项目协调（多 Agent Worker、项目级共享上下文）
- 更深的家用 / NAS 集成（目录授权、本机服务）
- 可选的桌面 / 手机虚拟机槽位，给需要完整 GUI 的 Agent 用

北极星不变：**能干的助手，跑在你控制的基础设施上**，而不是按分钟租的云。

## 更多文档

| 文档 | 内容 |
| --- | --- |
| [docs/architecture/project-layout.md](docs/architecture/project-layout.md) | 仓库结构 |
| [docs/architecture/browser-env.md](docs/architecture/browser-env.md) | Browser 来源（托管 / 局域网 / 云 / 本机 Chrome） |
| [docs/workspace-layout.md](docs/workspace-layout.md) | 工作区目录 |
| [docs/security.md](docs/security.md) | 隔离与策略 |
| [docs/auth.md](docs/auth.md) | 登录与 OAuth |
| [docs/free-llm-token.md](docs/free-llm-token.md) | 内置 FREE 模型的安装身份与限频 |
| [docs/connectors.md](docs/connectors.md) / [docs/mcp-connectors.md](docs/mcp-connectors.md) | Google / MCP 连接器 |
| [docs/skills.md](docs/skills.md) | 可复用技能工作流 |
| [mobile/](mobile/) | 手机上的控制台 App（Flutter） |

## License

Apache License 2.0，见 [LICENSE](LICENSE)。
