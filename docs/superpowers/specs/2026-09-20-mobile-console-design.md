# 设计：移动端控制台（Flutter）

> 日期：2026-09-20
> 状态：**已实施**（2026-09-20）；仅剩设备端手工验收（本机无 Android SDK/模拟器）
> 范围：`mobile/`（新 Flutter 工程）/ `internal/api/auth`（登录可返回会话 token、Bearer 接受会话 token）/ `docs/auth.md` / `Makefile`、`README.md`、`docs/architecture/project-layout.md`

## 背景与目标

Roundpen 目前唯一的客户端是 `web/` 控制台（React 19 + Vite + Semi Design，桌面优先，手机上靠媒体查询兜底）。
自托管用户在手机上只能开浏览器：聊天能读，但输入体验差、退到后台再回来会丢流、也没法用系统级能力（安全存储、后续推送）。

本轮新增一个 **Flutter 移动端 App**，首期只做**对话核心**：
配置服务器地址 → 登录 → 助手列表 → 会话列表（新建/重命名/删除）→ 聊天（WS 流式、工具调用、权限确认、断线重连回放）。

**命名**：README 与 `docs/architecture/project-layout.md` 里的 **Mobile** 指的是 Agent 槽位（QEMU 预留，`userenv.SlotMobile`），
与本 App 不是一回事。文档与代码一律称本 App 为**移动端控制台（mobile console）**，目录 `mobile/`，
pubspec 包名 `roundpen_console`。任何提到它的地方首次出现都要写清"Flutter 客户端，与 Mobile 槽位无关"。

## 需求结论（澄清结果）

1. **形态**：仓库内新增移动端 App（不是 WebView 壳，也不是先做 monorepo 抽共享包）。
2. **框架**：**Flutter**（Dart）。选型理由见下节——决定性的一条是原生 WebSocket 能在握手带上鉴权 header。
3. **首期范围**：对话核心——登录、助手列表、会话列表（新建/重命名/删除）、聊天（流式 + 工具调用 + 权限确认 + 重连回放）。
4. **认证**：登录接口在原生客户端显式要求时，把**本次会话的 token**（不是 API Key，见 §1）一并返回；
   App 存入安全存储，REST 与 WS 统一用 `Authorization: Bearer`，端上完全不依赖 Cookie。

## 方案选择

### 形态

**方案 A（采用）：新增移动端 App。** 直接复用 `/v1` REST + WS，体验与系统能力（安全存储、深链、后续推送）都成立。

- 方案 B（壳 + WebView 加载现有控制台）被否：前端零改动，但流式聊天在 WebView 里体验受限、
  原生的安全存储/推送用不上，最终还是要重做一遍 UI。
- 方案 C（先把 `web/src/api.ts`、types、i18n 抽成共享包再建 App）被否：首期收益低、改动面大，
  且 web 的 API 层深度依赖浏览器语义（`credentials:'include'`、`window.location` 拼 WS URL）。
  记入 follow-on：App 稳定后若两份 API 层开始漂移，再谈抽包。

### 框架：Flutter vs React Native

两者都能完成首期，代码可复用性差异很小（web 侧只有约 300 行 fetch 包装和 3 个纯函数模块可借鉴）。
决定性的差异在 **WebSocket 鉴权**：

- 浏览器的 WS 靠 Cookie 自动带上，原生端必须自己把凭据放进升级请求。
  **React Native 内置 WebSocket 的自定义 header 在 iOS 上不可靠**（长期未修的已知问题），
  要用它就必须在服务端新增一次性短 ticket 端点 + 中间件分支才能保证两端可用。
- **Flutter 的 `dart:io` WebSocket 原生支持握手 header**：`WebSocket.connect(url, headers: {...})`，
  每次（重）连都会带上。服务端因此不需要 ticket 机制——`auth.Middleware`
  包住整个 mux（`cmd/roundpend/main.go:449`），WS 升级请求同样过鉴权；
  agentapi 的 upgrader 又是 `CheckOrigin: true`（`internal/api/agentapi/handler.go:31-35`），
  没有 Origin 障碍。唯一要补的是让 Bearer 认会话 token（§2，已实施）。

