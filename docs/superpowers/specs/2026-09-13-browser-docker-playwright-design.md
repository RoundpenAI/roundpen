# Browser 环境迁移到 Docker + Playwright 引擎（多来源）设计

日期：2026-09-13
状态：待评审
影响面：`internal/browser`、`internal/userenv`、`internal/backend/multi`、`internal/api/envapi`、`internal/runtime`、`internal/hostsetup`、`internal/template`、`web`、`images/browser-qemu`（删除）

## 1. 背景与目标

现状：Browser 槽位是每用户一台 QEMU VM（qcow2 Ubuntu + XFCE + Chrome；CDP 经 hostfwd，桌面经 QEMU VNC→WebSocket），控制面用 chromedp 直连 CDP。运维重（宿主机要 QEMU/KVM、要构建 qcow2 盘），桌面链路复杂，且引擎是裸 CDP，agent 工具的可操作性等待全靠自己写。

目标：

1. **来源可插拔**：Browser 的浏览器可以来自四种来源 —— Roundpen 托管容器（默认）、局域网/自建 browserless、商业 browserless 云、roundpend 主机上的本机 Chrome。
2. **托管来源迁移到 Docker**：基于 `ghcr.io/browserless/chrome`（正牌 Google Chrome：H.264/AAC 等专有编解码、PDF 查看器都在），每用户一容器，而不是每用户一台 VM。
3. **控制面引擎改用 Playwright**（`playwright-go`），替换 chromedp：自动等待/可操作性检查让 agent 的 `browser_*` 工具更稳，且 Host/Remote/Cloud 都能用同一套引擎。
4. **实时视图替代 VNC 桌面**：托管容器用 browserless 自带 debugger 经控制面反代；外部来源直接打开上游 debugger；截图 + 像素输入的接管面板保留为通用兜底。

非目标（本期不做）：

- Desktop / Mobile 槽位（`internal/backend/qemu` 包保留，作为它们的落点）。
- 自研 CDP screencast 实时流：仅作为实时视图的兜底方案（见 4.4），不主动实现。
- cloud 来源的 live 链接自动发现（browserless.io 的会话控制台链接）。
- 浏览器容器内跑 Playwright（`/function` 方案）：不采用，引擎固定在控制面。

## 2. 已核实的事实（2026-09-13 实测）

| 事实 | 证据 |
|------|------|
| 局域网 `10.10.1.3:3000` = `ghcr.io/browserless/chrome:v2.56.7` | `GET /meta` → `{"version":"2.56.7","playwright":["1.63.0","1.62.1",...],"puppeteer":["25.10.0"],"chromium":null,...}` |
| v2 chrome 镜像的 CDP ws 路由是 `/chrome` | ws upgrade 探测：`/chrome` → 101，`/` → 404，`/chromium` → 404 |
| v2 的 `/json/version` 不能用于发现 ws 地址 | 返回 `webSocketDebuggerUrl: ws://0.0.0.0:3000/`（bind 地址），且根路径 ws 是 404 |
| v2 debugger 页面资源是相对路径 | `GET /debugger/` → `Browserless debugger` 页面，引用 `./router.js`、`app.bundle.js`、`favicon.ico` |
| LAN 部署无 token（本设计仍需支持 token） | 无 token 可访问 `/debugger/`、`/meta`、`/chrome` ws |
| `playwright-go` v0.6201.1 = Playwright 1.62.1 driver | 模块源码 `run.go: playwrightCliVersion = "1.62.1"`；支持 `ConnectOverCDP` / `Connect` / `Launch` |
| playwright-go 的 driver 可用系统 Node，不必换基础镜像 | `PLAYWRIGHT_DRIVER_PATH`（driver 目录）、`PLAYWRIGHT_NODEJS_PATH`（系统 node）、`PLAYWRIGHT_GO_NPM_REGISTRY`（npm 镜像，内网构建用） |

## 3. 架构

```
Agent browser_* 工具 / Browser 页面 / 接管面板（截图+像素输入）
                    │
             internal/browser（Hub + Engine 接口不变）
                    │        Engine = Playwright（唯一实现）
        ┌───────────┼───────────────────────────┐
        │           │                           │
   provider=docker  provider=remote/cloud   provider=host
   托管容器         直连 endpoint+token       playwright launch
   （browserless   （局域网自建 / 商业云）    （roundpend 主机
    /chrome:v2）                             上的 Chrome）
        │
   sandbox（Docker 后端）→ Backend.Dial(3000) → 本地 TCP 转发 → CDP ws
```

