# 常驻任务 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用户把一件反复发生的事分配给一个助手后，到点由控制面唤醒该助手做完，留下 `RTN-n` / `RUN-n`，并把一条摘要送回主对话和 IM。

**Architecture:** 新增 `internal/routine`（领域 + store + HTTP + 调度器）。表写进 `internal/storage/schema/postgres.sql`（幂等，启动时整文件生效）并留一份 `migrations/0022_routines.sql`。运行会话是 `agent_sessions.kind='routine'`，不进入主对话挑选，也不进入 IM 的会话列表。调度器在 `roundpend` 里轮询，领取用 `FOR UPDATE SKIP LOCKED`，执行复用 `agentapi.Handler.StartForAssistant` + `manager.Manager.Prompt`。`read` 运行不注册浏览器和 Shell；确认闸用新工具 `RequestRoutineConfirm` 挂协助单。IM 推送走 cc-connect 的 `ReplyContextReconstructor`，没有历史会话就只记 `deliver_error`。

**Tech Stack:** Go 1.26（`net/http` ServeMux、`database/sql`）、PostgreSQL、`github.com/robfig/cron/v3`（已在 go.mod indirect，本计划改为 direct）、React 19 + Vite + Semi Design、Playwright e2e（远程 browserless，不在本机装 Chromium）。

**Spec:** `docs/superpowers/specs/2026-10-01-standing-routines-design.md`

**分支：** 建议在独立分支 `feat/standing-routines` 上执行。

**Follow-on plans（本文件不实现）：**

| Plan | Scope |
|------|-------|
| 2 | 凭据库与浏览器登录态（抢券、报税填表） |
| 3 | 按简介自动派活 |
| 4 | 任务依赖、token 预算、邮件投递 |

---

## File map

| Path | Responsibility |
|------|----------------|
| `migrations/0022_routines.sql` | `routines` / `routine_runs`、两个 key 序列、`agent_sessions.kind` |
| `internal/storage/schema/postgres.sql` | 同样的幂等 DDL |
| `internal/routine/routine.go` | 类型、枚举、校验、cron / 宽限 / 最小间隔 |
| `internal/routine/routine_test.go` | 日程纯函数测试 |
| `internal/routine/store.go` | CRUD、领取事务、state 替换 |
| `internal/routine/store_test.go` | Postgres store 测试 |
| `internal/routine/http.go` | `/v1/routines` |
| `internal/routine/http_test.go` | 校验与错误码 |
| `internal/routine/prompt.go` | 注入运行会话的用户提示 |
| `internal/routine/runner.go` | 轮询、唤醒、超时、补跑、不重叠、收工后投递 |
| `internal/routine/runner_test.go` | 用假的会话启动器覆盖跳过 / 补跑 / 超时 |
| `internal/agentsession/store.go` | `kind`；`ListByAssistant` 只返回 `chat` |
| `internal/acp/manager/manager.go` | `StartOpts.Routine` 决定工具面 |
| `internal/acp/sysagent/tools/routines.go` | 常驻任务工具 |
| `internal/acp/sysagent/history.go` | 系统提示词 |
| `internal/automode/rules.go` | 记录类 Allow 规则 |
| `internal/imconnect/notify.go` | 向已有 IM 会话推一条摘要 |
| `internal/assistant/http.go` | 停用助手时暂停其任务 |
| `cmd/roundpend/main.go` | 挂载 handler、启动 runner |
| `web/src/api/routines.ts` | 类型与请求 |
| `web/src/pages/AssistantDetailPage.tsx` | 「常驻任务」分区 |
| `web/src/pages/RoutineDetailPage.tsx` | 运行列表 |
| `web/src/App.tsx` | 路由 |
| `web/src/i18n/en.ts`、`web/src/i18n/zh_CN.ts` | 文案 |
| `web/e2e/routines.spec.ts` | 控制台闭环 |
| `README.md` | 核心能力一条 |

---

### Task 1: 表、短 ID、日程纯函数

**Files:**
- Create: `migrations/0022_routines.sql`
- Modify: `internal/storage/schema/postgres.sql`（文件末尾追加同样语句）
- Create: `internal/routine/routine.go`
- Create: `internal/routine/routine_test.go`

- [ ] **Step 1: DDL**

`agent_sessions` 增加 `kind`，避免运行会话变成主对话（`ListByAssistant` 按 `updated_at DESC` 取第一条，见 `internal/assistant/http.go` 的 `fillPrimarySession`；IM 的 `ListSessions` 也走这条）。