Flutter 的代价与缓解：

- 引入第三套 SDK（Dart/Flutter），仓库工具链从 Go + npm 变成 Go + npm + pub；
  但 App 是独立工程，控制面二进制仍然零依赖（`build` / `test` 不接 `mobile-*`）。
- 与 web 不能共享代码（连那 3 个纯函数模块也要用 Dart 重写）——量小，且渲染思路可以照搬。
- **Markdown 生态正在换代**：官方 `flutter_markdown` 已于 2025 年停止维护，
  接棒的是社区 fork（`flutter_markdown_plus`）与 AI 聊天向的新包（`gpt_markdown`、pre-release 的 `streamdown`）。
  缓解：**封装自有 `MarkdownBody` widget**，内部先用 `flutter_markdown_plus`；
  将来换实现只改这一个文件。流式正文另有节流（见 §6），不依赖包的增量解析能力。
- **Flutter Web 不可用**：浏览器 WebSocket 禁止自定义握手 header（与 RN 的 iOS 问题同源），
  `flutter run -d chrome` 下 WS 鉴权必然失败。调试只用模拟器/真机；本 App 不把 Web 列为目标平台。

### 其它选型

| 维度 | 选择 | 理由 |
|------|------|------|
| 导航 | **go_router** | Flutter 团队维护（flutter/packages）；路由形状与 web 控制台一致（`/assistants/:id/sessions/:sid` ↔ `/a/:assistantId/s/:id`），后续推送深链是加路由而不是改结构 |
| 状态管理 | **无库**：纯 Dart reducer + `ChangeNotifier` | 与 `web/` 保持一致（web 也没有 Redux/Zustand/Query）；流式正文单独用 `ValueNotifier<String>` 驱动 |
| HTTP | **package:http** + 薄封装 | 只有普通请求（流走 WS），不需要 dio 的拦截器体系 |
| 安全存储 | **flutter_secure_storage**（Keychain / Keystore）；非敏感项 `shared_preferences` | 会话 token 必须进系统钥匙串，不能落普通偏好存储 |
| Markdown | **flutter_markdown_plus**，封装在自有 `MarkdownBody` 后面 | 见上 |
| 单测 | **flutter test**（内置） | 纯 Dart 逻辑（帧解析/reducer/outbox/退避/工具统计）不依赖 Flutter，测试快 |
| UI | **Material 组件 + 一份 theme** | 不引入 UI kit；Semi 没有 Flutter 版本 |

## 设计

### 1. 服务端：登录可选返回**会话 token**（不是 API Key）

最初的设计是"登录返回该用户的 API Key"，动手前核实发现**做不到**：数据库只存 API Key 的哈希
（`storedAPIKey`，`internal/storage/user.go:32`，`Upsert` 时哈希，pg 与内存实现一致），
明文只在生成/轮换的那一刻存在于内存里。`RotateAPIKey` 能返回明文是因为它当场生成了新的 key——
但用它换取登录凭据会作废其它设备（CLI、另一台手机）手里的 key，不可接受；而手机端粘贴 API Key
的体验正是选型时被否掉的那一项。

改用**会话 token**——它天然满足所有约束：登录时本来就要签发、存储的是哈希、**每设备一条**、
可单独吊销、7 天滑动过期（`internal/api/auth/session.go:15`，`Touch` 续期）。

```
POST /v1/auth/login   { user, password, returnSessionToken?: true }
200                   { user: {...仍掩码...}, sessionToken?: "<64 hex>" }
```

- `sessionToken` 是 `user` 的**兄弟字段**；`issueSession` 改为返回明文（今天只写 Cookie，明文被丢掉），
  仅当显式要求时放进响应体。web 控制台（`web/src/api.ts:105`）忽略多余字段，零影响。
- 限流器（`loginLimiter`）不变：新字段在口令校验**之后**才读取，成功才 `clear(ip)`，
  不存在绕过路径（`TestLogin_RateLimitUnaffectedByReturnSessionToken` 锁住顺序）。