- `Engine` 接口（`internal/browser/engine.go`）不变；实现从 chromedp 换成 playwright-go。
- `Hub` 的会话模型不变：一个 hub key 一个引擎（一个 browser context + page）；托管来源的 key = browser 沙箱 id，外部来源的 key = `browser-<user>`。
- 浏览器来源由设置里的 provider 决定，对上层（agent 工具、API、UI）透明。

## 4. 组件设计

### 4.1 来源（provider）解析

沿用现有五值配置 `ROUNDPEN_CDP_PROVIDER = auto | docker | remote | cloud | host`：

| provider | 含义 | 生命周期 | 引擎接入方式 |
|---|---|---|---|
| `auto`（默认值）| 解析为 `docker` | — | — |
| `docker` | **Roundpen 托管容器**（语义变更，原「dial 进 QEMU guest」）| 每用户一容器，`user_environments.slot=browser`，TTL 24h | `Backend.Dial(id, 3000)` → 本地 TCP 转发 → `ws://127.0.0.1:<port>/<path>?token=<browserToken>` |
| `remote` | 局域网/自建 browserless（含 v1 老部署）| 外部，Roundpen 不管理 | 直连 `ROUNDPEN_CDP_ENDPOINT`（+ token）|
| `cloud` | 商业 browserless 云 | 外部 | 直连 `ROUNDPEN_CDP_ENDPOINT`（+ token）|
| `host` | roundpend 主机上的 Chrome | 外部（进程由 Playwright 拉起）| `Chromium.Launch(ExecutablePath=…)` |

**endpoint 规约**：`ROUNDPEN_CDP_ENDPOINT` 接受

- `ws://` / `wss://` URL：直接用（可含路径与查询串）；
- `http://` / `https://` URL：作为 origin，路径用候选探测；
- `ROUNDPEN_CDP_TOKEN` 非空时，以 `?token=` 追加（已有查询串则 `&token=`，值做 URL 转义）。

**候选路径探测**：v2 的 `/json/version` 不可用于发现（见 §2），因此连接时按顺序尝试候选 ws 路径 `["/chrome", "/chromium", "/"]`（endpoint 显式给了路径则只用该路径），首个握手成功者胜出，结果缓存在进程内（按 endpoint）。`cloud`/`remote`/`docker` 共用该逻辑；`host` 不适用。

**测试连接**：新增 admin 接口 `POST /v1/admin/settings/browser/test`（与 `GET/PUT /v1/admin/settings` 同一鉴权层级），对当前 provider 做一次探测 —— `GET {origin}/meta` 识别 browserless 版本与内置 playwright 版本，再跑候选路径探测给出命中的 ws 路径（`host` 模式则返回本机 Chrome 路径与版本）。结果只回显给设置页，不改写配置。设置页新增「测试连接」按钮调用它。

**`RequireBrowser` 重写**（`internal/runtime/probe.go`）：

- `docker`：Docker ready + 镜像在本地（缺失时 setup 给 `docker pull <ROUNDPEN_BROWSER_IMAGE>`）；不再检查 QEMU 二进制与 qcow2。
- `remote`/`cloud`：endpoint 非空即视为就绪（连通性由「测试连接」或首次 attach 报错体现）。
- `host`：`browser.ChromeOnPATH()` 为真即就绪；否则提示安装 Chrome。

### 4.2 托管容器（provider=docker）

