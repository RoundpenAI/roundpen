# 移动端控制台（Flutter）Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: 用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务执行。步骤用 `- [ ]` 复选框跟踪。

**Goal:** 新增一个 Flutter 移动端 App，首期打通对话核心：登录（换会话 token）→ 助手列表 → 会话列表（新建/重命名/删除）→ 聊天（WS 流式、工具卡、权限确认、断线重连回放）。服务端改动两处（**均已实施**）：登录可格外返回本次会话 token、Bearer 接受会话 token。

**Architecture:** App 以 `Authorization: Bearer <sessionToken>` 访问既有 `/v1` REST + `GET /v1/agent-sessions/{id}/ws`。WS 用 `dart:io` 的 `WebSocket.connect(url, headers: {...})`——原生平台支持握手自定义 header（RN 的 iOS 实现不支持，这正是选 Flutter 的实证理由），因此不需要任何 ticket 机制；中间件包住整个 mux（`cmd/roundpend/main.go:449`）。**凭据是会话 token 不是 API Key**——数据库只存 API Key 的哈希（`internal/storage/user.go:32`），登录拿不到明文；会话 token 每设备一条、可吊销、7 天滑动。客户端把 wsIn/wsOut 帧喂给纯 Dart reducer，产出"每轮次 = 一个活动块（思考 + 工具卡）+ 助手气泡"；重连时先按 `status` 快照**替换**流式正文，再拉 `/messages` 合并工具行（工具行是流式落库的，能补回来）。

**Tech Stack:** **Flutter 3.47.5 / Dart 3.13.4**（flutter-team snap，classic）；go_router 18、package:http 1.6、flutter_secure_storage 11、shared_preferences 2.5、flutter_markdown_plus 1.0（封装在自有 `MarkdownBody` 后）；`flutter test` + `flutter_lints 6`；`flutter analyze` 即 lint。服务端 Go 1.26（`net/http`、gorilla/websocket）。

**Spec:** `docs/superpowers/specs/2026-09-20-mobile-console-design.md`

**分支：** 建议 `feat/mobile-console`。

---

## File map

| Path | Responsibility |
|------|----------------|
| `internal/api/auth/session_handler.go` | `loginRequest` 加 `returnSessionToken`；`issueSession` 返回明文；`Logout` 按 context 里的 sessionID 吊销（回退 Cookie） |
| `internal/api/auth/middleware.go` | `GetSessionID`；`resolveAuth` 的 Bearer 分支接受非 `rp-` 前缀的会话 token |
| `internal/api/auth/session_handler_test.go` | 5 条登录/登出测试（返回 token、默认不带、口令错、限流、登出即吊销） |
| `internal/api/auth/middleware_test.go` | 5 条中间件测试 + 真实 WS 升级集成测试 |
| `docs/auth.md` | 登录响应字段 + 「移动端登录」小节（为什么不用 API Key） |
| `mobile/pubspec.yaml`、`analysis_options.yaml` | Flutter 工程配置（包名 `roundpen_console`、依赖、SDK 约束） |
| `mobile/lib/main.dart`、`routes.dart` | MaterialApp.router + SessionScope；go_router 路由 |
| `mobile/lib/api/client.dart` | 绝对 baseUrl、Bearer 注入、`ApiError`、401 钩子、超时 |
| `mobile/lib/api/types.dart`、`endpoints.dart` | 类型 + auth/assistants/agents 命名空间 |
| `mobile/lib/session/storage.dart`、`session_controller.dart` | flutter_secure_storage 存取；ChangeNotifier + 未授权处理 |
| `mobile/lib/ws/frames.dart` | wsIn/wsOut 联合类型（唯一契约源）+ 往返解析 |
| `mobile/lib/ws/session_socket.dart` | 握手 header 鉴权、退避重连、生命周期恢复、outbox（可注入传输层便于测试） |
| `mobile/lib/ws/outbox.dart`、`backoff.dart` | 移植 web 语义（`sessionOutbox.ts` / `sessionWsUi.ts:82-85`） |
| `mobile/lib/chat/chat_items.dart` | 渲染模型：用户/助手气泡、每轮次一个活动块、分割线、系统行 |
| `mobile/lib/chat/chat_reducer.dart` | 纯逻辑：帧 + 历史 → ChatState（工具 upsert、快照替换、分组） |
| `mobile/lib/chat/tool_stats.dart` | Dart 重写 `web/src/lib/toolStats.ts`（测试用例照搬） |
| `mobile/lib/widgets/*.dart` | MessageBubble / ActivityBlock / ToolCard / MarkdownView / PermissionSheet / Composer / ConnectionChip / AsyncList |
| `mobile/lib/pages/chat_page.dart`、`settings_page.dart` | 聊天页（WS + 流式 + Auto + 权限弹层）、设置页 |
| `mobile/android/app/src/main/AndroidManifest.xml`、`mobile/ios/Runner/Info.plist` | cleartext / ATS / 本地网络权限 |
| `Makefile`、`.dockerignore`、`README.md`、`docs/architecture/project-layout.md` | `mobile-*` 目标、构建上下文、文档共存说明 |