```sql
ALTER TABLE agent_sessions
    ADD COLUMN IF NOT EXISTS kind TEXT NOT NULL DEFAULT 'chat'
        CHECK (kind IN ('chat', 'routine'));

CREATE SEQUENCE IF NOT EXISTS routine_key_seq;
CREATE SEQUENCE IF NOT EXISTS routine_run_key_seq;

CREATE TABLE IF NOT EXISTS routines (
    id                        TEXT PRIMARY KEY,
    key                       TEXT NOT NULL UNIQUE,
    user_id                   TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    assignee_assistant_id     TEXT REFERENCES assistants (id) ON DELETE SET NULL,
    created_by_assistant_id   TEXT REFERENCES assistants (id) ON DELETE SET NULL,
    created_by_session_id     TEXT NOT NULL DEFAULT '',
    issue_id                  TEXT REFERENCES issues (id) ON DELETE SET NULL,
    title                     TEXT NOT NULL,
    brief                     TEXT NOT NULL DEFAULT '',
    autonomy                  TEXT NOT NULL CHECK (autonomy IN ('read', 'browse')),
    hosts                     JSONB NOT NULL DEFAULT '[]',
    cron                      TEXT NOT NULL,
    timezone                  TEXT NOT NULL,
    deliver_im                BOOLEAN NOT NULL DEFAULT FALSE,
    max_duration_sec          INTEGER NOT NULL DEFAULT 900
        CHECK (max_duration_sec BETWEEN 60 AND 3600),
    state                     JSONB NOT NULL DEFAULT '{}',
    status                    TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'paused', 'archived')),
    next_run_at               TIMESTAMPTZ,
    created_at                TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at                TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS routines_due_idx
    ON routines (next_run_at) WHERE status = 'active';
CREATE INDEX IF NOT EXISTS routines_user_idx
    ON routines (user_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS routines_assignee_idx
    ON routines (assignee_assistant_id, status);

CREATE TABLE IF NOT EXISTS routine_runs (
    id               TEXT PRIMARY KEY,
    key              TEXT NOT NULL UNIQUE,
    routine_id       TEXT NOT NULL REFERENCES routines (id) ON DELETE CASCADE,
    user_id          TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    assistant_id     TEXT REFERENCES assistants (id) ON DELETE SET NULL,
    session_id       TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL
        CHECK (status IN (
            'queued', 'running', 'waiting_user',
            'succeeded', 'failed', 'skipped_overlap', 'skipped_stale', 'cancelled'
        )),
    scheduled_at     TIMESTAMPTZ NOT NULL,
    started_at       TIMESTAMPTZ,
    finished_at      TIMESTAMPTZ,
    summary          TEXT NOT NULL DEFAULT '',
    artifacts        JSONB NOT NULL DEFAULT '[]',
    error            TEXT NOT NULL DEFAULT '',
    deliver_error    TEXT NOT NULL DEFAULT '',
    assist_ticket_id TEXT NOT NULL DEFAULT '',
    budget_left_sec  INTEGER,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS routine_runs_routine_idx
    ON routine_runs (routine_id, created_at DESC);
CREATE UNIQUE INDEX IF NOT EXISTS routine_runs_one_open_idx
    ON routine_runs (routine_id)
    WHERE status IN ('queued', 'running', 'waiting_user');
```

`budget_left_sec` 不在规格表里，它是确认闸暂停墙钟时剩下的秒数（Task 7）。`routine_runs_one_open_idx` 把「不重叠」落到数据库，领取逻辑不用靠应用层碰运气。

助手删除时 FK 把 `assignee_assistant_id` 置空。置空之后调度器不再领取（Task 6 的 SQL 要求负责人非空）。停用助手由 Task 8 把任务改成 `paused`，因为停用不是删除。

- [ ] **Step 2: 领域类型与日程**

`internal/routine/routine.go`：

- 常量与规格 §8 一致：`AutonomyRead` / `AutonomyBrowse`，任务状态，运行状态。
- `ValidateCreate`：标题非空；`browse` 的 `hosts` 至少一个主机（小写、无协议、无路径）；`read` 的 `hosts` 必须为空；`max_duration_sec` 默认 900、范围 60–3600；时区能被 `time.LoadLocation` 解析。
- `ParseSchedule(cronExpr, tz string) (Schedule, error)` 用 `cron.ParseStandard`（五段，无秒）。把 `github.com/robfig/cron/v3` 加进 `go.mod` 的 require。
- `NextAfter(s Schedule, from time.Time) time.Time`：在该时区里算下一次严格晚于 `from` 的触发点，返回 UTC。
- `Interval(s Schedule, from time.Time) time.Duration`：`NextAfter(from)` 与再下一次的差。用它做两件事：
  - 最小间隔：从现在起连续 48 次触发，相邻间隔都 ≥ 1 分钟，否则拒绝。
  - 宽限：间隔 ≥ 24h 时宽限 24h，否则宽限等于这一间隔。
- `ClassifyDue(scheduled, now time.Time, grace time.Duration) string`：`fire`（`now-scheduled <= grace`）或 `stale`。
- state 上限常量 `MaxStateBytes = 64 << 10`。

- [ ] **Step 3: 测试**

`routine_test.go`，不连数据库：

- `0 8 * * 1` + `Asia/Shanghai`：下一次是周一 08:00 本地，UTC 正确。
- `* * * * *` 通过最小间隔；`* * * * *` 的秒级表达式（六段）被 `ParseStandard` 拒绝。
- 构造间隔 < 1 分钟的五段表达式（例如在一小时内多分钟密集）被拒绝。若标准五段最密就是每分钟，则断言每分钟表达式通过，并另写一个直接喂 `Interval` 的表项覆盖「小于 1 分钟则拒绝」的函数。
- 周报（间隔 7 天）错过 2 小时 → `fire`；错过 48 小时 → `stale`。
- 每小时任务错过 30 分钟 → `fire`；错过 3 小时 → `stale`。
- `browse` 无 hosts、`read` 有 hosts、坏时区、空标题 → 校验错误。

Run: `go test ./internal/routine/ -count=1 -run 'TestSchedule|TestValidate'`

- [ ] **Step 4: Commit**