- **镜像**：`ROUNDPEN_BROWSER_IMAGE`，默认 `ghcr.io/browserless/chrome:v2.56.7`（**与局域网同镜像同 tag**，一份镜像两处用）。语义从「qcow2 路径」改为「OCI ref」。许可提醒写入部署文档：browserless 非商业自用免费、商业使用需 license；`chrome` 变体为 Google Chrome，受其条款约束。
- **内置模板**：种子行 `browser-desktop`（`internal/template/store.go` 的 `ArtifactRef: browserArtifact`，即 `ROUNDPEN_BROWSER_IMAGE`，默认 qcow2 路径）替换为 `browser`：`slot=browser`、`ArtifactRef/BaseImage = ROUNDPEN_BROWSER_IMAGE`（默认改为 OCI ref）、`cpu=2`、`memory=2048MB`（原 4096）。`BuiltinNames` 与 `ROUNDPEN_DEFAULT_BROWSER_TEMPLATE` 默认值同步改名。`slot=browser` 的模板会自动置 `UseImageCmd`（`template/service.go` 现有逻辑），容器保留镜像 entrypoint，无需新字段。`runtime.EngineOfImage` 依现有规则（非 qcow2 → docker）判为 docker。
- **容器 env**（创建时写入）：
  - `TOKEN=<32 hex 随机>`：browserless 鉴权（同一 bridge 上其它沙箱容器理论上能摸到 :3000，必须开鉴权）；
  - `MAX_CONCURRENT_SESSIONS=3`、`CONNECTION_TIMEOUT=600000`、`ENABLE_DEBUGGER=true`；
  - `DEFAULT_LAUNCH_ARGS=["--window-size=1280,800","--hide-scrollbars","--mute-audio","--disable-dev-shm-usage"]`（Docker 默认 /dev/shm 只有 64MB，Chrome 会崩，故禁用 shm）；
  - `ROUNDPEN_SLOT=browser`、`ROUNDPEN_USER_ID=<user>`。
- **token 持久化**：写入 `sandbox.Metadata["browserToken"]`，随沙箱记录返回给属主（供引擎与实时视图反代使用）。不新增数据表。
- **端口与网络**：不发布端口；控制面 `Backend.Dial(id, 3000)`，复用现有 `startCDPProxy`（本地 TCP 转发）。
- **workspace**：bind 挂载用户 workspace 到 `/workspace`（下载文件落盘，agent 可从工作区取）。
- **生命周期**：`userenv.EnsureBrowser` 行为不变（ensure/adopt/失败重建），slot 记录、命名 `browser-<user>`、24h TTL 与 Agent 槽位一致。
- **探针**：见 4.1 的 `RequireBrowser`。
- **资源**：`cpu=2 / mem=2048MB`（Chrome 单实例实际占用通常 800–1500MB），模板可改。

### 4.3 引擎（playwright-go）

新增依赖：`github.com/playwright-community/playwright-go v0.6201.1`（driver = Playwright 1.62.1）。**移除 chromedp 与 cdproto 依赖**（确认无其它引用后）。

`internal/browser/playwright.go` 实现 `Engine`：

| Engine 方法 | Playwright 实现 |
|---|---|
| `Navigate` | `page.Goto(url, WaitUntil=domcontentloaded, Timeout)` |
| `URL` / `Title` | `page.URL()` / `page.Title()` |
| `Snapshot` | `page.Evaluate(snapshotJS)`（注入脚本原样保留，`data-rp-ref` 机制不变）|
| `Hover` / `Click` | `page.Locator("[data-rp-ref=\"<ref>\"]").Hover()/Click()`（自动等待可操作性）|
| `Type(ref,text,submit)` | `Locator.Fill(text)`，`submit` 为真再 `Locator.Press("Enter")` |
| `Press(key)` | `page.Keyboard.Press(key)` |
| `Screenshot` | `page.Screenshot()` → PNG 字节 |
| `SetViewport(w,h)` | `page.SetViewportSize(w,h)` |
| `Evaluate(expr)` | `page.Evaluate(expr)` → JSON |
| `InputClick/Move/Wheel` | `page.Mouse.Click/Move/Wheel`（CSS 视口坐标，人工接管用）|
| `InputType/InputKey` | `page.Keyboard.Type/Press` |
| `Close` | `browser.Close()` + `playwright.Stop()` |

接入方式：

- `docker`：`ConnectOverCDP(ws://127.0.0.1:<proxyPort>/<path>?token=<token>)`；
- `remote`/`cloud`：`ConnectOverCDP(endpoint)`（含候选路径探测与 token）；
- `host`：`Launch(ExecutablePath=ChromeOnPATH(), Headless=true, Args=默认尺寸参数)`；
- 连接成功后 `NewContext(viewport 1280×800, UserAgent=desktopChromeUA)` → `NewPage()`（保留现有 UA 伪装）。

