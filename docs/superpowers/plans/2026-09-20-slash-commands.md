# Slash 命令实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: 用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务执行。步骤用 `- [ ]` 复选框跟踪。

**Goal:** 用户能在输入框用 `/` 打开命令菜单并直接调用技能（内置 + 已安装），另有两个动作命令 `/clear`（清空会话上下文与记录）与 `/help`（列出命令）。

**Architecture:** 前端只发"哪个命令 + 什么参数"（`{type:"command",name,args}`）；控制面把技能展开成指令文本，作为 user 行的 `content` 持久化（`meta.display` 存原文供 UI 显示），再走既有 ACP 轮次路径。命令目录由新的 `GET /v1/agent-sessions/{id}/commands` 提供，内置技能/动作命令永远可用，已安装技能尽力而为（容器 running 时才列，绝不拉起容器）。

**Tech Stack:** Go 1.22+（`net/http` patterns、PostgreSQL、gorilla/websocket）、React 19 + Semi `@douyinfe/semi-ui-19`（Tiptap 编辑器）、`node:test`、Playwright。

**Spec:** `docs/superpowers/specs/2026-09-20-slash-commands-design.md`

---

## File map

| Path | Responsibility |
|------|----------------|
| `internal/commands/commands.go` | 新包：命令元数据（`Command`/`Kind`）、`Actions()`、`Catalog()`、`HelpText()`、内置技能中文描述覆盖表 |
| `internal/commands/commands_test.go` | 上述纯函数的表驱动测试 |
| `internal/acp/sysagent/tools/skill.go` | 抽出 `formatSkillInvocation`；新增 `ExpandSkill`、`ListInstalledSkills` |
| `internal/acp/sysagent/tools/skill_export_test.go` | golden 测试：`ExpandSkill` 输出与 `skillInvoke` 逐字节一致 |
| `internal/agentsession/store.go` | 新增 `DeleteMessages(ctx, sessionID)` |
| `internal/agentsession/store_test.go` | 删除语义测试（无 `DATABASE_URL` 时 skip） |
| `internal/api/agentapi/handler.go` | `wsIn` 加 `Name`/`Args`；`sessionWS` 加 `command` case；注册 `/commands` 路由；`cleared` 帧 |
| `internal/api/agentapi/command.go` | 新文件：`runner.command` / `reset` / `clearPending` / 目录处理器 |
| `internal/api/agentapi/command_test.go` | runner 命令路径与目录处理器的测试（fake ACP / fake Envs） |
| `internal/api/agentapi/runner.go` | `runner.actor`、`permWait.cancel`、`finishTurn` 处理 `clearPending` |
| `web/src/lib/slashCommand.ts` | 新文件：`buildSendPayload`、`contentsHaveSendableText` 的修正 |
| `web/src/lib/slashCommand.test.ts` | 纯函数单测 |
| `web/src/api.ts` | `agents.commands(id)` |
| `web/src/pages/ChatSessionPage.tsx` | catalog 状态、`skills`/`skillHotKey`/`renderSkillItem`、发送路径、`cleared` 分支、outbox |
| `web/src/lib/semiChatAdapter.ts` | user 分支用 `meta.display` |
| `tests/uismoke/main.go` | `/commands` stub |
| `web/e2e/slash-commands.spec.ts` | 假 WS 端到端 |
| `docs/skills.md` | 补 `/name` 直接调用说明 |

---

## Phase 1 — 命令目录与技能展开（纯逻辑，先测后写）

### Task 1: `internal/commands` 包

- [ ] 写 `internal/commands/commands_test.go`：
  - `Catalog()` 返回的 `clear`/`help` 元数据（`kind=action`、`source=action`）；
  - 中文覆盖表对 `commit/review/fix/summarize` 四个内置技能名都有条目，且**只**包含这四个；
  - `HelpText()` 包含每个动作命令与传入的技能名，且不含英文内置描述以外的乱码。
  运行：`go test ./internal/commands/ -run TestCatalog` → 期望 FAIL（包不存在）。