```bash
git add migrations/0022_routines.sql internal/storage/schema/postgres.sql internal/routine/routine.go internal/routine/routine_test.go go.mod go.sum
git commit -m "$(cat <<'EOF'
Add routine tables and schedule rules for standing tasks.

EOF
)"
```

---

### Task 2: Store

**Files:**
- Create: `internal/routine/store.go`
- Create: `internal/routine/store_test.go`

对照 `internal/issue/store.go` 的写法：`database/sql`、短 ID 用 `nextval`、列表按用户过滤。

- [ ] **Step 1: 写入与读取**

- `Create`：同一条 `INSERT` 里 `'RTN-' || nextval('routine_key_seq')`。`status='active'` 时 `next_run_at = NextAfter(schedule, now)`。`deliver_im` 由调用方传入（HTTP 层按负责人是否有渠道给默认值）。
- `Get` / `List`：`List` 支持 `assistantId`、`status`，默认排除 `archived`，`updated_at DESC`。`WHERE user_id=$1` 放在 SQL 里。
- `Update`：可改规格 §9 的字段。改 cron、时区，或从 `paused`/`archived` 回到 `active` 时重算 `next_run_at`。改为 `paused` 或 `archived` 时把 `next_run_at` 置空。改 `assignee` 不改进行中的 run。
- `UpdateState`：整块替换。字节数超过 `MaxStateBytes` 返回错误。调用方负责「只有负责人能写」（HTTP / 工具层）。
- `PauseByAssignee(assistantID)`：`UPDATE routines SET status='paused', next_run_at=NULL WHERE assignee_assistant_id=$1 AND status='active'`。
- `ListRuns` / `GetRun`。
- `FinishRun`：仅当 `status='running'` 且 `session_id` 匹配时写成 `succeeded` 或由调用方指定的终态，写 `summary`、`artifacts`、`finished_at`。
- `RequestConfirm`：`running` → `waiting_user`，写入 `assist_ticket_id` 和 `budget_left_sec`。
- `Resume`：`waiting_user` → `running`，清空 `assist_ticket_id`。
- `CancelRun`：非终态 → `cancelled`。

- [ ] **Step 2: 领取**

`ClaimDue(ctx, limit) ([]Claim, error)` 在一个事务里：

```sql
SELECT id FROM routines
WHERE status = 'active'
  AND assignee_assistant_id IS NOT NULL
  AND next_run_at IS NOT NULL
  AND next_run_at <= now()
ORDER BY next_run_at
FOR UPDATE SKIP LOCKED
LIMIT $1
```

对每一行在同一事务内：

1. 若已有 `queued|running|waiting_user` 的 run：插入 `skipped_overlap`（`scheduled_at` = 旧的 `next_run_at`），把 `next_run_at` 推到下一个未来点。不返回给执行器。
2. 若 `ClassifyDue` 为 `stale`：插入 `skipped_stale`，`next_run_at` 推到 `NextAfter(now)`。不返回。
3. 否则插入 `queued` run（`RUN-` + `nextval`，`scheduled_at` = 旧 `next_run_at`，`assistant_id` = 当前负责人），把 `next_run_at` 推到下一个未来点，把这条 claim 返回。返回值带任务快照（brief、autonomy、hosts、state、max_duration、deliver_im、user_id、assistant_id）和 run id/key，执行器不再为了启动再读一次可被别人改掉的行。

推进 `next_run_at` 放在领取事务里，这样进程在启动会话之前崩溃，不会把同一次触发领两次。崩溃后的 `queued` 由 Task 6 的启动补偿处理。

- [ ] **Step 3: 测试**

沿用 `internal/issue/store_test.go` 的 `ROUNDPEN_TEST_DATABASE_URL` / `MigrateEmbedded`。每个用例插入自己的 user 和 assistant，结束时删 user（级联）。

- 创建 → `RTN-` 键单调；`next_run_at` 在未来。
- 用户 A 的列表看不见用户 B。
- 两次 `ClaimDue`：第一次返回；第一次还没终态时第二次得到 `skipped_overlap` 且不返回 claim。
- 把 `next_run_at` 拨到 48 小时前（周 cron）再领取 → `skipped_stale`，没有 `queued`。
- 拨到 1 小时前 → `queued`，且 `next_run_at` 已是再下一次。
- `UpdateState` 超过 64KB 失败。
- `FinishRun` 用错误的 `session_id` 不改状态。

Run: `go test ./internal/routine/ -count=1`

- [ ] **Step 4: Commit**

```bash
git add internal/routine/store.go internal/routine/store_test.go
git commit -m "$(cat <<'EOF'
Persist standing routines and claim due runs without overlap.

EOF
)"
```

---

### Task 3: HTTP API

**Files:**
- Create: `internal/routine/http.go`
- Create: `internal/routine/http_test.go`
- Modify: `cmd/roundpend/main.go`（在 issue handler 旁边挂上）

- [ ] **Step 1: 路由**

与 `internal/issue/http.go` 相同的鉴权（`auth.GetUser`）和错误体 `{"error":"..."}`。

| Method | Path |
|--------|------|
| GET | `/v1/routines` |
| POST | `/v1/routines` |
| GET | `/v1/routines/{key}` |
| PATCH | `/v1/routines/{key}` |
| GET | `/v1/routines/{key}/runs` |
| GET | `/v1/routines/{key}/runs/{runKey}` |
| POST | `/v1/routines/{key}/runs/{runKey}/cancel` |

`{key}` 同时接受短 ID 和 uuid（`WHERE id=$1 OR key=$1`，再加 `user_id`）。