---

## Phase 1 — 服务端：登录返回会话 token、Bearer 接受会话 token（已实施）

> 动手前核实发现原设计（"登录返回 API Key"）不可行：数据库只存 API Key 的哈希，
> 明文只在生成/轮换时出现一次（见 spec §1）。改用每设备一条、可吊销、7 天滑动的**会话 token**。

### Task 1: 登录返回会话 token（已实施）

- [x] `internal/api/auth/session_handler_test.go` 先写 5 条：
  `TestLogin_ReturnsSessionTokenWhenRequested`（64 hex 且能直接当 Bearer 用、响应体不含明文 API Key）、
  `TestLogin_OmitsSessionTokenByDefault`、`TestLogin_FailedPasswordNeverReturnsSessionToken`、
  `TestLogin_RateLimitUnaffectedByReturnSessionToken`、`TestLogout_RevokesBearerSession`。
- [x] `session_handler.go`：`loginRequest` 加 `ReturnSessionToken bool`；`issueSession` 改签名返回明文
  （三个调用点同步）；`Login` 按需把 `sessionToken` 作为 `user` 的兄弟字段返回；
  仅在真签发时记一行 `slog.Info("auth.login", …)`（不记 token）。
- [x] `Logout` 改为优先用 context 里的 sessionID 吊销（回退 Cookie 查找）；新增 `GetSessionID`。
- [x] `docs/auth.md`：登录行、登出行、「移动端登录」小节（含为什么不用 API Key）。

### Task 2: Bearer 接受会话 token（已实施）

- [x] `internal/api/auth/middleware_test.go` 先写 5 条：Bearer 会话 token 通过 / 过期拒绝 /
  `X-API-Key` 不接受会话 token / 未知 token 拒绝 / Bearer API Key 仍照常工作。
- [x] `middleware.go`：`resolveAuth` 在 API Key 路径之外加一条分支——`Authorization: Bearer` 的
  值不以 `rp-` 开头时按会话 token 处理（`GetByTokenHash` → `Touch` → 返回 `(user, user.APIKey, sess.ID)`）。
- [x] 真实 WS 升级集成测试 `TestMiddleware_WSUpgradeWithBearerSessionToken`（httptest 起真实服务器 +
  gorilla 客户端）：无凭据 401、带 Bearer 会话 token 升级成功——这一条替代了原计划里的一次性 curl 验证，
  且可以一直跑。
- [x] 验证：`go test ./internal/api/auth/ ./internal/api/... ` 与 `go vet ./...` 全绿；
  中间件签名未变，`tests/uismoke/main.go` 无需改动。

## Phase 2 — Flutter 工程脚手架 + 登录 + 助手列表

### Task 2: 工程与共存

- [x] `flutter create mobile --org ai.roundpen --project-name roundpen_console --platforms=android,ios`
  （只生成两端，不生成 web——WS 鉴权在浏览器不可用）；`flutter pub add go_router http flutter_secure_storage shared_preferences flutter_markdown_plus`；
  版本已记入本文件 Tech Stack（Flutter 3.47.5 / Dart 3.13.4）。