- 口令错误时绝不返回 token（`TestLogin_FailedPasswordNeverReturnsSessionToken`）。
- 审计：仅在真的签发 token 时记一行 `slog.Info("auth.login", "user", …, "return_session_token", true, "ip", …)`——不记 token 本身。

### 2. 服务端：Bearer 接受会话 token

原生客户端没有 Cookie 罐，WS 升级靠握手 header（见 §2.1），所以服务端要让 `Authorization: Bearer`
既认 API Key 也认会话 token：

- `internal/api/auth/middleware.go` 的 `resolveAuth`：先按老路走 API Key；若 Bearer 值**不以 `rp-` 开头**
  则按会话 token 处理（哈希 → `GetByTokenHash` → `Touch`），成功则返回与 Cookie 路径一致的
  `(user, user.APIKey, sess.ID)`——把 sessionID 放进 context，登出与审计都拿得到。
- `X-API-Key` 头**不**接受会话 token（只有 `Authorization` 可以），避免把两种凭据混成一种。
- `Logout` 改为优先用 context 里的 sessionID 吊销，回退到 Cookie——否则 Bearer 客户端点"退出登录"
  只是清了个不存在的 cookie，会话在服务端仍然有效。
- 安全性不变：会话 token 本来就是 bearer 凭据（放在 HttpOnly Cookie 里只是浏览器侧的防 XSS 措施），
  改用显式 header 不会引入 CSRF（CSRF 依赖浏览器自动附带凭据，显式头不会）。

### 2.1 WS 鉴权：握手 header（无需 ticket）

客户端在**升级请求**上直接带凭据：

```dart
final ws = await WebSocket.connect(
  'ws://<host>:19001/v1/agent-sessions/$id/ws',
  headers: {'Authorization': 'Bearer $sessionToken'},
);
```

- `dart:io` 的 `WebSocket.connect` 每次（重）连的握手都会带上 headers，重连不需要额外取票步骤；
  服务端中间件包住整个 mux（`cmd/roundpend/main.go:449`），WS 升级请求同样过鉴权。
- agentapi 的 upgrader 是 `CheckOrigin: true`（`internal/api/agentapi/handler.go:31-35`），无 Origin 障碍。
- **不要**用 Flutter Web 调试（浏览器不允许 WS 自定义 header，会 401）；
  也**不要**退化成 `?token=` 查询串——凭据进反代日志与 `slog` 的 path 字段，是明确拒绝的方案。
- 回归测试：`TestMiddleware_WSUpgradeWithBearerSessionToken` 用真实服务器 + 真实 websocket 客户端
  验证「无凭据 401 / 带 Bearer 会话 token 升级成功」（`internal/api/auth/middleware_test.go`）。

### 3. 客户端认证与存储

- 登录页收集**服务器地址 + 用户名 + 密码**；登录请求带 `returnSessionToken: true`。
- 会话 token 存 `flutter_secure_storage`（Keychain / Keystore）；地址与"最后使用的助手"等非敏感项存 `shared_preferences`。
- 所有 REST 与 WS 升级请求带 `Authorization: Bearer <sessionToken>`；**不**依赖 Cookie
  （`dart:io` 的 HttpClient 自带 cookie 罐，但我们不依赖它，也正因为如此才不用 Cookie 头传凭据）。
- 全局 401 钩子：清空安全存储 → 跳登录页（每次失败只触发一次）。改密码、服务端吊销、token 过期都会走到这里。
- 设置页：改服务器地址、显示当前账号、退出登录（调 `/v1/auth/logout` **服务端吊销**该会话再清本地）。
  API Key 轮换入口不放 App——它只影响 CLI/自动化，与 App 的会话无关。

### 4. API 客户端

`mobile/lib/api/` 结构对齐 `web/src/api.ts`（薄封装 + 类型化命名空间），差异点：

- **绝对地址**：web 走 Vite 同源代理所以用相对路径；App 必须持有 origin。
  `configureBaseUrl()` / `getBaseUrl()`，输入归一化（`192.168.1.5:19001` → `http://192.168.1.5:19001`，去掉尾斜杠）。
