# 设计：Slash 命令（技能命令 + 内置动作）

> 日期：2026-09-20
> 状态：已实施（2026-09-20）
> 范围：`internal/api/agentapi`（协议与执行）/ `internal/commands`（新包）/ `internal/acp/sysagent/tools`（技能复用）/ `internal/agentsession`（标记与投影截断）/ `web/src`（菜单与发送）/ `tests/uismoke`、`web/e2e`

## 背景与目标

Roundpen 的技能（Skill）系统已经成型：`commit` / `review` / `fix` / `summarize` 四个内置技能，
用户还能把自定义技能安装到 agent 容器 `~/.roundpen/skills/<name>.md`（规范见 `docs/skills.md`）。
但技能**只对模型可见**——模型通过 `Skill` 工具调用，用户没有任何直接入口。

本轮补上用户侧入口：输入框打 `/` 弹出命令菜单（复用 Semi `AIChatInput` 自带的 skill 弹层），
选中技能即把技能指令注入为用户轮次；同时补两个动作型命令：

- `/clear`：清空当前会话的上下文（模型从零开始；聊天记录保留，界面多一条分割线）。
- `/help`：不调模型，直接在会话里列出全部可用命令。

目标：用户不依赖模型"自觉"就能复用沉淀好的工作流，并且能一键重置跑偏的长会话。

## 需求结论（澄清结果）

1. **范围**：技能命令（内置 + 已安装）与动作命令 `/clear`、`/help`。`/compact`（模型摘要压缩历史）
   本期不做（见非目标）。
2. **命令解析位置**：控制面（`agentapi`）。前端只负责"选中了什么命令"，展开成指令文本的事
   完全在后端做——这样 stdio provider 也一样能用技能命令，且技能正文不需要下发到浏览器。
3. **`/clear` 语义（2026-09-20 评审修订）**：不删任何行。控制面追加一条**标记行**
   （`role=event`、`meta.type="clear"`），模型上下文从最后一条标记**之后**开始；
   **聊天记录完整保留**，界面在标记处渲一条分割线。初版设计是删除消息行（对齐 Claude Code），
   评审时改为标记方案：误触可恢复、历史可回看，而且不需要 migration——因为模型上下文的唯一
   投影入口是 `projectHistory`（`history.go:75-147`，sysagent 与 stdio 的 `RestorePreamble`
   都走它），在那里按标记截断即可。
4. **未匹配的 `/foo`**：按普通文本发送给模型（例如用户想抱怨 `/etc/hosts`）。只有能匹配到命令
   目录的输入才走命令路径。
5. **持久化语义**：技能命令的 user 行 `content` 存**展开后的指令文本**（否则后续轮次的历史回放
   只剩 `/review` 三个字，技能指令丢失），原始输入存在 `meta.display` 里供 UI 显示。

## 方案选择

**方案 A（采用）：控制面解析 + 纯文本注入。** 前端发 `{type:"command", name, args}`，后端解析成
指令文本后走和普通 prompt 完全相同的轮次路径（`acp.Prompt`）。

- 选它的原因：`projectHistory`（`internal/acp/sysagent/history.go:119-123`）把 user 行**原样**回放，
  技能指令必须是 user 行正文的一部分才能进入后续每一轮的上下文；stdio provider 通过
  `restorePreamble`（`internal/acp/manager/manager.go:361-370`）做同样的事。展开放在控制面，
  两种 provider 自动一致。
- 方案 B（前端展开技能正文）被否：技能正文要下发到浏览器，且 stdio/前端各自维护一份展开逻辑。
- 方案 C（新增 `Skill` 工具的"用户预授权调用"）被否：那还是模型驱动，用户只是提前投票，绕。

**动作命令的实现位置**：`internal/commands` 新包（纯函数 + 静态表），不塞进 `agentapi`
（`handler.go` 已 500 行，且命令表需要独立单测，不该依赖 DB/WS）。

## 设计

### 1. WS 协议

入站新增一帧（`internal/api/agentapi/handler.go:339` 的 `wsIn` 加 `Name`/`Args`）：

```json
{ "type": "command", "name": "review", "args": "关注并发安全" }
```

`sessionWS` 的 switch（`handler.go:484-493`）加一个 case 转给 `runner.command(name, args)`。
出站新增一个裸帧 `{"type":"cleared"}`（`wsOut` 结构不变），通知所有已连接客户端"上下文边界已变"，
客户端据此重新拉取消息以渲染分割线。

前端只在能匹配命令目录时才发 command 帧；否则仍发 `{type:"prompt", text}`。这样旧客户端
（只会发 prompt）行为完全不变，协议是向后兼容的加法。

