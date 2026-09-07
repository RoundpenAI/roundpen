# QEMU Agent 固定环境

Agent 槽位是一台无桌面 QEMU VM（template `agent-claude`）。Guest 预装 git、OpenSSH 和 Claude Code。LLM 只走 Roundpen **llmgw**（virtual key）。运行时 **不依赖 Docker/Kata**。

## 运行时

| 路径 | 说明 |
|------|------|
| 系统盘 | Template `agent-claude` → `artifact_ref`（默认 `images/agent-qemu/out/agent.qcow2`）overlay |
| Workspace | 每用户一块 `data/sandboxes/user-{name}/workspace.qcow2`，virtio `/dev/vdb`，guest 挂 `/workspace`（属主 `roundpen`） |
| 引导 | 同目录 `vmlinuz` + `initrd.img` + `boot.json`；QEMU `-kernel`/`-initrd` |
| 后端 | `ROUNDPEN_BACKEND=qemu`（`internal/backend/qemu`） |
| LLM | CreateOpts.Env → `guest.env` → `-fw_cfg name=opt/roundpen/env` → guest `/etc/roundpen/env` |
| SSH | Guest `:22` ← user-mode `hostfwd` → `127.0.0.1:<host>`（账号 `roundpen` / `roundpen`） |

控制面通过 SSH `Exec` 跑 `git` / `sandbox_exec`，并注入 PAT。宿主机不直接读写 workspace 内部文件。

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

ACP stdio 走 QEMU SSH hostfwd。聊天里选 **Claude Code**（provider `claude`）会拉起 `agent-claude` 并 `AttachExec claude-agent-acp`。

Guest 从 Docker 导出时 `/etc/resolv.conf` 经常是空文件。后端在 SSH 起来后写入 QEMU slirp DNS `10.0.2.3`，并把 Claude Code 的内层 `sandbox.enabled` 关掉——隔离边界是这台 VM。

## 构建默认镜像

```bash
make agent-image
# 或 ./images/agent-qemu/build.sh
```

打包 qcow2 目前仍用 Docker 导出 rootfs（只在构建镜像时需要）。运行 Agent 不需要 Docker daemon。

配方：Ubuntu 24.04 + systemd + virtio 内核 + Node 22 + `@anthropic-ai/claude-code` + OpenSSH + git。详见 `images/agent-qemu/README.md`。

未构建镜像时 Create 会报明确错误（缺盘 / 占位盘 / 缺 kernel sidecar）。

## 配置

| 变量 | 含义 |
|------|------|
| `ROUNDPEN_BACKEND` | `qemu` |
| `ROUNDPEN_AGENT_IMAGE` | 默认 qcow2 路径 |
| `ROUNDPEN_DEFAULT_AGENT_TEMPLATE` | `agent-claude` |
| `ROUNDPEN_LLMGW_PUBLIC_URL` / `ROUNDPEN_PREVIEW_PUBLIC_URL` | 写入 guest 的控制面基址（loopback 会改成 `10.0.2.2`） |
| `ROUNDPEN_QEMU_BIN` | 默认 `qemu-system-x86_64` |

EnsureAgent 还会把 `userenv` 注入的 PublicURL + `vk-roundpen-internal` 传进 CreateOpts.Env。
