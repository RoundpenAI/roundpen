# Roundpen

在自己的机器上跑 AI 助手：隔离的 Agent / Browser 环境、对话记忆、LLM 网关，带 Web 控制台。Go 单二进制，适合 NAS 和 Linux 服务器。

## 跑起来

**Docker Compose（推荐）** — 不用装 Node / Go，控制台已打进镜像：

```bash
cp .env.compose.example .env
echo "POSTGRES_PASSWORD=$(openssl rand -hex 24)" >> .env
docker compose up -d --build
# 打开 http://127.0.0.1:9527
# 首次 admin 密码：docker compose logs roundpend | head
```

**飞牛 fnOS** — 装 `.fpk`，见 [deploy/fnos/README.md](deploy/fnos/README.md)。飞牛登录后可直接用内置免费模型，额度用尽再去 **系统管理 → LLM 网关** 填自己的 Key。

**本机开发**：

```bash
make setup && make dev
# API :19001，Vite 控制台 http://127.0.0.1:19000/
```

环境变量见 [.env.example](.env.example) / [.env.compose.example](.env.compose.example)。Compose 细节见 [deploy/compose/README.md](deploy/compose/README.md)。

## 装好后你会用到的

| 你想做的事 | 去哪 |
| --- | --- |
| 对话、建助手 | 控制台 `/a` |
| 免费模型 / 自己的 API Key | **系统管理 → LLM 网关**（未配时启动会自动启用内置 FREE） |
| 换 Agent 镜像 | **系统管理 → 通用**（或 `ROUNDPEN_AGENT_IMAGE`） |
| 个人 Git / 关联账号 | **设置** |
| 事项 / 常驻任务 | `/issues`、助手详情 |

Agent、Browser 都跑在 Docker 里。首次用会拉：

- Agent：`ghcr.io/roundpenai/code-agent:0.1.0`（可用 `ROUNDPEN_AGENT_IMAGE` 或 `docker load`）
- Browser：`ghcr.io/browserless/chrome:v2.56.7`（可用 `ROUNDPEN_BROWSER_IMAGE`）

离线或内网把镜像准备好即可，不必本机留 Dockerfile。

## 二进制

| 命令 | 用途 |
| --- | --- |
| `roundpend` | 控制面（可嵌入 Web UI） |
| `roundpen` | CLI |

两者都会读**当前工作目录**的 `.env`；已有环境变量优先。systemd 可用 `EnvironmentFile=`。

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
| [mobile/](mobile/) | 移动端控制台（Flutter；和预留的 Mobile 槽位不是一回事） |

## License

Apache License 2.0，见 [LICENSE](LICENSE)。
