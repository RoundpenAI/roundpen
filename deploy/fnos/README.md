# Roundpen on 飞牛 fnOS

把 Roundpen 打包成 fnOS 应用（`.fpk`）：**宿主机**运行 `roundpend` 二进制，
包内捆绑 [pg0](https://github.com/vectorize-io/pg0) 拉起本地 Postgres（含
pgvector）。应用中心通过 `cmd/main` 启停进程。Agent / Browser 沙箱仍使用宿主
Docker（挂 `/var/run/docker.sock`），首次使用相关槽位时再拉镜像。

## 构建

需要飞牛官方的 `fnpack`（V1.2.3）、本机 `go` 与 `npm`（构建时嵌入 Web UI）：

```bash
curl -fsSLo /usr/local/bin/fnpack https://static2.fnnas.com/fnpack/fnpack-1.2.3-linux-amd64
echo "54b97fa7b70968c4d05c79840f5daeff508957d0bb2062fdb0376d00d9615c93  /usr/local/bin/fnpack" | sha256sum -c -
chmod +x /usr/local/bin/fnpack
```

```bash
make fpk                     # x86 包（roundpend + pg0）
PLATFORM=arm make fpk        # arm64
```

产物在 `dist/fnos/roundpen-<version>-fnos-<platform>.fpk`，同名 `.sha256` 是
校验值。构建机从 GitHub Releases 拉取已 pin 的 pg0 二进制打进包，并按版本
缓存在 `~/.cache/roundpen/pg0/`（同一版本再次 `make fpk` 直接复用）；**安装机
不再访问 GitHub**。

`.github/workflows/fnos-fpk.yml` 在打 `v*` tag 时按两种架构出包并挂到 release。

本地构建可用 `NPM_REGISTRY` / `GOPROXY` / `PG0_VERSION` / `PG0_CACHE` 覆盖默认源、pg0 版本与缓存目录。

## 安装

- 图形界面：应用中心 → 手动安装 → 选择 `.fpk`；
- SSH：`appcenter-cli install-fpk roundpen-0.1.0-fnos-x86.fpk`。

`install_callback` 会用包内 pg0 创建本地实例，并把 `DATABASE_URL` 写入
`var/roundpend.env`。首次安装默认账号 **admin / RoundpenAdmin1**（只设一次，
改密后重启不会被重置）；登录页在默认密码仍有效时也会提示。

若已有 Postgres，可在 `var/roundpend.env` 里把 `DATABASE_URL` 改成外置库
（需含 `pgcrypto` / `vector`），重启应用即可；本地 pg0 不会对非 localhost
的 DSN 再起实例。

## 运行与数据

| 位置 | 内容 |
| --- | --- |
| `var/data` | `ROUNDPEN_DATA_ROOT`：主密钥 `secret.key`、沙箱状态、日志工作区 |
| `var/roundpend.env` | 运行时环境（含 `DATABASE_URL`） |
| `var/roundpend.log` / `var/roundpend.pid` | 进程日志与 pid |
| `var/.pg0` | pg0 实例数据（默认监听 `127.0.0.1:15432`，避开 NAS 常见的 5432） |
| `var/pg0-install.log` | pg0 自举详细日志（安装失败时可看） |

**roundpend 用的是宿主 Docker**（需 `docker` 组，见 `config/privilege`），
Agent / Browser 容器是宿主上的兄弟容器。传给 Docker 的路径必须是宿主真实
路径（`ROUNDPEN_DATA_ROOT` 下的 sandbox 目录）。

卸载不会删除上面数据，重新安装即可接着用；要彻底清空需手工删掉 `var/` 以及
（如需要）`pg0 stop/destroy --name roundpen`。

Agent 镜像默认 `ghcr.io/roundpenai/code-agent:0.1.0`，网络不通时可在应用设置里
改成镜像源或本地 tag。

## 模型：FREE 试用与 BYOK

1. **免费试用（零配置）**：首次启动若尚未绑定任何默认模型，Roundpen 会自动启用
   内置云端入口 `https://api.ttz.ac.cn`，生成
   `$ROUNDPEN_DATA_ROOT/install-identity/`（每安装一份 Ed25519 身份），向云端
   签发短时 `rp1…` token，并按 `install_id` 在云端限频。飞牛用户登录后即可直接
   对话，无需点「一键启用」。细节与验签测试向量见
   [docs/free-llm-token.md](../../docs/free-llm-token.md)。
   （运维可选覆盖：`ROUNDPEN_FREE_LLM_BASE_URL` / `MODEL` / `TOKEN_TTL`。）

2. **自己的 API Key（BYOK）**：达到 FREE 限额（HTTP 429）后再配置——
   **系统管理 → LLM 网关**，添加 OpenAI / Anthropic 兼容上游并绑定默认槽位。
   生产环境建议用 BYOK。

## 打包时需要留意

- **预览地址**：`ROUNDPEN_PREVIEW_PUBLIC_URL` 默认 `http://127.0.0.1:9527`，
  在 NAS 上应改成设备地址（系统管理 → 通用），否则预览链接只能本机打开。
  有通配域名（DNS + 证书覆盖 `*.rp.mk`）时可设 `ROUNDPEN_PREVIEW_DOMAIN=rp.mk`，
  预览改为每端口一个子域 `{沙箱}-{端口}.rp.mk`；反代需保留 Host、带上
  `X-Forwarded-Proto`，并把反代网段写进 `ROUNDPEN_TRUSTED_PROXIES`。
- **入口与端口**：桌面入口走**统一网关**（`app/ui/config` 里的
  `gatewayPrefix: "/app/roundpen"` + `gatewaySocket: "app.sock"`），飞牛校验 NAS
  登录态后经 Unix Socket 转发，并注入 `X-Trim-Userid` / `X-Trim-Isadmin` /
  `X-Trim-Username`；不同飞牛用户会映射为不同本地账号（`fnos-<uid>`），飞牛管理员
  映射为管理员。
  `manifest.service_port`（默认 9527）+ `checkport=true` **保留**，作为独立端口
  供 **API / 移动端 / CLI / 外部 LLM 客户端** 使用，也可用浏览器打开 `:9527` 以
  `admin`+密码登录 Web 控制台。网关条目下 `protocol`/`port` 被忽略。
- **图标**：由 `web/public/favicon.svg` 渲染，改 logo 后跑
  `node deploy/fnos/scripts/gen-icons.mjs` 重新生成。
- **`os_min_version`**：真机测过之后再往 `manifest` 里补兼容范围。
- **docker 权限**：`config/privilege` 里 `join-groups: ["docker"]` 让 roundpend
  能访问宿主 Docker socket（创建 Agent / Browser 容器）。若设备上该用户组名
  不同，按实际调整。
