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
pack the qcow2):

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

ACP stdio (`AttachExec`) is not available in this VM; keep chat agents on
`code-agent` (docker/kern) unless you SSH in and run `claude` interactively.