- [x] 平台配置：`AndroidManifest.xml` 加 `usesCleartextTraffic` 与 INTERNET 权限；
  `Info.plist` 加 `NSAppTransportSecurity.NSAllowsArbitraryLoads` 与 `NSLocalNetworkUsageDescription`；
  `applicationId` / bundle id 用脚手架生成的 `ai.roundpen.roundpen_console`（改它要动 Kotlin 包目录，不值当）。
- [x] 仓库共处：`.dockerignore` 追加 `mobile/.dart_tool`、`mobile/build`、`mobile/ios/Pods`；
  `Makefile` 加 `mobile-setup` / `mobile-dev` / `mobile-test` / `mobile-analyze`（**不接**进 `build`/`test`）；
  `README.md` 产品模型表加"Mobile 槽位 ≠ 移动端控制台"提示、本地开发节补命令与
  "**不要用 `flutter run -d chrome` 调试**"；`docs/architecture/project-layout.md` 树里加
  `mobile/  # 移动端控制台（Flutter），非 Mobile 槽位`。
- [x] 验证：`flutter analyze` 无问题、`flutter test` 全绿。

### Task 3: 会话与 API 层

- [x] `lib/api/client.dart`：`normalizeBaseUrl`（`host:port` → `http://host:port`）、`configure({baseUrl, token})`、
  每次请求新建 `http.Client` 并注入 `Authorization: Bearer`、`ApiError{status,message}`、
  `onUnauthorized` 钩子（401 只触发一次）、超时/连接失败/解析失败都映射成 `ApiError`；
  `test/client_test.dart` 用 `MockClient` 覆盖 6 类行为（Bearer 注入、解码、401 一次性、错误体映射、
  请求体序列化、空响应）。
- [x] `lib/session/storage.dart`（flutter_secure_storage 存 token，shared_preferences 存地址）+
  `session_controller.dart`（`ChangeNotifier`：`{baseUrl, token, user}` + `restore/signIn/signOut/handleUnauthorized`）。
- [x] `lib/api/endpoints.dart`：`login(user,password)` 带 `returnSessionToken: true` 并取回 `sessionToken`、
  `me`、`logout`、`assistants`、`sessions`、`ensureSession`。

### Task 4: 登录页与助手列表

- [x] `lib/routes.dart`（go_router，`/splash` 等待 `restore()`、登录态重定向）+ `lib/main.dart` 装配；
  `lib/pages/login_page.dart`：服务器地址 + 用户名 + 密码，空值本地校验，错误按 `ApiError.message` 展示，
  页脚提示局域网 http / 公网受信证书。
- [x] `lib/pages/assistants_page.dart`：助手列表（空态、错误态带重试、下拉刷新、退出登录入口）；
  `test/login_page_test.dart` 覆盖渲染与空值校验（不发请求）。
- [ ] **设备验证**（需 Flutter 环境 + 模拟器/真机 + 起 `make dev`）：登录 admin → 看到助手列表。
  本机无法跑模拟器，留待有设备时执行。

## Phase 3 — 会话列表（新建/重命名/删除）

### Task 5

- [x] `lib/pages/sessions_page.dart`：`/assistants/:id` 会话列表——`GET /v1/agent-sessions` 后
  按 `assistantId` 客户端过滤（服务端只回最近 50 条且不分助手，**已知限制写进了注释**）；
  新建（`POST {title,assistantId}` 后直接进会话）、重命名（对话框 + 非空校验 + `PATCH`）、
  删除（二次确认 + `DELETE`）、下拉刷新。
- [x] 打开会话：助手列表右侧按钮走 `ensureSession` 直接进当前会话（对齐 web 的
  `AssistantChatRedirect.tsx:17`）；会话行的点击进对应会话。
- [x] 抽出 `lib/widgets/async_list.dart`（FutureBuilder + 下拉刷新 + 空态/错误重试），
  assistants 与 sessions 两页共用，行为一致。