driver 落地：

- 生产（`deploy/Dockerfile`）：运行时仍用 alpine；`apk add nodejs`，构建阶段以 `PLAYWRIGHT_DRIVER_PATH=/opt/playwright`、`PLAYWRIGHT_NODEJS_PATH=<apk node>`、`SkipInstallBrowsers=true` 把 driver（`playwright-core` 包）烘焙进镜像，运行时同环境变量复用。内网构建可设 `PLAYWRIGHT_GO_NPM_REGISTRY`。
- 裸机开发：首次运行自动下载 driver 到 `~/.cache/ms-playwright-go/<版本>`（约 50MB，不含浏览器）。

重试与错误：

- 保留 45s 等待窗口与「可重试」语义；`cdpRetryable` 的错误匹配改为 Playwright/browserless 的报错文本（连接被拒、握手未就绪、容器启动中），容器未起来时的失败可重试。

### 4.4 实时视图（替代 VNC 桌面）

- **托管来源**：新增专用路由
  - `GET /v1/me/environments/browser/live/*`：会话 Cookie 鉴权（与其它 `/v1/me` 相同）→ 定位属主的 browser 沙箱 → 反代到容器 `:3000`，路径映射 `/live/<rest>` → `/debugger/<rest>`（空则 `/debugger/`），**每个上游请求服务端注入 `?token=<browserlessToken>`**（预览代理会剥 `token` 参数，故不复用 `/p/` 通用代理）；WS 升级同样转发。
  - `GET /v1/me/environments/browser/live-link` → `{ "url": ..., "mode": ... }`：托管 → `/v1/me/environments/browser/live/`；`remote`/`cloud` → 上游 `<origin>/debugger/`（配置了 token 则带上，便于在局域网浏览器里直接打开）；`host` → 空（无实时视图）。
- **外部来源**：不经 Roundpen 反代，UI 直接打开上游 debugger（前提是用户浏览器可达该来源；cloud 若不支持此类页面则 UI 只显示来源信息）。
- **接管面板**（`AgentBrowserPanel`，CDP 截图 + 像素输入）四种来源全部保留，是实时视图之外的通用兜底。
- **风险与兜底**（实现第一步做 spike 验证，见 §7）：
  1. debugger 页面资源是相对路径（已核实），但 bundle 内部可能有根绝对路径的 API/WS 调用 → 前缀反代可能失效。兜底 A：live handler 注入 `<base>` 并对已知绝对路径做重写/302；兜底 B：自研 CDP screencast 实时流（`NewCDPSession(page)` + `Page.startScreencast` + `Input.*` 转发，扩展现有接管面板）。
  2. browserless v2 有 issue（#4224）反馈 debugger 的会话页点进去连不上实时 CDP，版本相关；spike 若不通过则走兜底 B。
  3. 桌面能力事实性消失：不再有 XFCE、任意应用、文件管理器；实时视图只覆盖浏览器页面本身。

### 4.5 API / 设置 / UI

- **API 变更**：
  - 删除 `GET /v1/me/environments/browser/desktop`、`GET /v1/me/environments/browser/desktop/ws`（及 `auth` 中间件里的 ws 白名单特例）。
  - 新增 `GET /v1/me/environments/browser/live/*`、`GET /v1/me/environments/browser/live-link`（见 4.4）。
  - `userenv.Service.EnsureBrowser` 返回值从 `*sandbox.Sandbox` 改为 `*userenv.BrowserTarget`：
    ```go
    type BrowserTarget struct {
        Key      string            // hub 会话 key：托管=沙箱 id，外部=browser-<user>
        Provider string            // 解析后的 provider
        Managed  bool              // 是否 Roundpen 托管容器
        Sandbox  *sandbox.Sandbox  // Managed 时非空
    }
    ```
    调用方同步调整：`envapi`（ensure 响应）、`agentapi/browser.go`（hub key）、`agentapi/tasks.go`、`sysagent` 的 `BrowserSlot` 接口。
  - `userenv.EnvView` 增加 `provider` 字段；外部来源时 browser 槽位 `status="external"`、`sandboxId` 为空。
