# QEMU Agent 固定环境

Agent 槽位可以是 Docker/Kern（默认 `code-agent`，ACP stdio），也可以是一台独立的无桌面 QEMU VM（template `agent-claude`）。Guest 预装 Claude Code，LLM 只走 Roundpen **llmgw**（virtual key），不写上游 Anthropic/OpenAI key。

## 运行时

| 路径 | 说明 |
|------|------|
| 磁盘 | Template `agent-claude` → `artifact_ref`（默认 `images/agent-qemu/out/agent.qcow2`） |
| 引导 | 同目录 `vmlinuz` + `initrd.img` + `boot.json`；QEMU `-kernel`/`-initrd` |
| 后端 | `internal/backend/qemu`；`multi` 按镜像后缀 `.qcow2` 路由（任意 slot） |
| LLM | CreateOpts.Env → `guest.env` → `-fw_cfg name=opt/roundpen/env` → guest `/etc/roundpen/env` |
| SSH | Guest `:22` ← user-mode `hostfwd` → `127.0.0.1:<host>`（账号 `roundpen` / `roundpen`） |

QEMU slirp 里宿主机是 `10.0.2.2`。后端会把 `127.0.0.1` / `localhost` 的控制面 URL 改写成 `10.0.2.2`，再注入：

```
ANTHROPIC_BASE_URL=http://10.0.2.2:<port>/llmgw/anthropic
OPENAI_BASE_URL=http://10.0.2.2:<port>/llmgw/openai
ANTHROPIC_API_KEY=vk-roundpen-internal
ANTHROPIC_AUTH_TOKEN=vk-roundpen-internal
OPENAI_API_KEY=vk-roundpen-internal
```

Guest `roundpen-apply-env.service` 把同一组变量写进 `/etc/environment`、`/etc/profile.d/roundpen-llmgw.sh`、`/etc/claude-code/managed-settings.json` 和 `~/.claude/settings.json`。

**不要**在镜像里烘焙上游 API key。

`AttachExec`（ACP stdio）本期仍不支持；聊天 Agent 请继续用 `code-agent`（docker/kern）。这台 VM 给 EnsureAgent / SSH 交互式 `claude` 用。

## 构建默认镜像

```bash
make agent-image
# 或 ./images/agent-qemu/build.sh
```

需要 Docker。打包 qcow2 优先用宿主机 `virt-make-fs` / `guestfish`，否则走 privileged Docker loop+mkfs。

配方：Ubuntu 24.04 + systemd `multi-user.target` + virtio 内核 + Node 22 + `@anthropic-ai/claude-code` + OpenSSH。详见 `images/agent-qemu/README.md`。

未构建镜像时 Create 会报明确错误（缺盘 / 占位盘 / 缺 kernel sidecar）。

## 配置

| 变量 | 含义 |
|------|------|
| `ROUNDPEN_AGENT_IMAGE` | 默认 qcow2 路径（种子模板 artifact） |
| `ROUNDPEN_DEFAULT_AGENT_TEMPLATE` | 默认仍是 `code-agent`；设为 `agent-claude` 启用这台 VM |
| `ROUNDPEN_LLMGW_PUBLIC_URL` / `ROUNDPEN_PREVIEW_PUBLIC_URL` | 写入 guest 的控制面基址（loopback 会改成 `10.0.2.2`） |
| `ROUNDPEN_QEMU_BIN` | 默认 `qemu-system-x86_64` |

EnsureAgent 还会把 `userenv` 注入的 PublicURL + `vk-roundpen-internal` 传进 CreateOpts.Env。
