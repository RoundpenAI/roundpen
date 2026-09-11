# ACP Gateway & Agent Web UI

助手优先的控制面：浏览器经 WebSocket 与助手对话，控制面作为 ACP Client。**System Agent** 在控制面进程内运行（无沙箱）；coding / stdio Agent 仍在沙箱内拉起。每个助手（`/v1/assistants`）绑定一条主会话（`agent_sessions.assistant_id`）。

相关：[environment-services.md](./environment-services.md)、[project-layout.md](./project-layout.md)、[assistant-first UI](../superpowers/specs/2026-09-12-assistant-first-ui-design.md)。

## 目标

- 顶层 `/a` 管理助手；对话挂在助手下。
- 控制面 = ACP Client + 网关；禁止宿主机裸跑用户 coding Agent。
- System Agent：以当前用户权限调用 Roundpen API + Browser；经 llmgw 做真实 LLM tool-calling。

## 概念

| 概念 | 含义 |
|------|------|
| Assistant | 用户可见主体：简介、身份、能力、网络与目录授权 |
| Provider | Agent 入口：`sysadmin`（进程内）、`claude`（QEMU `claude-agent-acp`）、或 `stdio`（自定义 AttachExec）；助手 ensure-session 默认 `claude` |
| Agent Session | 助手下的对话会话；stdio 绑定 1 sandbox，sysadmin 可不绑沙箱 |
| Provisioner | `internal/agentenv`：仅 NeedsSandbox 的 provider 创建沙箱并注入环境 |
| AttachExec | 非 TTY 长驻 stdio 附着（coding ACP） |

## 架构

```
Browser (/a)
    │  WS + REST
Control plane
    ├── assistants (PG)
    ├── agentsession (PG, assistant_id)
    ├── agentenv.Provisioner → sandbox.Create(+env)   [stdio only]
    └── acp.Manager
            ├── sysadmin → sysagent (in-process pipes)
            │                 ├── llmgw /openai chat + tools
            │                 └── tools: Roundpen HTTP (user API key) + browser.Hub
            └── stdio → AttachExec in sandbox
```

## System Agent 工具（第一批）

| Tool | 说明 |
|------|------|
| `roundpen_list_sandboxes` / `get` / `create` / `delete` | 沙箱 CRUD（写操作首次询问；可选本会话记住） |
| `roundpen_list_templates` | 模板列表 |
| `roundpen_list_agent_sessions` | 当前用户会话 |
| `roundpen_get_settings` | 管理设置（admin） |
| `roundpen_ensure_agent` / `sandbox_exec` | Cloud Agent 槽位：ensure + 在 `/workspace` 里跑 shell（git clone / 构建） |
| `browser_navigate` / `snapshot` / `click` / `type` / `press` / `screenshot` / `set_viewport` / `evaluate` | 进程内 Browser Hub（对齐 MCP） |

`sandbox_exec` 绑定 **Agent 槽位**（QEMU 模板 `agent-claude`）。`list_environments` 里 agent `status=absent` 只表示还没启动，应调用 `roundpen_ensure_agent` / `sandbox_exec`。

Git 鉴权：**用户在 Settings → Git 填写 token** → 控制面存 PostgreSQL → `EnsureAgent` 经 SSH 写入 guest `/workspace/.roundpen/git`（不写宿主机目录）。**禁止**把宿主机 `~/.ssh` 拷进镜像或沙箱。见 [git-credentials.md](../git-credentials.md)。

LLM：loopback `POST {HTTP}/llmgw/openai/v1/chat/completions`，鉴权 `vk-roundpen-internal`；model 回落 settings Default Model。

写操作权限（`Mutating`）：只读不询问。写操作弹出选项：

- Allow once
- Allow this tool (session) — 本会话内同工具不再问
- Allow all tools (session) — 本会话内全部写工具不再问
- Reject / Reject this tool (session)

后续 `computer_use_*` 等 `*-use` 工具注册到同一 `sysagent/tools` registry。

## 沙箱注入表（stdio / coding）

写入 `CreateRequest.Env`，并可选落盘 `/workspace/.roundpen/env`：