- [x] `lib/pages/chat_page.dart`：会话壳（取会话标题 + 明确标注下一步接入流式聊天），
  路由 `/assistants/:assistantId/sessions/:sessionId` 就位。
- [x] `test/sessions_page_test.dart`：只列本助手的会话 / 空态 / 错误可重试三条。
- [x] **顺带修掉一个真 bug**：控制面返回 `application/json` 不带 charset，Dart 的 `package:http`
  会按 latin1 解码，中文全乱。`ApiClient` 改为自行 `utf8.decode(res.bodyBytes)`，
  并用不写 charset 的 mock 响应（与真实服务端一致）加了回归测试。
- [ ] **设备验证**（同 Task 4，留待有设备时）：手工 CRUD 一遍。

## Phase 4 — 聊天核心（流式 + 活动块 + 工具卡）

### Task 6: 协议与 reducer（已实施）

- [x] `lib/ws/frames.dart`：服务端帧 sealed 联合 + 客户端帧构造函数；
  `test/frames_test.dart` 覆盖全部帧类型、缺失字段、坏 JSON 与未知类型降级、客户端帧序列化。
- [x] `test/chat_reducer_test.dart`（18 条）：分块追加；快照**替换**流式正文（不叠加）；
  流式中途空快照清空；思考块合并与快照替换；工具按 `toolId` upsert + 部分字段合并 + 无 id 兜底；
  权限请求/命中与不命中的 resolved；无 perm 的快照收起陈旧弹层；`done` 清流式并回报 settled；
  错误保留记录；`cleared` 保留记录 + 分割线；未知事件降级；历史分组（思考+工具 → 一个活动块）、
  命令 display、clear 标记、以及"历史落库后丢弃重复的实时工具行"。
- [x] `lib/chat/chat_reducer.dart` + `lib/chat/chat_items.dart`：`ChatState` + `itemsFromRows`，
  分组规则对齐 `semiChatAdapter.ts`。（文件位置与原计划的 `lib/ws/chat_reducer.dart`、
  `lib/lib/tool_stats.dart` 不同——按职责归到 `lib/chat/`，file map 已同步。）
- [x] `lib/chat/tool_stats.dart`：Dart 重写 `web/src/lib/toolStats.ts`（输出文案保持英文，两个客户端读一致）；
  `test/tool_stats_test.dart` 逐条对应原用例，全绿。

### Task 7: socket 与聊天页（已实施）

- [x] `lib/ws/session_socket.dart`：`WebSocket.connect(url, headers: {'Authorization': 'Bearer …'})`；
  连上补发 `{type:'auto',enabled}`；退避 0/1/2/4/8s 封顶；`resume()`（回前台立即重连，attempt 归零）；
  outbox（离线排队 / `flushOutbox` / `clearOutbox`）。传输层与 delay 都可注入，
  `test/session_socket_test.dart` 用假连接覆盖：鉴权头每次重连都带、退避序列与封顶、连接失败只重试、
  dispose 停止循环、resume 立即重试、outbox 冲刷顺序与时机。
- [x] `lib/widgets/*`：MessageBubble、ActivityBlock（含思考与工具卡）、ToolCard（摘要行 + 可展开 Input/Output）、
  MarkdownView（唯一知道 markdown 包的地方）、Composer（发送/停止）、ConnectionChip、AsyncList。
- [x] `lib/pages/chat_page.dart`：挂载顺序对齐 web（先 `GET /messages`，再开 WS）；Auto 开关；
  `done`/`busy:false` 后重拉消息；**重连即重拉 `/messages`**（`onConnected`），按 `toolId` 去重——
  这点比 web 客户端强，mid-turn 重连不丢工具行。
- [x] 验证：`flutter analyze` 无问题、`flutter test` 全绿。

**有意偏离原计划的点**（记录以免误以为漏做）：