- **设置**：
  - 设置页 provider 选择保留五值，文案更新：`docker` → 「Roundpen 托管容器（browserless/chrome）」，`host` → 「本机 Chrome」等（中英同步）。
  - endpoint/token/port 字段沿用；`cdpPort` 默认值 9222 → **3000**；新增「测试连接」按钮（`/meta` + 候选路径探测结果）。
  - 系统信息区展示解析后的 provider、`/meta` 版本、主机 Chrome 发现情况（现有 `cdpHint` 机制扩展）。
- **Browser 页面**按 provider 渲染：
  - 托管：容器状态卡片（状态 / 镜像 ref / browserless 版本）+「打开实时视图」+ Ensure；
  - remote/cloud：来源卡片（端点、版本、连接状态）+「打开上游 debugger」；
  - host：本机 Chrome 路径 + 版本，「无实时视图，使用代理接管」说明。
- **接管面板**不变。
- **浏览器任务**（`browser-tasks` explore/verify）不变，只是底层引擎换成 Playwright。

### 4.6 删除清单（QEMU Browser 链路硬切）

- Go：
  - `images/browser-qemu/`（Dockerfile、guest/、build.sh、out/、README）整体删除；
  - `Makefile` 的 `browser-image` 目标；
  - `internal/hostsetup`：browser 镜像构建动作与校验（`runner.go`、`store.go`、`actions.go`、`plan.go`、`llm_plan.go` 中 browser 相关分支）；
  - `internal/runtime/probe.go`：`RequireBrowser` 的 QEMU 检查（按 4.1 重写）、`browserImage()` 的 qcow2 默认值；
  - `internal/api/envapi`：`desktopLink`/`desktopWS`/`VNCSockLookup`/`desktopUpgrader`；
  - `internal/api/auth/middleware.go`：desktop ws 路径白名单；
  - `internal/rfbtest/`（仅 uismoke 使用）、`tests/uismoke` 的 RFB 部分；
  - `internal/backend/qemu`：`useVirtioWorkspace` 条件里的 browser 特判（browser 不再走 qemu，mobile 保留）；`VNCSock` 保留（Desktop 槽位落点）；
  - chromedp / cdproto 依赖（`internal/browser/chrome.go`、`remote.go` 随之重写/删除）。
- Web：
  - `web/vnc.html`、`web/src/vnc-client.ts`、`web/src/lib/novnc-rfb.ts`、`web/src/novnc-rfb.d.ts`、`@novnc/novnc` 依赖、`web/e2e/vnc.spec.ts`；
  - `web/src/api.ts` 的 `browserDesktop` → `browserLive`；`BrowserPage.tsx` 桌面入口替换。
- Docs：`docs/architecture/qemu-browser.md` 重写为 `docs/architecture/browser-env.md`；README、`project-layout.md`、`environment-services.md` 中 Browser=QEMU 的描述更新。

### 4.7 数据与升级

- 已存在的 browser 沙箱（qcow2）在升级后首次 `EnsureBrowser` 时因引擎不匹配（qcow2 → docker）走既有 `errSlotFailed` 路径：删除并重建为容器，无需数据迁移。
- 模板 `browser-desktop` 内置记录替换为 `browser`；历史模板行保留不影响（不再被引用）。
- `user_environments` 表结构不变。

## 5. 配置项汇总

| 变量 | 默认 | 说明 |
|---|---|---|
| `ROUNDPEN_CDP_PROVIDER` | `auto`（→`docker`）| 来源选择 |
| `ROUNDPEN_CDP_ENDPOINT` | 空 | `remote`/`cloud` 的 ws/http(s) 端点 |
| `ROUNDPEN_CDP_TOKEN` | 空 | 外部来源 token |
| `ROUNDPEN_CDP_PORT` | `3000`（原 9222）| 托管容器内 CDP 端口 |
| `ROUNDPEN_BROWSER_IMAGE` | `ghcr.io/browserless/chrome:v2.56.7` | 托管 OCI 镜像（语义变更，`internal/config` 与 `internal/template` 两处默认值都改）|
| `ROUNDPEN_DEFAULT_BROWSER_TEMPLATE` | `browser`（原 `browser-desktop`）| 槽位模板名（已有变量，改默认值）|

## 6. 测试与验收

单元测试：