- [ ] 写 `internal/commands/commands.go`：

```go
package commands

// Kind 区分技能命令（展开成指令文本）与动作命令（控制面直接执行）。
type Kind string

const (
    KindSkill  Kind = "skill"
    KindAction Kind = "action"
)

// Command 是命令目录里的一条；JSON 形状即 /commands 接口的响应元素。
type Command struct {
    Name        string `json:"name"`
    Kind        Kind   `json:"kind"`
    Source      string `json:"source"` // builtin | installed | action
    Description string `json:"description"`
    Args        bool   `json:"args"` // 是否接受自由文本参数
}

// Actions 返回内置动作命令。
func Actions() []Command { ... } // clear: 清空本会话上下文；help: 显示可用命令

// SkillDisplayDescription 返回内置技能的中文展示描述（覆盖表未命中返回 "", false）。
func SkillDisplayDescription(name string) (string, bool)

// HelpText 渲染 /help 的输出（Markdown-ish 纯文本，前端按 event 气泡显示）。
func HelpText(skills []Command) string
```

  运行：`go test ./internal/commands/` → 期望 PASS。

### Task 2: `tools` 的技能复用入口

- [ ] 先写 `internal/acp/sysagent/tools/skill_export_test.go`：
  - golden：`ExpandSkill("review", "")` 与 `ExpandSkill("commit", "写中文信息")` 的输出与
    `skillInvoke(ctx, binder, actor, name, args)`（fake binder 会让它走 builtin 回退）逐字节一致；
  - `ExpandSkill("nope", "")` 返回 `unknown skill "nope"…` 错误；
  - `ListInstalledSkills` 在 binder 报错时返回错误而不是 panic。
  运行：`go test ./internal/acp/sysagent/tools/ -run 'TestExpandSkill|TestListInstalledSkills'` → 期望 FAIL。
- [ ] 改 `internal/acp/sysagent/tools/skill.go`：
  - 把 `skillInvoke`（`:447-473`）的格式化部分抽成
    `func formatSkillInvocation(s Skill, args string) string`，`skillInvoke` 改为调用它（行为不变）；
  - 新增 `func ExpandSkill(name, args string) (string, error)`：只查内置技能（`builtinSkill`），
    不 exec、不碰容器；
  - 新增 `func ListInstalledSkills(ctx context.Context, b *AgentBinder, actor Actor) ([]Skill, error)`：
    基于现有 `listInstalledSkillNames` + `readInstalledSkill`，带 5s 超时，单个技能读失败时用
    `Description: "installed skill (unreadable)"` 占位而不是整体失败。
  运行：同上 → 期望 PASS；再跑 `go test ./internal/acp/sysagent/tools/` 全量。

---

## Phase 2 — 存储

### Task 3: `Store.DeleteMessages`

- [ ] 在 `internal/agentsession/store_test.go` 加用例：插入两条消息到两个会话 →
  `DeleteMessages(ctx, sessionA)` → A 空、B 不变；返回删除行数。沿用现有
  `DATABASE_URL` 缺失即 `t.Skip` 的模式。
  运行：`go test ./internal/agentsession/ -run TestDeleteMessages` → 期望 FAIL。
- [ ] 在 `internal/agentsession/store.go` 头部常量区加 SQL，并实现：

```go
// DeleteMessages removes every message in a session and reports how many rows
// were deleted. Callers reset the agent context this way (/clear).
func (s *Store) DeleteMessages(ctx context.Context, sessionID string) (int64, error)
```

  运行：同上 → 期望 PASS（有 DB）或 SKIP（无 DB）。

---

## Phase 3 — 后端协议与执行

### Task 4: WS 协议与 `/commands` 路由

- [ ] `internal/api/agentapi/handler.go`：
  - `wsIn`（`:339-345`）加 `Name string \`json:"name,omitempty"\`` 与 `Args string \`json:"args,omitempty"\``；
  - switch（`:484-493`）加 `case "command": run.command(in.Name, in.Args)`；
  - `Mount`（`:62-73`）加 `mux.HandleFunc("GET /v1/agent-sessions/{id}/commands", h.listCommands)`。