- 保留 `ApiError` 形态（web `api.ts:40-101`）：服务端 `writeErr` 返回 `{"message": …}`，映射成 `{status, message}`。
- 超时用 `Future.timeout` + 每次请求新建 `http.Client`（用完即关，避免连接泄漏）。
- 只移植首期需要的命名空间：`auth`（login/me/logout）、`assistants`（list/ensureSession）、
  `agents`（sessions/create/get/rename/delete/messages）。templates/sandboxes/browser/workspace/issues/admin 一律不搬。

### 5. 会话 WS 协议处理

出入站帧（`internal/api/agentapi/handler.go:340-359`，`runner.go:100-134,483-489`）与首期处理：

| 方向 | 帧 | 载荷 | 首期行为 |
|------|----|------|----------|
| S→C | `hello` | `message` | 标记已连接；补发 `auto`；检查 outbox |
| S→C | `status` | `busy, reply?, thought?, perm?{requestId,title,options,ticketId?}` | 权威快照：设 busy；**用 `reply` 整体替换**（绝不追加）流式气泡内容；按 `perm` 弹出/收起权限弹层；`busy=false` → 清流式标记、重拉 `/messages`、冲 outbox |
| S→C | `event` `agent_message` | `text` | 追加进当前流式气泡（首块时创建） |
| S→C | `event` `agent_thought` | `text` | 追加进当前活动块的思考缓冲 |
| S→C | `event` `tool_call` / `tool_call_update` | `toolId,title,status,kind,input,output` | 按 `toolId` upsert 工具卡；缺 `toolId` 时合成 `anon-<ts>` |
| S→C | `event` `plan` | `text` | 活动块内一行（"N steps"） |
| S→C | `event` `command_result` | `text` | 助手气泡（`/help` 之类靠它） |
| S→C | `event` 未知类型 | `text`/`type` | 降级成系统行，**不崩** |
| S→C | `permission_request` | `requestId,title,options[{optionId,name,kind?}],ticketId?` | 权限弹层；回 `permission` 帧，且 `ticketId` 非空时同步 `POST /v1/assist-tickets/{id}/resolve`（对齐 `ChatSessionPage.tsx:944-980`） |
| S→C | `permission_resolved` | `requestId` | id 匹配时收起弹层 |
| S→C | `done` | `stopReason?` | 清 busy/流式；重拉 `/messages`；冲 outbox |
| S→C | `error` | `message` | 清 busy/流式；错误条；重拉 `/messages`；**保留连接** |
| S→C | `cleared` | — | 清 outbox 与流式；重拉 `/messages`；**保留聊天记录**，插一条"上下文已清空"分割线 |
| C→S | `prompt` / `cancel` / `auto` / `permission` | — | 发送 / 停止 / 开关 / 弹层按钮 |
| C→S | `command` | `name,args` | 首期不做 UI，但协议类型保留（follow-on 接 slash 菜单） |

**重连语义**：`status` 快照带 `busy`/`reply`/`thought`/`perm`，但**不带工具卡**；
工具行是流式落库的（`runner.go:193-202` 的 `UpsertToolMessage`），所以重连后除 `status` 外还要
`GET /v1/agent-sessions/{id}/messages` 合并（按 id 去重）——这一点比 web 客户端强（web 只在 `done` 时重拉）。
`agent_message` 正文只以 `status.reply` 为准做**替换**，避免重连后重复半句。

**退避**：`0, 1s, 2s, 4s, 8s` 封顶（照 `web/src/lib/sessionWsUi.ts:82-85`）；连上即重置。
web 里那些给 React StrictMode 双挂载与 Vite 代理擦屁股的补丁（`detachSocket`、100ms 探活、
8 秒连接超时自愈）**不移植**。后台恢复用 Flutter 的 `WidgetsBindingObserver.didChangeAppLifecycleState`：
回到 `resumed` 且 socket 未连接时立即以 `attempt=0` 重连。

**outbox**：移植 `web/src/lib/sessionOutbox.ts` 的语义（未连接时排队、`busy:false`/`done` 后冲刷）。

