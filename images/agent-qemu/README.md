# Agent QEMU image (headless + Claude Code)

Default Roundpen **Claude Code** agent disk. Guest runs systemd
`multi-user.target` (no XFCE), OpenSSH, Node 22, and
`@anthropic-ai/claude-code`. LLM traffic is forced through Roundpen
**llmgw** via QEMU fw_cfg env — do **not** bake upstream API keys.

The Docker rootfs is **not** BIOS-bootable. `build.sh` extracts `vmlinuz` and
`initrd.img`; the QEMU backend boots with `-kernel` / `-initrd`.

## Output

```
images/agent-qemu/out/agent.qcow2
images/agent-qemu/out/vmlinuz
images/agent-qemu/out/initrd.img
images/agent-qemu/out/boot.json
```

Seeded template `agent-claude` points at the qcow2 (override with
`ROUNDPEN_AGENT_IMAGE`).

## Build

Needs Docker (and either `virt-make-fs`, `guestfish`, or privileged Docker to
pack the qcow2). Apt uses the USTC Ubuntu mirror (`mirrors.ustc.edu.cn`, HTTP
because the base image has no CA bundle). Node 22 and npm use npmmirror:
USTC's node dist / npmreg return 403 from this Docker network.

```bash
./images/agent-qemu/build.sh
# or: make agent-image
```

## Guest expectations

| Service | Port / path |
|---------|-------------|
| SSH | `:22` (`roundpen` / `roundpen`; hostfwd by qemu backend) |
| Init | systemd `multi-user.target` → `roundpen-apply-env` → ssh |
| Claude Code | `claude` on PATH; settings in `/etc/claude-code/managed-settings.json` |
| LLM | `ANTHROPIC_*` / `OPENAI_*` → `{slirp-host}/llmgw/{anthropic,openai}` |

Boot-time `roundpen-apply-env` reads QEMU fw_cfg `opt/roundpen/env` (KEY=VALUE)
and writes `/etc/roundpen/env`, `/etc/environment`, profile.d, and Claude
managed settings. Loopback control-plane URLs are rewritten to `10.0.2.2` on
the host before fw_cfg is attached.

ACP stdio uses QEMU SSH hostfwd. The guest also has
`claude-agent-acp` (`@agentclientprotocol/claude-agent-acp`). Provider `claude`
runs that binary; set `ACP_PERMISSION_MODE=bypassPermissions` in the injected
env so the sandbox can test without interactive permission prompts.

## Workspace (9p)

Each login has a persistent host directory `{dataRoot}/sandboxes/user-{name}/workspace/`.
QEMU exports it with `-virtfs` (tag `workspace`); the guest mounts it at `/workspace`.

`guest/fstab` and `guest/modules` bake that mount into the next `make agent-image`.
Existing `agent.qcow2` disks do not need a rebuild: after SSH is up the backend
`modprobe`s `9pnet_virtio`/`9p` and mounts the same tag.

The Docker-exported rootfs often ships an empty `/etc/resolv.conf`. After SSH the
backend writes QEMU slirp DNS (`10.0.2.3`) and sets Claude Code
`sandbox.enabled: false` plus `permissions.defaultMode: bypassPermissions`.
The VM is the isolation boundary; Claude's inner bwrap sandbox must not block
`apt` / downloads.