POST 体：`title`、`brief`、`cron`、`timezone`、`autonomy`、`hosts`、`deliverIm`、`maxDurationSec`、`assigneeAssistantId`、`issueKey`、`sessionId`、`assigneeConfirmed`。

- 校验负责人属于当前用户且 `status=active`。缺省时：有 `sessionId` 就用该会话的 `assistant_id`，否则 400。
- `sessionId` 必须属于当前用户。`created_by_*` 从这行会话写入，不信客户端自报的助手 id。
- `assigneeAssistantId` 不是创建会话的助手时，要求 `assigneeConfirmed=true`。控制台代用户创建时总是带 true。
- 能力检查（规格 §3.1）：读助手的 `capabilities.browser` 和 `networkTier`。`read` 要求网络不是 `none`；`browse` 还要求 Browser 已开。不满足返回 400，不插入。
- `issueKey` 解析成该用户的 issue id，否则 400。
- `deliverIm` 省略时：负责人 `ImChannels.HasEnabledChannel()` 则为 true。

PATCH 改负责人或自主级别时重做能力检查。`UpdateState` 不进公开 PATCH；state 只通过 Task 5 的工具写，避免控制台把游标覆盖掉。GET 详情照常返回 state。

GET 详情附最近 10 条 run。运行详情单独的 GET 返回全文。

- [ ] **Step 2: 测试**

`httptest` + 真实 store（同一 DSN 开关）。覆盖：未登录 401；browse 但助手没开 Browser → 400 且无行；跨用户 GET 404；改派不带 `assigneeConfirmed` → 400；带了且助手属于该用户 → 200。

Run: `go test ./internal/routine/ -count=1`

- [ ] **Step 3: 装配**

`cmd/roundpend/main.go` 在 `issue.Handler` 之后：

```go
routineStore := &routine.Store{DB: db.SQL}
(&routine.Handler{
    Store:      routineStore,
    Assistants: asstStore,
    Sessions:   agentStore,
    Issues:     issueStore,
}).Mount(mux)
```

`asstStore` 用这里已经构造的 assistant store（与 `asstHandler` 同一个）。若它在更后面才创建，把 routine handler 的 `Mount` 挪到 assistant store 创建之后，不要再 new 一个。

- [ ] **Step 4: Commit**

```bash
git add internal/routine/http.go internal/routine/http_test.go cmd/roundpend/main.go
git commit -m "$(cat <<'EOF'
Expose standing routines over the user-scoped HTTP API.

EOF
)"
```

---

### Task 4: 运行会话不占用主对话

**Files:**
- Modify: `internal/agentsession/store.go`
- Modify: `internal/agentsession/store_test.go`（若已有 List 测试则补一条）

- [ ] **Step 1: `kind`**

`Session` 增加 `Kind string` `json:"kind"`。`Create` 仍写 `chat`。新增 `CreateKind(..., kind string)`，只接受 `chat` 和 `routine`。

`sessionCols` 带上 `kind`。`ListByAssistant` 增加 `AND kind = 'chat'`。这同时保护：

- `fillPrimarySession` 和 ensure-session（`internal/assistant/http.go`）
- 助手活动（`internal/assistant/http_activity.go`）
- IM `ListSessions`（`internal/imconnect/agent.go`）

运行会话只按 id 读取（`Get` 不过滤 kind）。

- [ ] **Step 2: 测试**

插入一条 `chat` 和一条更新时间更晚的 `routine`，`ListByAssistant` 只返回 chat。

Run: `go test ./internal/agentsession/ -count=1`

- [ ] **Step 3: Commit**

```bash
git add internal/agentsession/store.go internal/agentsession/store_test.go
git commit -m "$(cat <<'EOF'
Keep routine-run sessions out of the assistant primary chat.

EOF
)"
```

---

### Task 5: 工具、提示词、运行期工具面

**Files:**
- Create: `internal/acp/sysagent/tools/routines.go`
- Modify: `internal/acp/manager/manager.go`
- Modify: `internal/acp/sysagent/history.go`
- Modify: `internal/automode/rules.go`
- Modify: `internal/automode/rules_test.go`
- Modify: `internal/acp/sysagent/tools/registry_surface_test.go`

- [ ] **Step 1: 普通对话里的工具**

`RegisterRoutines(r, api *RoundpenHTTP, sessionID string)`，调用方式同 `RegisterIssues`：HTTP，不直连 DB。注册期绑定 `sessionID`。

| 工具 | 要点 |
|------|------|
| `CreateRoutine` | 参数同 POST。始终带上 `sessionId`。描述里写明：周期、负责人、自主级别没问清就不要调；交给别的助手前先 `AskUserQuestion`，得到肯定答复才传 `assigneeAssistantId` 和 `assigneeConfirmed=true` |
| `ListRoutines` | `status` 可选 |
| `GetRoutine` | 含 state 和最近一次摘要 |
| `UpdateRoutine` | 暂停、改期、归档、改派。改派同样要求先问过用户 |
| `UpdateRoutineState` | `key` + `state` 对象。新增 `PUT /v1/routines/{key}/state`，仅当当前会话的助手等于 `assignee_assistant_id` 时成功，否则 403。工具走这个路由 |
| `FinishRun` | 见 Step 2，普通对话不注册 |
| `RequestRoutineConfirm` | 见 Step 2，普通对话不注册 |

