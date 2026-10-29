# Browser 固定环境

Browser 槽位是每个用户一个浏览器来源：默认由 Roundpen 托管一个 browserless Chrome 容器，也可切到局域网 / 商业云 / 本机 Chrome。控制面用 Playwright 引擎经 CDP 驱动，对上层（agent 工具、Browser 页、接管面板）呈现同一套会话模型。

旧的每用户 QEMU 桌面（qcow2 + XFCE + VNC）已整体删除；历史见 git 记录。

## 三层模型（Runtime / Session / Profile）

| 层 | 含义 | 代码 |
|----|------|------|
| **Runtime** | Chromium 真正跑在哪（托管容器或外部 CDP） | `browser.Runtime`，由设置里的 **Source**（provider/endpoint/token/port）在 attach 时解析 |
| **Session** | 一次可控制的 attach：引擎、接管、生命周期 | `browser.Session`（Hub 按 key 缓存） |
| **Profile** | 跨 Session 的用户数据（Cookie / storage） | `browser.Profile`；控制面磁盘上的 Playwright `storage.json`，docker / remote / cloud 共用 |

说明：

- 设置项里的「Browser source」是 **Source**（CDP 从哪来），不是 Chromium user-data Profile。
- Session 内目前只有一个 active **Tab**（`main`）；多标签未对 Agent 暴露。
- Agent 工具仍是裁剪后的 `browser_*`，不是完整 Playwright API。
- Profile 持久化靠 Roundpen 侧 storage state，不依赖远程机上的 user-data-dir，因此远程 browserless 同样适用。

## 来源（provider）

`ROUNDPEN_CDP_PROVIDER` 决定浏览器从哪来（设置页「CDP 提供方」可热改）：

| provider | 含义 | 生命周期 | 引擎接入 |
|----------|------|----------|----------|
| `auto`（默认） | 解析为 `docker` | — | — |
| `docker` | Roundpen 托管容器（browserless/chrome，每用户一个） | 每用户一容器：`user_environments.slot=browser`、命名 `browser-<user>`、TTL 24h | `Backend.Dial(id, ROUNDPEN_CDP_PORT)` → 本地 TCP 转发 → Playwright `ConnectOverCDP`（`ws://127.0.0.1:<proxy>/<path>?token=<browserToken>`） |
| `remote` | 局域网 / 自建 browserless（含 v1 老部署） | 外部，Roundpen 不管理 | 直连 `ROUNDPEN_CDP_ENDPOINT`（+ 可选 token） |
| `cloud` | 商业 browserless 云 | 外部 | 同 `remote` |
| `host` | roundpend 主机上的 Chrome | 外部（进程由 Playwright 拉起） | `Launch(ExecutablePath=…)`，headless；**无实时视图** |

endpoint 规约（`remote` / `cloud`）：

- `ws://` / `wss://`：直接使用（可含路径与查询串）；
- `http://` / `https://`：作为 origin，ws 路径按候选探测；
- `ROUNDPEN_CDP_TOKEN` 非空时以 `?token=` 追加（已有查询串则 `&`，值做 URL 转义）。

候选 ws 路径按顺序尝试 `/chrome`（v2 chrome 镜像）→ `/chromium` → `/`（endpoint 显式给了路径则只用该路径），首个握手成功者胜出，结果按 endpoint 缓存在进程内。**不要**用 `/json/version` 做发现：它返回的是服务端 bind 地址（`ws://0.0.0.0:3000/`），不是可连的路径。

`auto` 只解析为 `docker`；host Chrome 是显式 opt-in（`ROUNDPEN_CDP_PROVIDER=host`），不会自动降级。

## 托管容器（provider=docker）