- [ ] `internal/api/agentapi/command.go` 里实现 `listCommands`：
  - 鉴权照抄 `listMessages`（`:313-337`，`sess.UserID != user.Username && role != admin` → 403）；
  - 起始集合 = `commands.Actions()` + 四个内置技能（`source=builtin`，描述取中文覆盖表）；
  - 已安装技能：仅当 `h.Envs != nil` 且 `h.Envs.List(ctx, sess.UserID)` 里 agent slot
    `Status == string(sandbox.StatusRunning)` 时，用
    `&tools.AgentBinder{Slots: h.Envs, Exec: h.Sandboxes, Files: h.Sandboxes}` +
    `manager.Actor{Username: sess.UserID, Role: string(sess owner role), APIKey: <owner key>}` 调
    `tools.ListInstalledSkills`（5s 超时）；失败只记日志、不影响响应；
  - 响应 `{"commands":[...]}`，按 `kind` 再按名字排序，保证稳定。
- [ ] 测试（`command_test.go`）：fake `userenv.Service` / `sandbox.Manager`（参考
  `internal/userenv/fake_test.go` 与 `runner_perm_test.go` 的 fake 写法）断言：
  slot 非 running 时只有内置命令；running 时包含已安装技能；鉴权失败 403。
  运行：`go test ./internal/api/agentapi/ -run TestListCommands` → 期望 PASS。
- [ ] `web/e2e` 之前先在 `tests/uismoke/main.go`（`:152-180` stub 区）加
  `GET /v1/agent-sessions/{id}/commands` 返回两条命令（`review` 技能 + `clear` 动作），供 e2e 使用。

### Task 5: `runner.command`（技能命令 + `/help`）

- [ ] `command.go` 实现：

```go
// command executes a slash command: skills expand into instruction text and run
// as a normal user turn; /help and /clear act on the control plane directly.
func (r *runner) command(name, args string) {
    name = strings.TrimSpace(strings.ToLower(name))
    switch name {
    case "clear": r.requestClear(); return
    case "help":  r.help();         return
    }
    text, err := r.expandSkill(name, args)   // 已安装优先 → 内置回退
    if err != nil { r.broadcast(wsOut{Type: "error", Message: err.Error()}); return }
    display := "/" + name
    if strings.TrimSpace(args) != "" { display += " " + strings.TrimSpace(args) }
    r.startUserTurn(text, map[string]any{
        "type": "user", "command": name, "commandArgs": args, "display": display,
    })
}
```

- [ ] `runner.prompt`（`runner.go:220-233`）重构出 `startUserTurn(text string, meta any)`：
  落行 + busy 排队 + `beginTurnLocked` 三段逻辑两处共用（`prompt` 传 `meta = {"type":"user"}`）。
- [ ] `expandSkill(name, args)`：`tools.ListInstalledSkills` 里找同名（精确匹配，设备上可能有很多技能
  → 用 `readInstalledSkill` 单读更省）→ 命中则 `formatSkillInvocation`；否则 `tools.ExpandSkill`。
- [ ] `help()`：构造 `commands.HelpText`（目录内容用 `listCommands` 同一份构造逻辑，抽
  `func (h *Handler) commandCatalog(ctx, sess, user) []commands.Command`）→ 落 `role=event` 行
  （`meta = {"type":"event","command":"help"}`）→ 广播 `wsOut{Type:"event", Event: acpclient.Event{Type:"command_result", Text: text}}`。
  前端无需改 adapter：`meta.type=="event"` 走 `semiChatAdapter.ts:140-148` 的系统气泡分支。
- [ ] 测试（`command_test.go`，fake ACP runtime）：技能命令落库行 `content` 是展开文本、
  `meta.display` 是 `/review …`；未知命令广播 error 帧且不落库。
  运行：`go test ./internal/api/agentapi/ -run TestRunnerCommand` → 期望 PASS。