### 6. 界面与渲染（首期）

移植范围（参照 `web/src/lib/semiChatAdapter.ts:192-232` 的"思考+工具收成一个活动块"这个关键思路）：

- 聊天气泡（user / assistant，markdown）、流式助手气泡（独立 widget）、
  每轮次一个**活动块**（折叠的思考 + 工具卡列表，`tool_stats` 用 Dart 重写、测试用例照搬 `web/src/lib/toolStats.test.ts`）；
- 工具卡：一行摘要（`title · key=value`）+ 可展开的 Input/Output JSON（对齐 `web/src/components/ToolCallCard.tsx`）；
- 权限弹层（`showModalBottomSheet`）、错误条、连接状态指示、输入框（发送/停止）；
- 会话列表页：新建 / 重命名 / 删除（web 是双击改名 + 标签页，移动端收进行内操作菜单）；
- 聊天页头部保留 **Auto 开关**（`ChatSessionPage.tsx:856-868`）：一个布尔量直接决定权限弹层出不出，成本极低。

**丢弃**（桌面专属）：xterm 终端、浏览器实时视图/截图/takeover、工作区文件、`/issues`、设置与管理、slash 命令菜单、附件与 @提及（web 本身也是关掉的）。

**流式性能**（web 没有、移动端必须处理）：逐块 `setState` 在设备上就是逐 token 重建。
做法：chunk 先写缓冲区，按 50–80ms 定时 flush；流式正文用 **`ValueNotifier<String>` + `ValueListenableBuilder`**，
只重建流式气泡本身；已定稿的消息 widget `const`/`RepaintBoundary` 隔离。
长会话（`/messages` 上限 2000 行）先只渲染尾部，翻页留 follow-on。

### 7. 目录结构与共存

```
mobile/
├── pubspec.yaml                  # 包名 roundpen_console；记录解析到的 Flutter/Dart SDK 版本
├── analysis_options.yaml         # flutter_lints
├── lib/
│   ├── main.dart                 # MaterialApp.router 装配
│   ├── routes.dart               # go_router：/splash、/login、/settings、/assistants、/assistants/:id、/assistants/:id/sessions/:sid
│   ├── api/        client.dart | types.dart | endpoints.dart
│   ├── ws/         frames.dart | session_socket.dart | outbox.dart | backoff.dart
│   ├── chat/       chat_items.dart | chat_reducer.dart | tool_stats.dart（纯逻辑，不依赖 widget）
│   ├── session/    storage.dart（flutter_secure_storage）| session_controller.dart（ChangeNotifier + 401 钩子）
│   ├── pages/      login_page.dart | assistants_page.dart | sessions_page.dart | chat_page.dart | settings_page.dart
│   └── widgets/    message_bubble.dart | activity_block.dart | tool_card.dart | permission_sheet.dart
│                   composer.dart | connection_chip.dart | markdown_view.dart | async_list.dart
├── test/                         # 纯 Dart / widget 单测（与 lib 同构）
├── android/                      # 提交（Flutter 约定）；AndroidManifest 配 usesCleartextTraffic
├── ios/                          # 提交；Info.plist 配 ATS 例外 + NSLocalNetworkUsageDescription
└── assets/
```

- **无 monorepo 工具**：`mobile/` 是独立 Flutter 工程（pub 管理依赖），与 `web/`（npm）并列，互不引用。
- Flutter 平台目录**提交进仓库**（Flutter 常规做法，且 cleartext/ATS 配置就在里面）；
  `flutter create` 生成的 `mobile/.gitignore` 覆盖 `.dart_tool/`、`build/`、`ios/Pods/` 等构建产物，根 `.gitignore` 无需改动。
- `.dockerignore` 追加 `/mobile/.dart_tool`、`/mobile/build`、`/mobile/ios/Pods`（保持 compose 构建上下文小）。
- `Makefile` 追加 `mobile-setup`（`flutter pub get`）、`mobile-dev`（`flutter run`）、
  `mobile-test`（`flutter test`）、`mobile-analyze`（`flutter analyze`）；
  **不接进** `build`、`test`——控制面二进制保持零 Node/Dart 依赖（`build-ui` 的注释已经写明这条）。