| 变量 | 用途 |
|------|------|
| `ROUNDPEN_URL` | 控制面可达基址 |
| `ROUNDPEN_API_KEY` | 用户 API Key（MVP） |
| `ROUNDPEN_SANDBOX_ID` / `ROUNDPEN_AGENT_ID` / `ROUNDPEN_SESSION_ID` | 作用域 |
| `OPENAI_BASE_URL` / `ANTHROPIC_BASE_URL` | `/llmgw/openai`、`/llmgw/anthropic` |
| `OPENAI_API_KEY` / `ANTHROPIC_API_KEY` | virtual key（禁止上游真实密钥） |
| `ROUNDPEN_BROWSER_MCP_URL` | `/v1/sandboxes/{id}/browser/mcp` |
| `ROUNDPEN_MEMORY_URL` | `/v1`（mem0 风格路径） |

## API

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/v1/agents` | Provider 列表 |
| GET/POST | `/v1/agent-sessions` | 列出会话 / 创建（sysadmin 跳过 Provisioner） |
| GET/DELETE | `/v1/agent-sessions/{id}` | 详情 / 结束 |
| GET | `/v1/agent-sessions/{id}/ws` | prompt / cancel / permission ↔ session updates |
| GET | `/v1/agent-sessions/{id}/browser` | System Agent 浏览器状态（Hub key `sysagent-<id>`） |
| GET | `/v1/agent-sessions/{id}/browser/screenshot` | viewport PNG（聊天右侧轮询） |
| POST | `/v1/agent-sessions/{id}/browser/takeover` | `{enabled}` 人工接管；开启后 agent 写工具返回 `browser under human takeover` |
| POST | `/v1/agent-sessions/{id}/browser/input` | takeover 下 `click\|move\|type\|key`（CDP 像素输入） |

鉴权与控制台一致（cookie session 或 API key）。

### Chat 浏览器面板

`/chats/:id` 宽屏左右分栏：左侧对话，右侧截图预览。出现 `browser_*` 工具或用户点 Show browser 时打开。Takeover 后可在预览上点击/打字处理验证码，Resume 后 Agent 继续控制。

这是 **headless CDP 截图流**，不是沙箱 noVNC。沙箱浏览器仍走 Workbench `preview-link` + :6080。

## System Agent 上下文（对齐 Claude Code）

每次 `Prompt` 从 `agent_messages` 回放，不依赖进程内记忆。进程重启后只要库还在，上下文可恢复。

| 落库 role | 是否进 prompt | 映射 |
|-----------|---------------|------|
| `user` | 是 | `user` |
| `assistant` | 是（跳过空 / `(no response)`） | `assistant` 文本 |
| `tool` | 是 | 连续工具合成一条 `assistant.tool_calls` + 多条 `tool` |
| `event`（`type=error`） | 是 | `user`: `Previous turn error: …` |
| `thought` | 否 | 与 Claude Code 一样，旧 thinking 不回放 |
| `permission` | 否 | 只给 UI / 审计 |
| 其它 `event`（plan 等） | 否 | — |

当前用户这句话如果已经落库，不再重复追加。超过约 80k 字符时丢掉最老的完整 turn，并插入一条 omitted 提示；不做 LLM 摘要（下一步）。

## Claude Code 上下文

不能走 System Agent 那条「改 LLM messages 数组」的路径：`claude-agent-acp` 自己组请求。`session/load` 也只认 guest 磁盘上 Claude 自己的 session id，不是我们的 PG。

同一套落库投影可以在 **stdio runtime 刚 `NewSession` 后的第一轮 Prompt** 里种进去：前面一块 restore 文本（user / assistant / tool 结果），后面仍是当前用户这句话。活着的 ACP session 后续轮次只发新字，避免和 Claude 内存历史叠两份。不要把历史再 `Prompt` 一遍，否则会重跑工具。

## 包布局

```
internal/acp/{client,manager,providers,sysagent,sysagent/tools}
internal/agentsession/
internal/agentenv/
internal/browser  # Hub + agent session browser API
```

## 非目标

- 宿主机直接跑 CLI coding Agent（Zacp 模式）
- 用 ACP 驱动会话式 Agent UI
- 完整 policy / toolgw 产品化（registry 仅 System Agent 内）
- computer-use / 宿主机键鼠（预留扩展点；takeover 仅 CDP Input）
- 聊天内嵌 noVNC / WebRTC 视频流
- 替换 Sandboxes / Templates 运维页