| 项 | 说明 |
|----|------|
| 镜像 | `ROUNDPEN_BROWSER_IMAGE`，默认 `ghcr.io/browserless/chrome:v2.56.7`（OCI ref） |
| 模板 | `ROUNDPEN_DEFAULT_BROWSER_TEMPLATE`，内置种子名 `browser`：`slot=browser`，`ArtifactRef`/`BaseImage` = 上面的镜像，cpu=2 / mem=2048MB；`slot=browser` 自动保留镜像 entrypoint（`UseImageCmd`） |
| 后端 | `internal/backend/multi` 按 slot 路由：`browser`（及 agent）→ Docker；只有 `mobile` / `.qcow2` 才走 QEMU |
| 端口 | 容器内 CDP `ROUNDPEN_CDP_PORT`（默认 `3000`）；**不发布端口**，控制面经 `Backend.Dial` 拨入容器网络（bridge 直连），再在本地起 TCP 转发给引擎 |
| 工作区 | 容器 bind 挂载该沙箱自己的 workspace 目录（`{data_root}/sandboxes/<id>/workspace`）到 `/workspace`；独立目录，不与 Agent 槽位共享 |
| 就绪 | `runtime.Probe.RequireBrowser`：Docker ready（镜像可缺省；`roundpend` 启动与 fnOS 安装/启动会后台 `EnsureLocalImage` / `docker pull` 预热） |

容器 env（创建沙箱时写入）：

| 变量 | 值 | 用途 |
|------|----|------|
| `TOKEN` | 32 位 hex 随机 | browserless 鉴权（同一 bridge 上其它容器理论上可摸到 :3000，必须开鉴权） |
| `MAX_CONCURRENT_SESSIONS` | `3` | 每容器并发会话上限 |
| `CONNECTION_TIMEOUT` | `600000` | 会话超时（ms） |
| `ENABLE_DEBUGGER` | `true` | 打开 browserless 自带 debugger（实时视图依赖） |
| `DEFAULT_LAUNCH_ARGS` | `["--window-size=1280,800","--hide-scrollbars","--mute-audio","--disable-dev-shm-usage"]` | Docker 默认 `/dev/shm` 只有 64MB，Chrome 会崩，故禁用 shm |
| `ROUNDPEN_SLOT` / `ROUNDPEN_USER_ID` | `browser` / 用户名 | 槽位标识 |

`TOKEN` 同时写入沙箱记录 `Metadata["browserToken"]`（随沙箱返回给属主），引擎与实时视图反代都从这里读，不新增数据表。

生命周期：`userenv.EnsureBrowser` 行为与 Agent 槽位一致 —— 已有记录则 adopt（paused/stopped 自动 Connect），引擎不匹配（旧 qcow2）或 failed 则删除重建，命名 `browser-<user>`，TTL 24h。

## 引擎（Playwright）

`internal/browser` 的 `Engine` 接口不变，实现是 playwright-go（`github.com/mxschmitt/playwright-go v0.6201.1`，内置 driver = Playwright 1.62.1）；Hub 的会话模型不变：一个 hub key 一个引擎（一个 context + page），托管来源的 key = browser 沙箱 id，外部来源的 key = `browser-<user>`。`Navigate` / `Click` / `Type` 等动作带自动等待与可操作性检查；快照注入脚本与 `data-rp-ref` 机制保持原样。

driver 只装 driver、不下载浏览器（浏览器在 Browser env 容器里）：

```bash
make browser-driver            # = go run ./cmd/browserdriver
```

官方镜像（`deploy/Dockerfile`）已把 driver 烘焙进 `/opt/playwright`（`PLAYWRIGHT_DRIVER_PATH=/opt/playwright`，运行时用镜像内 alpine `nodejs`），离线 / 内网部署开箱即用；裸机 / 开发环境首次跑引擎仍会自动安装（幂等，约几十 MB；从 npm / Node 分发源下载），也可用上面的 `make browser-driver` 显式安装。相关变量：

| 变量 | 含义 |
|------|------|
| `PLAYWRIGHT_DRIVER_PATH` | driver 目录（默认用户缓存 `ms-playwright-go/<版本>`）；`cmd/browserdriver` 与运行时都认 |
| `PLAYWRIGHT_NODEJS_PATH` | 复用系统 Node，跳过 driver 附带 Node 的下载（无预编译 Node 的平台必需） |
| `PLAYWRIGHT_GO_NPM_REGISTRY` | 下载 playwright-core 用的 npm 镜像（内网 / 镜像构建） |