### 2. 命令目录接口

`GET /v1/agent-sessions/{id}/commands`（鉴权与 `listMessages` 一致）：

```json
{ "commands": [
  { "name": "clear",  "kind": "action", "source": "action",    "description": "清空本会话上下文", "args": false },
  { "name": "review", "kind": "skill",  "source": "builtin",   "description": "审查最近改动…",   "args": false },
  { "name": "my-flow","kind": "skill",  "source": "installed", "description": "…",              "args": true }
] }
```

- `kind ∈ {skill, action}`，`source ∈ {builtin, installed, action}`。
- 内置技能与动作命令是编译期的，永远返回；内置技能描述在 `internal/commands` 里有一份**中文覆盖表**
  （只影响展示，模型可见文案不动——内置技能描述同时出现在 `history.go` 的系统提示与 `skillList`
  输出里，不能为 UI 改）。
- **已安装技能是尽力而为**：技能文件在 agent 容器 `~/.roundpen/skills` 里，读取需要容器 exec；
  只有 `h.Envs.List(ctx, sess.UserID)` 显示 agent slot `running` 时才去列（5s 超时），否则省略。
  绝不因为"列技能"而拉起容器。actor 恒用 `sess.UserID`（管理员看别人的会话时也对）。
- 拉取时机：会话页挂载时 + 每轮 `done` 之后（技能可能刚被安装）。失败退化为空列表，
  输入框照常当普通文本用。

### 3. 执行语义（`internal/api/agentapi/command.go` 新文件）

- **技能命令** `runner.command("review", args)`：
  1. 解析技能：已安装优先，回退内置（与 `Skill` 工具的 `skillInvoke` 完全同序，`skill.go:452-463`）。
  2. 用 `formatSkillInvocation`（从 `skillInvoke` 抽出的格式化函数，`skill.go:464-472`）得到展开文本
     —— 与今天模型调 `Skill(action=invoke, skill=review, args=…)` 拿到的字符串**逐字节一致**。
  3. 落 user 行：`content = 展开文本`，`meta = {"type":"user","command":"review","commandArgs":args,"display":"/review 关注并发安全"}`。
  4. 走 `prompt` 同款开轮次路径（busy 时排队）。
- **`/help`**：不调模型。落 `role=event` 行（`meta.type` 保持 `"event"`，这样前端
  `semiChatAdapter.ts:140-148` 现有的 event 分支自动渲染成系统气泡，adapter 零改动）+
  广播 event 帧 + 该帧同时推给其它标签页。
- **`/clear`**：见下节。
- **未知命令**：后端收到 `command` 帧但解析不出技能 → 广播 `event/command_error` 帧（前端按系统气泡
  显示），不落库、不开轮次。不用 `error` 帧是因为那会把连接状态切成错误态，而"命令不存在"
  不是连接问题。（前端理论上不会发未知命令，属于防御。）

### 4. `/clear` 状态机

`/clear` 是唯一有副作用的命令，必须处理三件事：在途回合、未决权限对话框、stdio provider 的
子进程状态。

1. `runner` 增加 `actor manager.Actor`（`runnerFor` 里已经构造了，`runner.go:639-643`，存下来即可）
   与 `clearPending bool`。
2. 收到 clear：若 `busy` → 置 `clearPending=true`、清空 `pending` 队列、`acp.Cancel()`；
   若空闲 → 直接走 reset。
3. `finishTurn`（`runner.go:272-287`）：发现 `clearPending` → 清标志、保持 `busy=true`、
   `go r.reset()`。保持 busy 是为了锁住输入框、并且让其它标签页此刻发来的 prompt 进 `pending` 排队。
4. `reset()`：
   1. 落**标记行**：`role=event`、`meta={"type":"clear"}`（用现有 `AddMessage`，不删任何旧行）；
   2. 广播 `{"type":"cleared"}`（各客户端 refetch，界面出现分割线，旧记录都还在）；
   3. `r.acp.Stop(sessionID)` + `r.acp.Start(ctx, sess.ID, sess.SandboxID, sess.ProviderID, manager.StartOpts{Actor: r.actor})`
      + 重挂 `SetEventHandler` / `SetPermissionHandler` / `SetAutoMode`，替换 `r.rt`。
      **stdio 必须重启**（子进程自己记着对话，DB 标记它看不见）；**sysadmin 也重启**：顺手清掉
      sysagent 内存里的 `planMode` / `allowTools` 等每会话状态（`sysagent/agent.go:523-533`），
      标记截断管不到它们。stdio 重启最坏 1-2 分钟（QEMU + ACP 适配器），全程 composer 保持 busy。
   4. 释放 `busy=false`，广播 snapshot，drain `pending`。