`UpdateRoutineState` 的 PUT 放在 `internal/routine/http.go`，不要塞进 PATCH。

`automode.Defaults().Allow` 加一条：创建和修改用户自己的常驻任务（`CreateRoutine`、`UpdateRoutine`、`UpdateRoutineState`）只写用户自己的任务行，不授予新能力。`rules_test.go` 断言这句在。`Mutating: true` 保持。

- [ ] **Step 2: 运行会话的工具面**

`manager.StartOpts` 增加：

```go
type RoutineStart struct {
    RunKey   string // 非空表示这是一次常驻运行
    Autonomy string // read | browse
}
```

`Manager.Start` 里，`RunKey==""` 时工具面与今天相同，再加 `RegisterRoutines`（不含 Finish / Confirm）。

`RunKey!=""` 时：

- 仍然注册 Roundpen 只读查询、文件（Read/Write/Edit/Glob/Grep）、WebFetch、WebSearch（若该用户配置了搜索）、Issues、Routines。
- **不**注册 Shell（`RegisterShell`）、Interactive（`AskUserQuestion` / Plan mode）、Skill。运行中要问人走确认闸，不走会挂起的选择题。
- `Autonomy=="browse"` 才 `RegisterBrowser`。`read` 不注册。
- 额外注册 `FinishRun` 和 `RequestRoutineConfirm`，注册期绑定 `runKey` 和 `sessionID`。

`FinishRun` → `POST /v1/routines/runs/{runKey}/finish`，body：`sessionId`、`summary`、`artifacts`。服务端要求 run 的 `session_id` 等于 body，且状态是 `running`。成功后 runner（Task 6）的收工投递由 finish 接口同步调用一个 `OnFinished` 钩子，避免工具和调度器各写一次摘要。钩子在进程内注入（`routine.Handler.OnFinished`），HTTP 测试里可以为 nil。

`RequestRoutineConfirm` → `POST /v1/routines/runs/{runKey}/confirm`，body：`sessionId`、`title`、`reason`、`askHuman`。服务端创建 `assist_tickets`（`kind=other`，助手和会话取自这次 run），把 run 置为 `waiting_user` 并记下剩余预算秒数，然后返回「已停下，不要继续操作」。剩余秒数 = `max_duration_sec - 已运行秒数`，至少 60。票据创建放在 `routine` 包里调用 `assistticket.Store`，避免 sysagent 工具直连 DB。

- [ ] **Step 3: 系统提示词**

`history.go` 的 `system` 常量追加一段，放在 Issues 段落后：

- 用户说以后反复做（每天、每周、每月、到点）时，先问清说明、负责人（默认就是你）、cron 能表达的时间、时区、`read` 还是 `browse`、要不要推 IM，再 `CreateRoutine`。没问清不要建。
- 交给另一个助手必须先用 `AskUserQuestion` 让用户选，再带 `assigneeConfirmed=true`。
- 常驻运行里：按提示里的 brief 做；`read` 不许开浏览器；支付、扣款、申报或表单最终提交、删用户数据、往投递渠道以外发消息，调用 `RequestRoutineConfirm` 后停止，即使用户在 brief 里写了直接提交。
- 做完调用 `FinishRun`。已见集合写进 `UpdateRoutineState`，不要写进周报正文。

- [ ] **Step 4: 测试**

`registry_surface_test.go`：普通注册含 `CreateRoutine`、不含 `FinishRun`。用一段与 `Manager.Start` 相同的注册函数（把注册抽成 `registerSessionTools(reg, deps, opts)`，Start 调用它，测试也调用它）断言 `read` 运行没有 `browser_navigate` 和 `Bash`，有 `FinishRun`；`browse` 运行有 `browser_navigate`，没有 `Bash`。

Run: `go test ./internal/acp/sysagent/tools/ ./internal/acp/manager/ ./internal/automode/ -count=1`

- [ ] **Step 5: Commit**

```bash
git add internal/acp/sysagent/tools/routines.go internal/acp/manager/manager.go internal/acp/sysagent/history.go internal/automode/rules.go internal/automode/rules_test.go internal/acp/sysagent/tools/registry_surface_test.go internal/routine/http.go
git commit -m "$(cat <<'EOF'
Let the agent create routines and narrow tools on a run.

EOF
)"
```

---

### Task 6: 调度器

**Files:**
- Create: `internal/routine/prompt.go`
- Create: `internal/routine/runner.go`
- Create: `internal/routine/runner_test.go`
- Modify: `cmd/roundpend/main.go`
- Modify: `internal/api/agentapi/handler.go`（导出一个能指定 kind 的启动，或在 runner 里自己 CreateKind + `ACP.Start`）

当前默认助手是 `sysadmin`，跑在控制面进程里，`StartForAssistant` 不会因为 `NeedsSandbox==false` 去拉容器。工具会在用到时 `EnsureAgent` / `EnsureBrowser`。调度器仍在 `Prompt` 之前主动确保环境，避免冷启动吃掉 `max_duration`。

- [ ] **Step 1: 提示文本**

`RenderPrompt(c Claim) string` 只含：`RTN` / `RUN` 编号、brief、autonomy、hosts、state JSON、上一份 `succeeded` 摘要（没有就写「无」）、本次必须在多少分钟内结束。不要把系统提示词再贴一遍。

- [ ] **Step 2: Runner**