自建镜像可仿照 `deploy/Dockerfile` 在构建阶段跑 `go run ./cmd/browserdriver`（配 `PLAYWRIGHT_DRIVER_PATH=/opt/playwright` 与 `PLAYWRIGHT_NODEJS_PATH`），运行时用同一组环境变量复用。

## 实时视图（替代 VNC 桌面）

| 来源 | 观看方式 |
|------|----------|
| `docker` | 控制面反代容器内 browserless debugger：`GET /v1/me/environments/browser/live/`（会话 Cookie 鉴权，与其它 `/v1/me` 相同） |
| `remote` / `cloud` | UI 直接打开上游 `<origin>/debugger/`（配置了 token 则带上）；是否可达取决于用户浏览器到该来源的网络 |
| `host` | 无实时视图，`live-link` 返回 `hint` 提示改用截图接管面板 |
| 全部来源 | **截图 + 像素输入接管面板**（headless CDP 截图流）是通用兜底 |

`GET /v1/me/environments/browser/live-link` 返回 `{mode, url, hint?}`：

```json
{ "mode": "managed", "url": "/v1/me/environments/browser/live/" }
```

`GET /v1/me/environments/browser/live/*` 反代托管容器的 debugger：

- 路径映射：`/live/<rest>` → `/debugger/<rest>`（空则 `/debugger/`；debugger 资源是相对路径，前缀反代即可，仅保留根斜杠以免绝对 `Location` 把浏览器顶出代理前缀）；
- **服务端注入** `?token=<browserToken>`（每次上游请求都重写），并剥离控制面会话凭据（Cookie / Authorization / X-API-Key），避免泄漏进沙箱；
- WebSocket 升级同样转发；
- 解析出的沙箱 + token 缓存约 10s，拨入失败即失效重解析。

旧的 `GET /v1/me/environments/browser/desktop`（WS）与 noVNC 资产已删除；`internal/api/auth` 里的 desktop ws 白名单特例一并移除。

## 就绪与测试连接

`runtime.Probe.RequireBrowser` 按 provider 判定：

- `docker`：Docker ready + 镜像在本地；
- `remote` / `cloud`：endpoint 非空（连通性由测试连接或首次 attach 报错体现）；
- `host`：主机 PATH 上有 Chrome。

管理端新增 `POST /v1/admin/settings/browser/test`（admin 鉴权）：对当前 provider 探测，返回 browserless `/meta` 版本与内置 playwright 版本、命中的 ws 路径，或 host Chrome 的路径。只回显结果，不改写配置。

## 配置

| 变量 | 默认 | 含义 |
|------|------|------|
| `ROUNDPEN_CDP_PROVIDER` | `auto`（→`docker`） | 来源选择：`auto` / `docker` / `remote` / `cloud` / `host` |
| `ROUNDPEN_CDP_PORT` | `3000` | 托管容器内 CDP 端口（live 反代拨入同一端口） |
| `ROUNDPEN_CDP_ENDPOINT` | 空 | `remote` / `cloud` 的 ws(s) / http(s) 端点；这两个 provider 必填 |
| `ROUNDPEN_CDP_TOKEN` | 空 | `remote` / `cloud` 的 token，以 `?token=` 追加 |
| `ROUNDPEN_BROWSER_IMAGE` | `ghcr.io/browserless/chrome:v2.56.7` | 托管容器 OCI 镜像（`internal/config` 与 `internal/template` 两处默认值一致） |
| `ROUNDPEN_IMAGE_MIRRORS` | 空（启用内置 ghcr 回退） | 逗号分隔拉取模板/主机；`none` 关闭内置回退 |
| `ROUNDPEN_IMAGE_MIRROR_RACE_DELAY` | `15` | 官方拉取进行中多久后并行试第一个镜像；`0` 关闭竞速 |
| `ROUNDPEN_DEFAULT_BROWSER_TEMPLATE` | `browser` | 槽位模板名 |
| `ROUNDPEN_QEMU_ENABLED` / `ROUNDPEN_QEMU_BIN` | `true` / `qemu-system-x86_64` | 只影响 Desktop / Mobile 槽位；Browser 不再使用 QEMU |