- provider 解析与候选路径顺序（config）；
- `BrowserTarget` 解析（托管/外部、hub key）；
- live handler：路径映射 `/live/*` → `/debugger/*`、token 注入、未认证拒绝（httptest 假上游）；
- hub：连接重试与错误匹配（假引擎，沿用现有 hub_test 结构）；
- `EnvView.provider` 与外部来源状态。

集成测试（gated，环境变量缺失则 skip）：

- `ROUNDPEN_TEST_BROWSER_WS`（如 `ws://10.10.1.3:3000/chrome`）：跑引擎全方法 —— navigate、snapshot、hover、click、type、press、screenshot、set viewport、evaluate、像素输入。

e2e（web）：

- 删除 `vnc.spec.ts`；`tests/uismoke` 提供 live-link 桩；`browser.spec.ts` 相应更新。

手工验收：

1. `remote` 指向 LAN(`10.10.1.3:3000`)：Browser 页显示来源与 v2.56.7；跑一次 browser 任务（explore/verify）；接管面板截图与像素输入可用；「打开上游 debugger」能看到会话并接管（spike 条件）。
2. 托管（devbox）：Ensure 拉起 `browserless/chrome` 容器 → 状态 running、browserless 版本可见 → agent `browser_*` 工具全绿 → 「打开实时视图」在浏览器中可达并接管 → Stop/Start/Delete 生命周期正确回收容器。
3. `host`：装 Chrome 的机器上 provider=host 可导航、截图、接管；无 Chrome 时设置页给出明确提示。
4. 升级路径：带旧 qcow2 browser 沙箱的实例，升级后 ensure 自动重建为容器。

## 7. 实施顺序（供 writing-plans 分解）

1. **Spike（先做，决定实时视图路线）**：用 LAN v2 验证「Playwright 连接 → 会话出现 → debugger 可看可接管」；同时验证 debugger 在路径前缀反代下的资源/接口可达性。
2. 引擎：playwright-go 落地 + Engine 实现 + gated 集成测试；`remote` 模式端到端跑通（不改托管）。
3. 来源模型：provider 解析、候选探测、`BrowserTarget` 重构、设置「测试连接」。
4. 托管容器：模板/镜像/探针/容器 env/token 持久化，multi 路由（browser→docker）。
5. 实时视图：live 路由 + live-link + UI 替换（或 spike 兜底方案）。
6. 清理：QEMU Browser 链路、VNC/noVNC、hostsetup、文档。
7. e2e 与手工验收。

## 8. 风险

| 风险 | 影响 | 缓解 |
|---|---|---|
| debugger 前缀反代不可用（bundle 用根绝对路径）| 实时视图不可用 | 兜底 A：`<base>` 注入 + 路径重写；兜底 B：自研 screencast |
| browserless v2 debugger 会话接管 bug（#4224）| 实时视图能看不能用 | spike 前置验证；同上兜底 |
| `host` 模式：主机无 Chrome / 无显示 | 该模式不可用 | 明确提示；仅 headless 语义，无实时视图 |
| alpine + apk nodejs 版本不满足 driver | 镜像构建失败 | 构建期验证；必要时基础镜像升到 node 22 的 alpine 版本 |
| 外部来源（cloud）出网/跨域限制 | 用户打不开 debugger | UI 只展示来源信息，不承诺 live |
| 许可 | 合规风险 | 部署文档注明 browserless 许可与 Chrome 条款 |

## 9. 验收标准

- [ ] provider 五值可用，`auto`→`docker`；四种来源都能通过设置页配置并「测试连接」。
- [ ] 托管 Browser 槽位是 Docker 容器（browserless/chrome），QEMU/qcow2 路径完全移除（含 hostsetup、make 目标、images/、VNC 桌面链路）。
- [ ] `browser_*` 工具（navigate/snapshot/hover/click/type/press/screenshot/set_viewport/evaluate/explore）与接管面板在托管与 remote 两种来源下全绿。
- [ ] 实时视图：托管经 `/v1/me/environments/browser/live/` 可达并可接管；remote/cloud 可打开上游 debugger 或给出明确说明；host 显示「无实时视图」。
- [ ] chromedp 依赖移除，`go test ./...` 与 gated 集成测试通过，web e2e 更新后通过。
- [ ] 旧 qcow2 browser 沙箱升级后自动重建为容器。