### Task 6: `/clear` 状态机

- [ ] `runner.go`：`runner` 结构体加 `actor manager.Actor` 与 `clearPending bool`；
  `runnerFor`（`:639-643`）把已构造的 `actor` 存进 runner；
  `permWait`（`:25-32`）加 `cancel chan struct{}`，`onPermission`（`:401`）构造时初始化，
  select（`:485-509`）加：

```go
case <-pw.cancel:
    r.cancelPermission(reqID, title, req.Options)
    return acp.RequestPermissionResponse{Outcome: acp.RequestPermissionOutcome{
        Cancelled: &acp.RequestPermissionOutcomeCancelled{}}, nil
```

- [ ] `command.go` 实现 `requestClear` / `finishClear`（`reset`）：

```go
func (r *runner) requestClear() {
    r.mu.Lock()
    if r.busy { r.clearPending = true; r.pending = nil } // busy 分支：排队到回合结束
    r.mu.Unlock()
    for _, pw := range r.snapshotPerms() { close(pw.cancel) } // 解开可能卡住的权限对话框
    if !busy { go r.reset() }
}
```

  `reset()`：`Store.DeleteMessages` → 落 `/clear` event 行 → 广播 `{"type":"cleared"}` →
  `r.acp.Stop(sessionID)` → `r.acp.Start(ctx, sess.ID, sess.SandboxID, sess.ProviderID, manager.StartOpts{Actor: r.actor})` →
  重挂 `SetEventHandler`/`SetPermissionHandler`/`SetAutoMode(r.auto)`、`r.rt = rt`；
  失败时广播 error 且**保持** `busy=false`（下一次 WS 连接时 `runnerFor` 会重建）。
- [ ] `finishTurn`（`:272-287`）加：`clearPending` → 清标志、`busy=true`、`go r.reset()`，
  其余排队逻辑不变。
- [ ] 测试：fake ACP 断言顺序（cancel → 回合结束 → delete → cleared 帧 → Stop/Start 调用）；
  在途权限对话框下 clear 能返回而不是卡住（`select` 带 timeout 断言）。
  运行：`go test ./internal/api/agentapi/ -run 'TestClear'` → 期望 PASS；
  再全量：`go build ./... && go test ./internal/api/agentapi/ ./internal/commands/ ./internal/acp/sysagent/tools/ ./internal/agentsession/`

---

## Phase 4 — 前端

### Task 7: `slashCommand.ts` + 单测

- [ ] 写 `web/src/lib/slashCommand.test.ts`（`node:test`）：四种输入 →
  纯文本 → `{kind:'text'}`；纯 chip → `{kind:'command',name}`；chip + 尾随文本 →
  `{kind:'command',name,args}`；`/etc/hosts 坏了`（无 chip）→ `{kind:'text',text:'/etc/hosts 坏了'}`；
  另测 `contentsHaveSendableText` 对 chip-only 返回 true。
  运行：`cd web && node --test --experimental-strip-types src/lib/slashCommand.test.ts` → 期望 FAIL。
- [ ] 写 `web/src/lib/slashCommand.ts`：

```ts
export type CommandCatalogEntry = { name: string; label?: string; description?: string; kind?: string; args?: boolean }
export type SendPayload = { kind: 'command'; name: string; args: string } | { kind: 'text'; text: string }

export function buildSendPayload(contents: SendContent[] | undefined, catalog: CommandCatalogEntry[]): SendPayload | null
export function contentsHaveSendableText(contents: Array<Record<string, unknown>> | undefined): boolean
```

  `buildSendPayload` 从 chip（`type === 'skillSlot'`）取 `value ?? label`，命中 catalog 才算命令；
  args = chip 之后所有 text 节点拼接后 trim；chip 之前若有非空文本，按普通文本处理（不发送命令）。
  运行：同上 → 期望 PASS。

### Task 8: 接线 `ChatSessionPage` 与 adapter

