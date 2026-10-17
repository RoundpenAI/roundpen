# Roundpen on 飞牛 fnOS

应用中心展示名是 **AI助手**（`display_name`）；包名 / 数据目录仍是 `roundpen`，升级路径不变。

把 Roundpen 打成 fnOS 应用（`.fpk`）：宿主机跑 `roundpend`，包里带 [pg0](https://github.com/vectorize-io/pg0) 起本地 Postgres（含 pgvector）。Agent / Browser 用宿主 Docker（挂 `/var/run/docker.sock`）。安装与启动时会在后台预拉 `ROUNDPEN_BROWSER_IMAGE` / `ROUNDPEN_AGENT_IMAGE`（默认 browserless/chrome 与 code-agent），避免装完立刻 `browser_navigate` 时才开始下多 GB 镜像；进度见 `var/warm-images.log`。

## 安装

- 应用中心 → 手动安装 → 选 `.fpk`
- 或：`appcenter-cli install-fpk roundpen-<version>-fnos-x86.fpk`

首次默认账号 **admin / RoundpenAdmin1**（只写一次，改密后重启不会盖回去；默认密码仍有效时登录页会提示）。

桌面入口走飞牛统一网关（SSO）：飞牛用户映射为 `fnos-<uid>`，飞牛管理员映射为 Roundpen 管理员。也可用浏览器打开设备的 `:9527`，用 admin 密码进控制台（给 API / 移动端 / CLI 用）。

### 模型

装好即可对话，内置免费模型会自动启用，不用先配 Key。额度用尽（或频繁 429）再到 **系统管理 → LLM 网关** 填自己的 OpenAI / Anthropic 兼容上游。实现细节见 [docs/free-llm-token.md](../../docs/free-llm-token.md)。

### 数据目录（`var/`）

| 路径 | 内容 |
| --- | --- |
| `var/data` | `ROUNDPEN_DATA_ROOT`：密钥、沙箱、工作区 |
| `var/roundpend.env` | 运行环境（含 `DATABASE_URL`） |
| `var/roundpend.log` / `var/roundpend.pid` | 日志与 pid |
| `var/.pg0` | pg0 数据（默认 `127.0.0.1:15432`） |
| `var/pg0-install.log` | pg0 安装日志 |

卸载不删这些；清干净需手删 `var/`，必要时再 `pg0 stop/destroy --name roundpen`。

已有 Postgres 时，把 `var/roundpend.env` 里的 `DATABASE_URL` 改成外置库（需 `pgcrypto` / `vector`）并重启即可；非 localhost 的 DSN 不会再起本地 pg0。

### 常见配置

- **预览链接打不开**：把 `ROUNDPEN_PREVIEW_PUBLIC_URL` 改成 NAS 地址（**系统管理 → 通用**），默认是 `http://127.0.0.1:9527`。
- **Agent 镜像拉不动**：默认 `ghcr.io/roundpenai/code-agent:0.1.0`，在 **系统管理 → 通用** 换成镜像源或本地 tag。
- **Browser 镜像拉不动**：默认 `ghcr.io/browserless/chrome:v2.56.7`，可在 `var/roundpend.env` 设 `ROUNDPEN_BROWSER_IMAGE`（例如内网镜像或 offline `docker load` 后的本地 tag）。离线包太大，`.fpk` 不内嵌 OCI 层。
- **国内访问 ghcr.io 失败/很慢**：Docker 的 `registry-mirrors` **不会**加速 ghcr。Roundpen 会按序试 `ghcr.1ms.run` → `ghcr.nju.edu.cn` → DaoCloud；若官方能通但很慢，约 15s 后会与第一个镜像源并行竞速。可用 `ROUNDPEN_IMAGE_MIRRORS` / `ROUNDPEN_IMAGE_MIRROR_RACE_DELAY` 调整（`none` / `0` 分别关闭内置源与竞速）。
- **Docker 权限**：包用户 `roundpen` 加入 `docker` 组（`config/privilege` → `join-groups`）；组名不同就按设备改。

### 升级与 `secret.key` 权限

生命周期脚本以 root 跑，便于把 `var/` 收归包用户；真正的 `roundpend` 再用 `runuser` 降到 `roundpen`。

若从早期 **Docker 版 fpk** 升上来，以前容器可能把 `var/data/secret.key` 写成 `root:root` 且 `0600`，降权后的进程会报 `permission denied`。新包在 `upgrade_callback` / 启动时会 `chown` 整棵 `var/`。若仍失败，在 NAS 上执行：

```bash
chown -R roundpen:roundpen /vol1/@appdata/roundpen
chmod 0600 /vol1/@appdata/roundpen/data/secret.key
```

（路径以实际 `TRIM_PKGVAR` 为准。）

## 构建 `.fpk`

需要飞牛 `fnpack` 1.2.3、本机 `go` 与 `npm`：

```bash
curl -fsSLo /usr/local/bin/fnpack https://static2.fnnas.com/fnpack/fnpack-1.2.3-linux-amd64
echo "54b97fa7b70968c4d05c79840f5daeff508957d0bb2062fdb0376d00d9615c93  /usr/local/bin/fnpack" | sha256sum -c -
chmod +x /usr/local/bin/fnpack

make fpk                     # x86
PLATFORM=arm make fpk        # arm64
```

产物：`dist/fnos/roundpen-<version>-fnos-<platform>.fpk`（附 `.sha256`）。构建机从 GitHub Releases 拉 pin 过的 pg0，缓存在 `~/.cache/roundpen/pg0/`；**装到 NAS 上不再访问 GitHub**。打 `v*` tag 时 [.github/workflows/fnos-fpk.yml](../../.github/workflows/fnos-fpk.yml) 会出两种架构。

可用 `NPM_REGISTRY` / `GOPROXY` / `PG0_VERSION` / `PG0_CACHE` 覆盖源与缓存。

### 打包注意

- 统一网关：`app/ui/config` 的 `gatewayPrefix: "/app/roundpen"` + `gatewaySocket: "app.sock"`；`manifest.service_port`（默认 9527）留给直连端口，网关项里的 `protocol`/`port` 会被忽略。
- 通配预览域：设 `ROUNDPEN_PREVIEW_DOMAIN`（如 `rp.mk`），反代保留 Host、带 `X-Forwarded-Proto`，网段写进 `ROUNDPEN_TRUSTED_PROXIES`。
- 图标：改 `web/public/favicon.svg` 后跑 `node deploy/fnos/scripts/gen-icons.mjs`。
- `os_min_version`：真机验证后再写进 `manifest`。
