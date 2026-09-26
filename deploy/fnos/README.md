# Roundpen on 飞牛 fnOS

把 Roundpen 打包成 fnOS 应用（`.fpk`）：`roundpend` + PostgreSQL/pgvector 走
Docker Compose，应用中心随应用一起启动和停止它（`config/resource` 里的
`docker-project`），`cmd/main` 只负责状态查询。

## 构建

只需要飞牛官方的 `fnpack`（V1.2.3）；镜像由 CI 发布，本地打包不碰 Docker：

```bash
curl -fsSLo /usr/local/bin/fnpack https://static2.fnnas.com/fnpack/fnpack-1.2.3-linux-amd64
echo "54b97fa7b70968c4d05c79840f5daeff508957d0bb2062fdb0376d00d9615c93  /usr/local/bin/fnpack" | sha256sum -c -
chmod +x /usr/local/bin/fnpack
```

```bash
make fpk                     # x86 包（安装时由 NAS 拉取 ghcr 镜像）
PLATFORM=arm make fpk        # arm64 包
WITH_IMAGE=1 make fpk        # 离线包：本地构建镜像并塞进包内（需要 Docker）
```

产物在 `dist/fnos/roundpen-<version>-fnos-<platform>.fpk`，同名 `.sha256` 是校验值。
`.github/workflows/fnos-fpk.yml` 在打 `v*` tag 时先推送
`ghcr.io/<owner>/roundpend:<version>`（amd64 + arm64 多架构），再按两种架构出包并挂到 release。
`WITH_IMAGE=1` 时构建容器看不到宿主的 npm / Go 配置，脚本会自动把宿主的
`npm config get registry` / `go env GOPROXY` 作为 build-arg 传进去（可显式覆盖
`NPM_REGISTRY` / `GOPROXY`），并默认用 `--network=host` 构建 —— 默认 bridge 没有
IPv6，而镜像源常解析到 IPv6，走 bridge 时每个包都要等 IPv6 超时才回落（实测
100~300 秒/包）。需要隔离网络时用 `BUILD_NETWORK=default make fpk`。

## 安装

- 图形界面：应用中心 → 手动安装 → 选择 `.fpk`；
- SSH：`appcenter-cli install-fpk roundpen-0.1.0-fnos-x86.fpk`。

首次安装后应用中心即启动；首启日志里打印一次管理员密码与 API key。
PostgreSQL 不设密码（`POSTGRES_HOST_AUTH_METHOD=trust`）：数据库端口不对外发布、
只存在于本 compose 项目的私有网络里，而应用中心在安装阶段就会校验 compose，等
不及生命周期脚本生成口令文件。要改成口令认证，就在包里预置一个 `env_file`
（每次出包唯一）或确认设备上的安装顺序允许脚本先写文件。

## 运行与数据

| 位置 | 内容 |
| --- | --- |
| `var/data` | `ROUNDPEN_DATA_ROOT`：主密钥 `secret.key`、沙箱状态、日志 |
| Docker 卷 `roundpen_roundpen-pgdata` | PostgreSQL 数据目录 |

**容器里的 roundpend 用的是宿主 Docker**（挂 `/var/run/docker.sock`），Agent /
Browser 容器是宿主上的兄弟容器，不是嵌套在 roundpend 里。因此传给 Docker 的路径
必须是宿主真实路径：`/data` 用的是 `${TRIM_PKGVAR}/data` 这种 **bind 挂载**
（宿主上真实存在），不能用 Docker 命名卷 —— 用命名卷时宿主侧是
`/var/lib/docker/volumes/.../_data`，而 roundpend 传给 daemon 的是
`/data/sandboxes/<id>`，会挂不上或挂到空目录。

卸载不会删除上面两处数据，重新安装即可接着用；要彻底清空需要手工删掉
`var/data` 和 `docker volume rm roundpen_roundpen-pgdata`。

Agent 镜像默认 `ghcr.io/roundpenai/code-agent:0.1.0`，网络不通时可在应用设置里改成
镜像源或本地 tag。

## 打包时需要留意

- **预览地址**：`ROUNDPEN_PREVIEW_PUBLIC_URL` 默认 `http://127.0.0.1:9527`，
  在 NAS 上应改成设备地址（系统管理 → 通用），否则预览链接只能本机打开。
  有通配域名（DNS + 证书覆盖 `*.rp.mk`）时可设 `ROUNDPEN_PREVIEW_DOMAIN=rp.mk`，
  预览改为每端口一个子域 `{沙箱}-{端口}.rp.mk`，前端资源的绝对路径与 HMR 更省事；
  反代需保留 Host、带上 `X-Forwarded-Proto`，并把反代网段写进 `ROUNDPEN_TRUSTED_PROXIES`。
- **端口**：`manifest.service_port` 与桌面入口固定 9527；改端口要同时改
  `app/ui/config`。
- **图标**：由 `web/public/favicon.svg` 渲染，改 logo 后跑
  `node deploy/fnos/scripts/gen-icons.mjs` 重新生成。
- **`os_min_version`**：真机测过之后再往 `manifest` 里补兼容范围。
- **docker 权限**：`config/privilege` 里 `join-groups: ["docker"]` 让生命周期脚本
  能以应用用户身份访问 `docker` 组受保护的 socket（加载镜像、查状态）。若设备上
  该用户组名不同，按实际调整。
- **纯离线安装**：`WITH_IMAGE=1` 的包已带 `roundpend` 镜像；pgvector 默认从
  Docker Hub（走 fnOS 的镜像加速）拉取，完全离线时可把它也 `docker save`
  成 `app/images/pgvector.tar` 再重新打包，`cmd/install_callback` 会一起加载。