- 原生配置（漏了就白屏/连不上，必须一次配齐）：
  - Android 9+ 禁明文 → `android/app/src/main/AndroidManifest.xml` 的 `android:usesCleartextTraffic="true"`；
  - iOS ATS 禁 http → `ios/Runner/Info.plist` 的 `NSAppTransportSecurity.NSAllowsArbitraryLoads: true`
    （地址由用户输入，无法预声明域名），**外加** `NSLocalNetworkUsageDescription`
    （iOS 14+ 访问局域网地址会弹本地网络权限，缺这个串会静默失败）；
  - `applicationId` / bundle id 现在就定，避免将来加推送/深链要换身份。
  - 自签 HTTPS 会被 `dart:io` 拒绝：登录页与 README 写明"局域网用 http，公网用受信证书"。
- 运行/分发：开发 `flutter run`；自托管用户可 `flutter build apk` 侧载（不依赖任何云构建服务）。

### 8. 测试

- **单测（`flutter test`）**：
  - `chat_reducer_test.dart` 是重头：分块追加、快照**替换而非追加**、流式中途快照、工具按 `toolId` upsert、
    `permission_request` → 弹层、`permission_resolved` id 匹配/不匹配、`done` 清流式、`error` 保记录、
    `cleared` 保记录、未知事件降级；
  - `session_socket_test.dart`（注入假 WebSocket/假 client）：连上补发 `auto`、退避序列、连接失败只重试不冒泡、
    outbox 只在 `busy:false`/`done` 后冲刷、headers 每次重连都带；
  - `frames_test.dart`：出入站帧解析/序列化往返；
  - `client_test.dart`：401 钩子只触发一次、错误体 → `ApiError`、地址归一化；
  - `tool_stats_test.dart`：移植 `web/src/lib/toolStats.test.ts` 的用例（Dart 重写，用例逐条对应）。
- **Go 侧**（`internal/api/auth`，已实施并全绿）：
  - 登录 5 条：`returnSessionToken` 时返回 64 hex 且可用于 Bearer、响应体不含明文 API Key；
    不带该字段时**没有** `sessionToken` 字段；口令错误绝不返回 token；限流不受影响；
    登出后同一 token 立即 401（`TestLogout_RevokesBearerSession`）。
  - 中间件 5 条：Bearer 会话 token 通过、过期 token 拒绝、`X-API-Key` **不**接受会话 token、
    未知 token 拒绝、Bearer API Key 仍照常工作；外加真实 WS 升级集成测试
    `TestMiddleware_WSUpgradeWithBearerSessionToken`。
  - 中间件签名未变，`tests/uismoke/main.go` 不需要动。
- **设备验收**（写进 plan 的验收清单）：`make dev` 后从手机浏览器确认 `http://<IP>:19001/health` 通，
  再跑 App；Android 模拟器用 `10.0.2.2`、iOS 模拟器用 `127.0.0.1`、真机用局域网 IP。
  必测：登录 → 助手列表 → 开聊 → 流式 → 工具卡 → 关 Auto 触发权限弹层 → **中途切后台再回来**
  （回复不重复、工具行不丢）→ 杀服务端再重启（自动重连 + 快照对齐）。
- **端到端（Maestro）推迟**：v1 不做，最后阶段再决策。Maestro 支持 Flutter，配合 `tests/uismoke` 做桩冒烟最省。
  **iOS 真机验收是必做项，不是可选项**（本机无法代替）。

## 非目标（首期不做）

终端、浏览器实时视图与截图、工作区文件浏览器、`/issues` 议题、模板/沙箱/管理设置、
推送通知、离线模式、slash 命令菜单（协议类型先保留）、平板专门布局、
**Flutter Web**（浏览器不允许 WS 握手自定义 header，鉴权必失败）、
多语言层（v1 文案直接中文，follow-on 复用 `web/src/i18n` 的结构补 zh_CN/en）、CI 集成。