```go
type SessionStarter interface {
    StartRoutine(ctx context.Context, user *storage.User, assistantID, title, runKey, autonomy string) (sessionID string, err error)
}

type Runner struct {
    Store    *Store
    Users    storage.UserStore
    Envs     interface {
        EnsureAgent(ctx context.Context, userID string) (*sandbox.Sandbox, error)
        EnsureBrowser(ctx context.Context, userID string) (*userenv.BrowserTarget, error)
    }
    Starter  SessionStarter
    ACP      interface {
        Prompt(ctx context.Context, sessionID, text string) (acp.StopReason, error)
        Cancel(ctx context.Context, sessionID string) error
    }
    Sessions *agentsession.Store // 往主会话写摘要
    Notify   func(ctx context.Context, assistantID, text string) error // Task 7 实现；本期先可空
    Tick     time.Duration // 默认 15s
}
```

`StartRoutine` 的实现放在 `agentapi`：`sessions.CreateKind(..., "routine")`，然后 `ACP.Start`，`StartOpts{Actor: ..., Routine: {RunKey, Autonomy}}`。标题用 `RTN-n RUN-n`。不要调用会把 kind 写成 chat 的 `startSession`。

`loop`：

1. `ClaimDue(ctx, 4)`。
2. 每个 claim 起一个 goroutine 跑 `execute`。同一 runner 内用 map 记住正在执行的 run id，防止本进程重入；数据库唯一索引是跨进程的保证。
3. 另扫 `queued` 且 `started_at IS NULL`、创建时间超过 1 分钟的 run（上次进程死在领取之后）：重新 `execute`。用 `UPDATE ... WHERE status='queued' AND started_at IS NULL` 抢所有权，抢到才跑。

`execute`：

1. 把 run 从 `queued` 标成 `running`，写 `started_at`。抢不到就返回。
2. 再读负责人。若已停用、已删、或不再满足自主级别（网络 `none`，或 `browse` 但 Browser 关了）：`failed`，摘要说明原因，并把任务 `paused`。不要每周期失败一次。
3. `EnsureAgent`。`browse` 再 `EnsureBrowser`。失败 → `failed`，任务保持 `active`（环境故障下次再试）。
4. `StartRoutine` + `Prompt(RenderPrompt)`。`Prompt` 的 ctx 在 `max_duration` 后取消，并 `ACP.Cancel`。
5. 正常返回后若 run 仍是 `running`（模型没调 `FinishRun`）：`failed`，摘要「运行结束但没有提交结果」。
6. ctx 超时：`failed`，摘要「超过时限」。

`OnFinished`（Task 5 的钩子）和 `execute` 的失败路径都走 `deliver`：

- `Sessions.AddMessage` 到该助手最新的 `kind=chat` 会话（没有主会话就跳过，不新建）。role 用 `assistant`，内容是一两句：`RTN-n` / `RUN-n`、成功或失败、摘要。这是写历史，不调用 `Prompt`，所以不会让主会话再跑一轮。
- `deliver_im` 为真且 `Notify != nil`：调用它。错误写入 `deliver_error`，不把 run 从 `succeeded` 改成 `failed`。

- [ ] **Step 3: 测试**

`runner_test.go` 用假的 `SessionStarter` / `ACP` / `Envs`，store 仍是 Postgres。

- 到期任务：starter 被调用一次，autonomy 传对；假 Prompt 里调用 store `FinishRun`；主会话出现一条摘要；run 为 `succeeded`。
- 已有 `running`：领取结果是 `skipped_overlap`，starter 不被调用。
- `next_run_at` 过期超过宽限：`skipped_stale`，starter 不被调用。
- 假 Prompt 阻塞到超时：run `failed`，且 `Cancel` 被调用。
- 负责人 `networkTier=none`：run `failed`，任务 `paused`。

不在这个测试里打真实 LLM。

Run: `go test ./internal/routine/ -count=1`

- [ ] **Step 4: 装配**

`main.go` 在 HTTP 监听的 goroutine 旁边启动 `runner.Loop(ctx)`。`Tick` 15s。`Notify` 先传 nil，Task 7 接上。进程退出时 `ctx` 取消，loop 返回。

- [ ] **Step 5: Commit**

```bash
git add internal/routine/prompt.go internal/routine/runner.go internal/routine/runner_test.go internal/api/agentapi/handler.go cmd/roundpend/main.go
git commit -m "$(cat <<'EOF'
Wake standing routines on schedule without using the primary chat.

EOF
)"
```

---

### Task 7: 确认闸与 IM 推送

**Files:**
- Modify: `internal/routine/runner.go`
- Modify: `internal/routine/http.go`（confirm / finish 已在 Task 5 落路由的，这里补恢复）
- Create: `internal/imconnect/notify.go`
- Modify: `internal/imconnect/supervisor.go`（导出按助手取 engine 的方法，若还没有）
- Modify: `cmd/roundpend/main.go`

- [ ] **Step 1: 协助单恢复**

用户处理协助单的现有路径在 `assistticket` 的 resolve。不要在票据包里 import `routine`。在 `agentapi` 或负责 resolve 的 handler 上加可选钩子 `OnTicketResolved(ticket)`，`main.go` 接到 runner：