- [ ] `web/src/api.ts`：加 `agents.commands(id) → GET /v1/agent-sessions/{id}/commands`，
  类型 `AgentCommand { name; description?; kind?; source?; args? }`。
- [ ] `ChatSessionPage.tsx`：
  - 新增 `catalog` 状态：挂载时 + 每次 `done` 后拉取（失败静默为空数组）；
  - `AIChatInput`（`:1076`）加 `skills={catalog.map(toSkillItem)}`、`skillHotKey="/"`、
    `renderSkillItem`（名称 + 描述 + `installed` 标记）；
  - `handleMessageSend`（`:859`）改用 `buildSendPayload`：命令帧在 `ws.readyState === OPEN` 时
    直接 `ws.send(JSON.stringify({type:'command', name, args}))` 并本地回显 `/{name} {args}`；
    **不走 outbox**（断线时提示用户重发）；
  - WS 消息处理（`:531-653`）加 `case 'cleared'`：清 `messages`、`clearStreaming`、清空 outbox、
    `refetchMessages()`；
  - `composerHasText` 改用新的 `contentsHaveSendableText`（chip-only 可发送）。
- [ ] `semiChatAdapter.ts:119-127`：`content: typeof m.meta?.display === 'string' ? m.meta.display : m.content`
  （`AgentMessageMeta` 类型加可选 `display`/`command`/`commandArgs`）。
- [ ] 验证：`cd web && npx tsc -b && npm run lint && node --test --experimental-strip-types src/lib/slashCommand.test.ts src/lib/semiChatAdapter.test.ts`

---

## Phase 5 — e2e 与文档

### Task 9: Playwright 端到端

- [ ] `web/e2e/slash-commands.spec.ts`：
  - `page.goto('/as-seed/s/sess-seed')`（uismoke 的种子会话）；
  - `page.routeWebSocket('**/v1/agent-sessions/*/ws', ws => { ws.onMessage(...); })`，连接后推
    `{type:'hello'}`、`{type:'status'}`，并记录前端发出的帧；
  - 在输入框输入 `/` → 断言菜单出现 `review` → 点选 → 输入 `关注并发` → Enter；
  - 断言收到 `{type:'command',name:'review',args:'关注并发'}`，气泡文本为 `/review 关注并发`；
  - 推 `{type:'cleared'}` → 断言消息列表被清空并触发 `/messages` 重新拉取（用 `page.waitForRequest`）。
  运行：`cd web && npm run test:e2e -- slash-commands` → 期望 PASS。
- [ ] 若 Composer 的假 WS 需要登录态：复用 `web/e2e/helpers.ts:14-31` 的登录等待 helper，
  并确认 uismoke 的 `/commands` stub 已就绪（Task 4 最后一步）。

### Task 10: 文档

- [ ] `docs/skills.md` 顶部「Skill 工具动作」附近补一段：内置与已安装技能都可以由用户在
  输入框用 `/name [args]` 直接发起；语义与模型 `Skill(invoke)` 完全一致（同一份指令文本）。
- [ ] `docs/architecture/acp-agent-ui.md` 的 WS 协议小节补 `command` 入站帧与 `cleared` 出站帧。

---

## 验收清单（实施完成后逐条跑）

- [ ] `go build ./... && go vet ./...`
- [ ] `go test ./internal/commands/ ./internal/acp/sysagent/tools/ ./internal/api/agentapi/ ./internal/agentsession/`
      （无 `DATABASE_URL` 时存储用例 SKIP）
- [ ] `cd web && npm ci && npx tsc -b && npm run lint && node --test --experimental-strip-types src/lib/slashCommand.test.ts`
- [ ] `cd web && npm run test:e2e -- slash-commands`
- [ ] 手工（`make dev`，sysadmin provider 会话）：打 `/` 出菜单 → `/review` 跑通一轮 →
      `/help` 输出清单 → `/clear` 清空且下一轮模型不知道旧内容；
      再开一个 `claude` provider 会话验 `/clear` 的重启路径（不打开 installed 技能，符合限制）。