- **没做 50–80ms 流式节流 / `ValueNotifier` 单独驱动**：Flutter 的 `ListView.builder` 只构建可见项，
  逐块 `notifyListeners` 的重建量远小于 web 的一次整表重渲。真机上若观察到卡顿再引入，
  届时改动局限在 `chat_page.dart` 与 `MessageBubble`。
- **没有单独的 ThoughtBlock widget**：思考渲染收进 ActivityBlock 的展开区，少一层折叠。

## Phase 5 — 权限弹层、重连回放、错误与清空（已实施）

### Task 8

- [x] `lib/widgets/permission_sheet.dart`：`showModalBottomSheet` 展示 `title` + `options`
  （reject 类走描边按钮，其余实心）；选择后发 `{type:'permission',requestId,optionId}`，
  `ticketId` 非空时并发 `POST /v1/assist-tickets/{id}/resolve`（`resolutionFor()` 对齐
  `ChatSessionPage.tsx:944-980` 的 reject → `reject`、其余 `allow_once`）。
- [x] reducer/socket 补齐：`permission_request`/`permission_resolved`（id 匹配才收起）、
  `permission_resolved` 到达时同时关闭已弹出的弹层、`error`（保留连接 + 横幅）、
  `cleared`（分割线 + 清 outbox）、outbox 冲刷时机（`busy:false` / `done`）。
- [ ] **手工验收（待设备）**：关 Auto 触发权限弹层并作答；一轮进行中切后台再回前台
  （回复不重复、工具行补回来）；杀服务端 → "重连中" → 重启 → 自动重连并快照对齐。

## Phase 6 — 设置页、文档、可选冒烟

### Task 9

- [x] `lib/pages/settings_page.dart` + `/settings` 路由（助手页右上角入口）：服务器地址、账号与角色、
  退出登录（`/v1/auth/logout` 服务端吊销 + 清 SecureStore）。**不放 API Key 轮换**——它只影响
  CLI/自动化，与 App 的会话无关（spec §3 已说明）；改地址 = 退出后在登录页改（登录页会预填上次地址）。
- [x] 空态/错误态：列表页共用 `AsyncList`（空态、错误可重试、下拉刷新），聊天页空态与错误横幅。
  长会话"只渲尾部 + 翻页"**未做**——`ListView.builder` 已经是懒构建，真出现长历史卡顿再补（follow-on）。
- [x] 文档：`README.md` 产品模型表下补"Mobile 槽位 ≠ 移动端控制台"、本地开发节补 `make mobile-*` 与
  "别用 `-d chrome` 调试"提示、路线图补"移动端"一条；`docs/architecture/project-layout.md` 目录树补 `mobile/`。
- [ ] 可选 Maestro/集成冒烟：**未做**，记为 follow-on（spec 已说明 v1 不强制）。

---

## 验收清单（实施完成后逐条跑）

> 已跑通的在下面勾掉；**需要设备的四项留在最后**，本机没有 Android SDK/模拟器。

- [x] `go build ./... && go vet ./...`
- [x] `go test ./internal/api/auth/ ./internal/api/agentapi/`（另加 `go test ./internal/...` 全绿）
- [x] `cd mobile && flutter analyze && flutter test`（54 个用例全过，analyze 无问题）
- [ ] 手工（`make dev` + `cd mobile && flutter run`，**不要用 `-d chrome`**）：
  - [ ] 从手机浏览器确认 `http://<局域网IP>:19001/health` 可达（先排除网络问题再怀疑 App）；
  - [ ] 登录 admin → 助手列表 → 开会话 → 发一条会触发工具调用的指令 → 流式 + 工具卡正常；
  - [ ] 关 Auto → 触发权限弹层 → 作答 → 轮次继续且不重复弹；
  - [ ] 一轮中途切后台再回前台：回复不重复、工具行不丢、轮次正常收尾；
  - [ ] 杀服务端 → "重连中" → 重启 → 自动重连并快照对齐；
  - [ ] 设置页退出登录 → 该 token 立即失效（用 curl 验 401）；重新登录后 App 恢复正常。
- [ ] **iOS 真机验收**（模拟器不能替代）：登录 + 一轮流式对话 + 权限弹层。