部署前置：Docker（托管来源）。控制面在 Docker 就绪后异步预拉 `ROUNDPEN_BROWSER_IMAGE`（与 Agent 镜像）；fnOS 安装/启动另有一层后台 `docker pull`。首次 `browser_navigate` 仍会在镜像未就绪时走同一拉取路径（进程内 singleflight），但多数情况下安装后镜像已在本地。`host` 来源还需要宿主机装 Chrome。

说明：Browser 镜像约 1GB+ 压缩体积，不适合打进 `.fpk`；“内置初始化”指安装/启动时自动预热上游 browserless 镜像，而不是把 OCI 层塞进安装包。需要更小体积时可改 `ROUNDPEN_BROWSER_IMAGE` 为 `ghcr.io/browserless/chromium:v2.56.7`（路径探测会命中 `/chromium`）。

### 镜像拉取与国内网络

`daemon.json` 的 `registry-mirrors` 只作用于 Docker Hub，**不能**加速 `ghcr.io`。控制面拉取（预热 / `EnsureLocalImage` / `RefreshImage`）在官方源失败时会按序尝试：

1. 进程内记住的上次成功源  
2. 原始 ref（如 `ghcr.io/browserless/chrome:v2.56.7`）  
3. `ROUNDPEN_IMAGE_MIRRORS` 自定义模板（`{image}` = 完整 ref，`{path}` = 去掉 registry 后的路径）  
4. 内置回退（按序，任一成功即停；限速源靠后）：
   - `ghcr.1ms.run/{path}`（毫秒镜像免费 ghcr 通道）
   - `ghcr.nju.edu.cn/{path}`（南大镜像站缓存；新 tag 可能尚未同步）
   - `m.daocloud.io/{image}`、`ghcr.m.daocloud.io/{path}`（DaoCloud，常用但有限速）

从镜像源拉成功后会 `docker tag` 回逻辑名，上层配置无需改写。设 `ROUNDPEN_IMAGE_MIRRORS=none` 可关闭内置回退。第三方镜像源无 SLA，内置多源是为了提高「装完能用」概率，不是对某一家做背书。

**官方源能通但很慢：** 默认在开始拉官方镜像约 15s 后，若仍未完成，会并行启动列表中的第一个镜像源，谁先成功用谁（并取消另一方）。可用 `ROUNDPEN_IMAGE_MIRROR_RACE_DELAY`（秒）调整等待；`0` 关闭竞速，改回「官方失败再试镜像」。

## API

- `GET /v1/me/environments` — 槽位状态（browser 槽含 `provider`；外部来源 `status=external`）
- `POST /v1/me/environments/browser/ensure` — 启动/恢复 Browser 环境
- `GET /v1/me/environments/browser/live-link` — 实时视图入口 `{mode,url,hint?}`
- `GET /v1/me/environments/browser/live/*` — 托管容器 debugger 反代（含 WS）
- `POST /v1/admin/settings/browser/test` — 连接测试（admin）

## 许可

- **browserless**：非商业自托管免费；商业使用需要 license（见 browserless 官方条款）。
- 默认的 `chrome` 镜像变体打包的是 Google Chrome，受 Google 的条款约束；需要规避时改用其 `chromium` 变体并同步 `ROUNDPEN_BROWSER_IMAGE`（路径探测会命中 `/chromium`）。

## 升级

- 旧的 qcow2 browser 沙箱在升级后首次 `EnsureBrowser` 因引擎不匹配（qcow2 → docker）走既有 `errSlotFailed` 路径：**删除并重建为容器**，无需数据迁移。
- 内置模板 `browser-desktop` 替换为 `browser`；历史模板行保留但不再被引用。`user_environments` 表结构不变。

## 二期

Agent 和 Browser 都用 Docker（见上）。`internal/backend/qemu` 留给以后的 Desktop / Mobile 虚拟机槽位（qcow2、kernel sidecar、VNC unix sock）。以前的 Agent-on-QEMU 和 `images/agent-qemu/` 已删，细节看 git 历史。
