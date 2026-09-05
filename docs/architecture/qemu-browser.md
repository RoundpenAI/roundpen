# QEMU Browser 固定环境

Browser 槽位是一台独立 QEMU VM（与 Cloud Agent 不同机）。

## 运行时

| 路径 | 说明 |
|------|------|
| 磁盘 | Template `browser-desktop` → `artifact_ref`（默认 `images/browser-qemu/out/browser.qcow2`） |
| 引导 | 同目录 `vmlinuz` + `initrd.img` + `boot.json`；QEMU `-kernel`/`-initrd`（不是 GRUB/BIOS 盘） |
| 后端 | `internal/backend/qemu`；经 `internal/backend/multi` 按 `slot=browser` 路由 |
| CDP | Chrome `127.0.0.1:9333` → guest socat `:9222` ← user-mode `hostfwd` → Hub Dial |
| 桌面 | QEMU `-vga virtio` + `-vnc unix:{data_root}/qemu/{id}/vnc.sock` → `GET /v1/me/environments/browser/desktop` → WS 代理 |

**不要**在 Guest 内安装 noVNC / websockify / x11vnc。

Guest 启动链：内核 → systemd `graphical.target` → LightDM 自动登录 `roundpen` → XFCE → Chrome CDP。

## 构建默认镜像

```bash
make browser-image
# 或 ./images/browser-qemu/build.sh
```

需要 Docker。打包 qcow2 优先用宿主机 `virt-make-fs` / `guestfish`，否则走 privileged Docker loop+mkfs。

配方：Ubuntu 24.04 + systemd + virtio 内核 + XFCE + LightDM + Chrome（CDP `0.0.0.0:9222`）。详见 `images/browser-qemu/README.md`。

未构建镜像时 `EnsureBrowser` 会报明确错误（缺盘 / 占位盘 / 缺 kernel sidecar），而不会拉起一台黑屏 VM。

## 配置

| 变量 | 含义 |
|------|------|
| `ROUNDPEN_QEMU_ENABLED` | 默认 `true`；找不到 qemu 二进制时自动降级告警 |
| `ROUNDPEN_QEMU_BIN` | 默认 `qemu-system-x86_64` |
| `ROUNDPEN_BROWSER_IMAGE` | 默认 qcow2 路径（种子模板 artifact） |
| `ROUNDPEN_DEFAULT_BROWSER_TEMPLATE` | 默认 `browser-desktop` |
| `ROUNDPEN_CDP_PROVIDER` | `auto` 时优先 Dial Browser env（不再默认 host Chrome） |

宿主机还需要 `qemu-img`。有 `/dev/kvm` 时用 KVM，否则 TCG（桌面会很慢）。

## API

- `GET /v1/me/environments` — 槽位状态
- `POST /v1/me/environments/browser/ensure` — 启动/恢复 Browser VM
- `GET /v1/me/environments/browser/desktop` — 签发桌面 WebSocket URL
- `GET /v1/me/environments/browser/desktop/ws?token=` — RFB↔WS 代理

## 二期

独立 Agent QEMU 见 `docs/architecture/qemu-agent.md`（template `agent-claude`）。qemu Backend 保持通用：qcow2 + kernel sidecar，不要做成 Browser 专用死接口。