5. **模型上下文截断**：`projectHistory`（`history.go:75`）开头把 `rows` 截到
   `AfterLastClear(rows)` 之后。sysagent 的 `buildPromptMessages` 与 stdio 的 `RestorePreamble`
   共用这个投影，两条路径自动一致；标记行本身不会被回放（非 `error` 的 event 行本来就被跳过）。
   `runner.recentDigest`（classifier 上下文，取最新 200 行）用同一个 helper —— 若标记不在窗口内，
   窗口内的行本来就都在标记之后，截断是幂等的。
6. **未决权限对话框**：`onPermission`（`runner.go:474-510`）在没有客户端应答时会一直循环，
   `/clear` 会因此卡死。给 `permWait` 加 `cancel chan struct{}`，clear 时 close 之，
   select 加一个 `case <-pw.cancel:` 分支走 `cancelPermission`（记 `cancelled` 并回 cancelled 响应）。
7. 历史读者已核对：`assistant/http_activity.go` 的活动流与 `GET /messages` 都是**用户可见视图**，
   保持完整历史（分割线由标记行渲染）；会话标题从不从消息推导。

### 5. 前端（`web/src/pages/ChatSessionPage.tsx`）

- `AIChatInput` 传 `skills={catalog}`（`label` + `description`）、`skillHotKey="/"`、
  `renderSkillItem`（渲染名称 + 描述 + 来源标记）。半屏弹层由 Semi 内部维护：
  只有输入框为空时按 `/` 才打开，方向键 + Enter 选择。
- 新 `web/src/lib/slashCommand.ts`（纯函数 + 单测）：
  - `buildSendPayload(contents, catalog)` → `{kind:'command',name,args}` 或 `{kind:'text',text}`。
  - 修复 `contentsHaveSendableText`（原在 `ChatSessionPage.tsx`，现随纯函数一起搬进
    `slashCommand.ts`）：原来只看 `text` 节点，导致"只选技能不打字"永远不可发送。
  - chip 之后的文本节点拼起来就是 `args`（`/review 关注并发` → args = `关注并发`）。
- 命令帧只在 WS `OPEN` 时发送，**不走离线 outbox**（命令没有"稍后补发"的语义）；
  收到 `cleared` 时清空 outbox 并 refetch（**不清空已渲染的消息**——旧记录仍可见，只是多出一条分割线）。
- 分割线渲染：`semiChatAdapter` 给 `meta.type === "clear"` 的 event 行打上
  `model: CLEAR_DIVIDER_MODEL`（沿用 `ACTIVITY_MODEL` 的既有模式，`semiChatAdapter.ts:4,216-218`），
  `chatDialogueRender.tsx:96-103` 加一个分支渲染 `.chat-clear-divider`（文案"上下文已清空"）。
- `semiChatAdapter.ts:119-127` 的 user 分支改为 `content: m.meta?.display ?? m.content`，
  发送时本地回显也用 `display`，保证气泡显示用户打的原文而不是展开后的长指令。

### 6. 测试与文案

- Go：`internal/commands` 表驱动测试（目录、HelpText、中文覆盖表）；`tools` 的
  `ExpandSkill` golden 测试（输出必须与 `Skill(action=invoke)` 一致）；`agentsession` 的
  `AfterLastClear` 与 `sysagent` 的投影截断测试；`agentapi` 的目录/runner 命令测试
  （需要 DB 的沿用仓库既有"缺 DATABASE_URL 即 skip"的模式）。
- 前端：`node --test --experimental-strip-types src/lib/slashCommand.test.ts`。
- e2e：`tests/uismoke` 加 `/commands` stub；新 `web/e2e/slash-commands.spec.ts` 用
  `page.routeWebSocket` 起假 WS：打 `/` → 选 `review` → 输参数 → Enter → 断言收到
  `{type:"command",name:"review",args:…}`、气泡显示 `/review …`；再推 `{type:"cleared"}` 断言刷新。
- `docs/skills.md` 补一句：内置技能与已安装技能都可以用 `/name` 由用户直接发起。

### 7. 与参考实现（Claude Code）的有意偏离