- 找到 `assist_ticket_id` 等于这张单、状态 `waiting_user` 的 run。
- resolution 是拒绝：run `failed`，摘要用 resolution note，然后 `deliver`。
- resolution 是继续（`allow_once` 或等价的「继续」）：`Resume`，用 `budget_left_sec` 作为这次的时限，对**同一** `session_id` `ACP.Start`（若 runtime 已停）并 `Prompt`：「用户已确认继续。确认说明：…。不可逆动作仍然要再停一次。」会话历史还在，所以模型接着做，而不是新开一条任务。
- 调度器每轮另外查询 `waiting_user` 且 `started_at + 已用时间` 的等待起点超过 24 小时的 run（在进入 `waiting_user` 时写 `finished_at` 为空、用 run 上新增的 `wait_started_at`，或直接用 `updated_at`——实现时给 `routine_runs` 加 `wait_started_at TIMESTAMPTZ`，在 Task 1 的 DDL 里补上，不要另开迁移文件如果 Task 1 还没提交到主分支；若 Task 1 已提交，追加 `migrations/0023_routine_run_wait.sql` 和 schema 里的 `ADD COLUMN IF NOT EXISTS`）。超时则 `failed`，摘要「等待确认超过 24 小时」，任务仍为 `active`。

等待期间 `execute` 的墙钟 ctx 必须已经结束（`RequestRoutineConfirm` 返回后，模型按提示词停止；若 Prompt 还没返回，runner 在看到 `waiting_user` 时取消超时计时器但不 Cancel 会话，让这一轮自行结束）。实现时以「看到状态变成 `waiting_user` 就停止本轮计时」为准，不要在等待中把 run 打成超时失败。

- [ ] **Step 2: IM**

`Supervisor.Notify(ctx, assistantID, text string) error`：

- 没有该助手的 engine → 返回错误（调用方写入 `deliver_error`）。
- 读该助手 cc-connect 的 session store（`DataDir/<assistantID>/sessions.json`，类型用 cc-connect 已有的 session manager 加载，不手写解析）。
- 对每个已启用平台，取最近一个 sessionKey，断言平台实现 `core.ReplyContextReconstructor`，`ReconstructReplyCtx` 后 `Platform.Send`。这是 cc-connect 给「没有入站消息也要发」留的接口（`core/interfaces.go`）。
- 某个平台失败不影响其它平台；全部失败才把错误返回。从未有过 IM 对话（没有 sessionKey）时返回明确错误，不新建聊天。

`main.go` 把 `Notify` 接到 runner。`Notify` 在 IM 未配置时保持 nil。

- [ ] **Step 3: 测试**

- store 测试：`waiting_user` 不会被 `ClaimDue` 当成可以再开一条（唯一索引 + overlap）。
- notify 测试：假的 `ReplyContextReconstructor` 平台收到文本。没有 sessionKey 时返回错误、不 panic。
- runner 测试：假票据回调用「拒绝」后 run 为 `failed` 且主会话有摘要。

Run: `go test ./internal/routine/ ./internal/imconnect/ -count=1`

- [ ] **Step 4: Commit**

```bash
git add internal/routine/ internal/imconnect/notify.go internal/imconnect/supervisor.go cmd/roundpend/main.go internal/storage/schema/postgres.sql migrations/
git commit -m "$(cat <<'EOF'
Pause a routine run for confirmation and push the summary to IM.

EOF
)"
```

---

### Task 8: 停用助手时暂停任务

**Files:**
- Modify: `internal/assistant/http.go`
- Modify: `internal/assistant/http` 的现有测试，或 `internal/routine/store_test.go` 里已有 `PauseByAssignee`

- [ ] **Step 1: 钩子**

`assistant.Handler` 增加 `OnDisabled func(ctx, assistantID)`。`Update` 把状态写成 `disabled` 成功后调用。`main.go` 设为 `routineStore.PauseByAssignee`。

不要在 `assistant` 包里 import `routine`。

系统助手不能停用（现有 `ErrSystemUndeletable`），这条路径走不到它。

- [ ] **Step 2: 测试**

助手 PATCH 成 disabled 后，它名下 `active` 任务变成 `paused`，`next_run_at` 为空。再把助手开回 active，任务仍是 `paused`。

Run: `go test ./internal/assistant/ ./internal/routine/ -count=1`

- [ ] **Step 3: Commit**

```bash
git add internal/assistant/http.go cmd/roundpend/main.go internal/routine/store_test.go
git commit -m "$(cat <<'EOF'
Pause standing routines when their assistant is disabled.

EOF
)"
```

---

### Task 9: 控制台

**Files:**
- Create: `web/src/api/routines.ts`
- Modify: `web/src/pages/AssistantDetailPage.tsx`
- Create: `web/src/pages/RoutineDetailPage.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/i18n/en.ts`、`web/src/i18n/zh_CN.ts`

不新增一级导航。文案用 i18n 键，中英都加。助手详情现有分区是硬编码中文（「基本信息」「此刻」）；本分区跟该页一样写中文，键仍放进 i18n，页面引用键。不要只改一种语言。

- [ ] **Step 1: API 客户端**

对照 `web/src/api/issues.ts`。类型字段与 Go json 一致：`key`、`title`、`brief`、`autonomy`、`hosts`、`cron`、`timezone`、`deliverIm`、`status`、`nextRunAt`、`assigneeAssistantId`、runs 的 `status` / `summary` / `scheduledAt`。

- [ ] **Step 2: 助手详情分区**

插在「此刻」后面。列表：`RTN-n`、标题、下次运行（本地时区显示）、状态、最近一条摘要。空态说明可以在对话里说「每周一早上把 AI Agent 论文发成周报」。

新建 / 编辑用 Semi `Modal` + `Form`：

- 标题、说明
- 预设：每周一 08:00 → `0 8 * * 1`；每月 1 日 09:00 → `0 9 1 * *`；自定义 cron
- 时区默认 `Intl.DateTimeFormat().resolvedOptions().timeZone`
- 自主级别 `read` / `browse`；`browse` 时主机用逗号或换行拆开
- IM 开关
- 负责人下拉：当前用户的助手，默认本页这位

暂停、继续（`paused` ↔ `active`）、归档（确认后再 PATCH `archived`）。

- [ ] **Step 3: 运行详情**

路由 `/a/:assistantId/routines/:key`，放在 `AssistantLayout` 下，与 `AssistantDetailPage` 同级。展示说明、负责人、cron、只读 state、运行表（时间、状态、摘要）。进行中链到 `/a/:assistantId/s/:sessionId`。`waiting_user` 链到该助手详情里已有的协助单区域（票据 id 在 run 上，详情页能滚动到或列出这张单）。取消按钮调用 cancel API。

- [ ] **Step 4: 类型检查**

Run: `cd web && npx tsc --noEmit`

- [ ] **Step 5: Commit**

```bash
git add web/src/api/routines.ts web/src/pages/AssistantDetailPage.tsx web/src/pages/RoutineDetailPage.tsx web/src/App.tsx web/src/i18n/en.ts web/src/i18n/zh_CN.ts
git commit -m "$(cat <<'EOF'
Show standing routines on the assistant that owns them.

EOF
)"
```

---

### Task 10: e2e 与 README

**Files:**
- Create: `web/e2e/routines.spec.ts`
- Modify: `README.md`

- [ ] **Step 1: e2e**

沿用 `web/e2e/helpers.ts` 的登录。浏览器走仓库现有的 `PLAYWRIGHT_CDP_ENDPOINT`（远程 browserless），不要 `npx playwright install`。

```ts
test('create a weekly routine from the assistant page', async ({ page }) => {
  await loginAsAdmin(page)
  await page.goto('/a')
  // 进入系统助手或列表里的第一个助手详情，选择器以 AssistantDetailPage 实际标题为准
  await page.getByRole('button', { name: /新建常驻任务/ }).click()
  await page.getByLabel('标题').fill('论文周报')
  await page.getByLabel('说明').fill('汇总过去七天的 AI Agent 论文')
  await page.getByRole('button', { name: /创建/ }).click()
  await expect(page.getByText(/RTN-/)).toBeVisible()
})
```

选择器以 Task 9 落地的 label 为准。不断言真实到点跑模型。

- [ ] **Step 2: 跑 e2e**

Run: `cd web && npx playwright test e2e/routines.spec.ts`

栈没起来或没设 `PLAYWRIGHT_CDP_ENDPOINT` 时按 `web/e2e` 里既有约定 skip，提交说明里写原因。

- [ ] **Step 3: README**

「核心能力」加一条：对话里把反复要做的事交给一个助手（`RTN-n`），到点自行运行（`RUN-n`），结果进主对话和 IM。助手详情可看。抢券和报税依赖后续的登录态，本期不宣称能做完。

- [ ] **Step 4: Commit**

```bash
git add web/e2e/routines.spec.ts README.md
git commit -m "$(cat <<'EOF'
Cover standing-routine creation in the console and note it in the README.

EOF
)"
```

---

## Plan self-review

| Spec | 落地 |
|------|------|
| §3.1 唯一负责人、创建时能力检查、停用后暂停且不自动恢复 | Task 3、Task 8 |
| §3.1 改分配不影响进行中的 run | Task 2 `Update` |
| §3.2 运行→会话、可选议题；无任务依赖 | Task 2 的 `issue_id`；依赖不实现 |
| §3.3 环境是用户槽位；sysadmin 不另开容器 | Task 6 `EnsureAgent` / `EnsureBrowser` + `StartRoutine` |
| §4 不擅自创建、改派要确认 | Task 5 提示词 + `assigneeConfirmed` |
| §5 cron、时区、最小间隔、补跑一次、不重叠、领取锁 | Task 1、Task 2 `ClaimDue` |
| §5 运行会话不是主对话；主对话一条摘要 | Task 4、Task 6 `AddMessage`（不 `Prompt` 主会话） |
| §5 `max_duration` | Task 6 |
| §6 `read` / `browse` 收紧；主机不能放宽助手网络 | Task 5 不注册工具；网络档仍走现有 policy（工具调用路径不变） |
| §6 支付 / 提交 / 删除 / 对外发送必须停下 | Task 5 `RequestRoutineConfirm` + Task 7 |
| §7 IM 失败不失败整次运行；state 64KB | Task 6 `deliver_error`、Task 2 |
| §8 短 ID、无硬删除、`archived` | Task 1、Task 2 |
| §9 API | Task 3 |
| §10 工具不直连 DB；来源从会话反查 | Task 5 |
| §11 助手详情，无一级导航 | Task 9 |
| §14 周报验收；凭据库不做 | Task 6 的假 Prompt 测试 + Task 10 的创建 e2e。端到端打真实模型和真实周一早晨不在本计划里自动化 |
| §15 非目标 | 无对应任务 |

实现时不要做的：按 bio 选助手、凭据库、Cookie 持久化、任务依赖、邮件、秒级 cron、在容器里放 crontab。