| 点 | 参考实现 | 本设计 | 原因 |
|----|----------|--------|------|
| 弹层过滤 | 输入 `/rev` 实时过滤 | 不做（只在空输入框按 `/` 打开） | Semi 弹层无增量过滤，要自定义 Tiptap 扩展；非目标 |
| 自定义命令文件 | `.claude/commands/*.md` | 复用技能（`~/.roundpen/skills`） | 技能已有安装/命名/生命周期管理，再做一套职责重叠 |
| `/compact` | 有 | 不做 | 需要控制面接一次 LLM 摘要调用，本轮不铺管道 |
| 命令是否入历史 | 以命令原文进上下文 | user 行存展开文本、`meta.display` 存原文 | 历史回放需要指令正文，UI 需要原文，两者都要 |

### 8. 历史写入路径审计（2026-09-20 确认）

会话历史原则上是**仅追加**的，本轮确认了全部写入路径，两处例外保持现状：

| 路径 | 位置 | 性质 |
|------|------|------|
| `AddMessage`（纯 INSERT） | `store.go:269` | 追加 |
| `UpsertToolMessage`（`UPDATE … WHERE id=$1`） | `store.go:330` | 工具行原地更新：同一行的 status/output 随 `tool_call_update` 事件合并，属于运行时状态而非对话内容 |
| `Store.Delete`（外键级联） | `store.go:235` | 删除整个会话时级联删除其消息；是用户对会话的显式操作，不是对单条历史的篡改 |

没有编辑/删除单条消息的接口（路由只有 `GET /messages` + WS 追加）。`/clear` 采用标记方案后
也不删行，与本原则一致。

## 非目标（本期不做）

- `/compact`（模型摘要压缩历史）与任何自动压缩策略
- 弹层增量过滤、命令别名、自定义快捷键
- 项目级命令文件（`.roundpen/commands/*.md`）
- 命令的权限门（技能展开是纯文本注入，不触发工具权限；`/clear` 是用户显式操作，不再二次确认）
- 离线命令队列（断线时命令不入 outbox）
- 历史归档/裁剪（标记方案下 `agent_messages` 只增不减，长会话的存储增长留给后续迭代）

## 测试计划

- `internal/commands`：目录内容与顺序、动作命令元数据、中文覆盖表命中内置技能名。
- `tools`：`ExpandSkill` 对四个内置技能的输出 golden（与 `skillInvoke` 逐字节一致）；
  未知技能名报错文案；`ListInstalledSkills` 在 binder 报错时的降级。
- `agentsession`：`AfterLastClear` 的边界（无标记 / 单标记 / 多标记 / 标记在首尾 / 跨标记的工具行
  不被拼装）。
- `sysagent`：`projectHistory` 截断后只回放标记之后的行；`RestorePreamble` 对 stdio 走同一投影，
  用同一组用例覆盖。
- `agentapi`：`/commands` 路由鉴权与响应形状（用 fake Envs/Sandboxes）；`command` 帧在 runner 上的
  解析与落库行为（fake ACP）。
- 前端单测：`buildSendPayload` 的四种输入（纯文本 / 纯 chip / chip+参数 / 未匹配斜杠文本）。
- Playwright：`web/e2e/slash-commands.spec.ts`（假 WS，验证协议帧与气泡文案、`cleared` 刷新）。
- TDD：先写测试再实现。

## 依赖与前置

- 无需新依赖。Semi `AIChatInput` 的 `skills`/`skillHotKey`/`renderSkillItem` 已在
  `@douyinfe/semi-ui-19@2.103.0` 中。
- Playwright `page.routeWebSocket` 需要 `@playwright/test ≥ 1.48`（仓库为 `^1.55`，满足）。
- `web/node_modules` 未安装在 worktree 里，实施前需 `npm ci`（或从主 checkout 软链）。

## 风险与边界

- **stdio `/clear` 慢**：`claude` provider 重建运行时最坏 1-2 分钟，期间输入框保持 busy。
  这是现有 runtime 生命周期的固有代价（没有 Reset API），换来做完即干净。
- **已安装技能对 stdio 会话不可见**：`Skill` 工具与 `~/.roundpen/skills` 都只属于 sysadmin
  模式的 agent 容器；`claude`/`stdio` 会话只保证内置技能 + 动作命令。v1 记录为限制。
- **历史只增不减**：标记方案下 `/clear` 不缩小 `agent_messages`，长会话的存储增长需要归档/裁剪
  迭代来解决（非目标）。
- **`/help` 依赖 event 气泡渲染**：若前端后续改 event 的渲染方式，`/help` 的输出样式会跟着变
  （功能不受影响）。
- **技能目录与实际可用性可能短暂不一致**：目录是拉取时的快照，技能被删除后旧菜单仍可能显示，
  此时命令返回"未知技能"错误帧，用户可以重新拉取（`done` 后会刷新）。
