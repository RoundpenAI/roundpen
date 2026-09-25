# 议题与任务跟踪 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让「要做的事」成为一等实体：对话中由助手创建**议题**（`ISS-n`），澄清边界/方向/决策后写版本化的 **Spec / Plan 文档**（`DOC-n`），拆成**任务清单**（`TSK-n`）逐个实现；控制台可读、可改、可跟踪，全部数据在 PostgreSQL。

**Architecture:** 新增 `internal/issue` 包（domain + store + http），三张表 `issues` / `tasks` / `issue_docs` 落在既有 `migrations/NNNN.sql` 与 `internal/storage/schema/postgres.sql`。状态机是纯函数（`Advance`），自动流转发生在 store 层的事务内。Agent 侧注册 8 个工具，全部经 `RoundpenHTTP` 调 REST API（不直连 DB），规范写进系统提示词；auto mode 的 `Allow` 增加一条记录类规则。控制台加一个一级导航与列表/详情两页。

**Tech Stack:** Go 1.22+（`net/http` ServeMux、`database/sql` + pgx）、PostgreSQL（`CREATE SEQUENCE` + `nextval` 生成短 ID）、React 19 + Vite + Semi Design（`MarkdownRender` 渲染文档，无新依赖）、Playwright e2e。

**Spec:** `docs/superpowers/specs/2026-09-20-issue-task-tracking-design.md`

**分支：** 建议在独立分支 `feat/issue-tracker` 上执行。

**Follow-on plans（本文件不实现）：**

| Plan | Scope |
|------|-------|
| 2 | 议题事件流 / 审计时间线（谁在何时改了什么） |
| 3 | 对话内议题卡片与「此刻」进度聚合 |
| 4 | 优先级 / 标签 / 跨议题依赖 |

---

## File map (Plan 1)

| Path | Responsibility |
|------|----------------|
| `migrations/0018_issues.sql` | 三张表 + 三个 key 序列（历史记录） |
| `internal/storage/schema/postgres.sql` | 同样的 DDL，幂等块（实际生效的 schema） |
| `internal/issue/issue.go` | 领域类型、枚举、状态机 `Advance` |
| `internal/issue/issue_test.go` | 状态机与校验的纯单元测试 |
| `internal/issue/store.go` | CRUD + key 生成 + 文档版本化 + 自动流转 |
| `internal/issue/store_test.go` | Store 测试（Postgres） |
| `internal/issue/http.go` | `/v1/issues`、`/v1/issues/{key}/docs`、`/v1/issues/{key}/tasks`、`/v1/tasks/{key}` |
| `internal/issue/http_test.go` | Handler 校验与错误码测试 |
| `cmd/roundpend/main.go` | 装配并挂载 issue handler |
| `internal/acp/sysagent/tools/issues.go` | 8 个工具（CreateIssue…UpdateTask） |
| `internal/acp/sysagent/tools/registry_surface_test.go` | 工具面测试补充 |
| `internal/acp/manager/manager.go` | 注册 `RegisterIssues` |
| `internal/acp/sysagent/history.go` | 系统提示词加「议题工作流」规范 |
| `internal/automode/rules.go` | `Allow` 增加记录类规则 |
| `internal/automode/rules_test.go` | 断言该规则存在 |
| `web/src/api.ts` | `Issue` / `IssueDoc` / `IssueTask` 类型 + `issues` 命名空间 |
| `web/src/lib/appNav.ts` | 一级导航「议题」+ `matchPrimaryMenu` |
| `web/src/i18n/en.ts`、`web/src/i18n/zh_CN.ts` | 文案键 |
| `web/src/App.tsx` | 路由 `/issues`、`/issues/:key` |
| `web/src/pages/IssuesPage.tsx` | 列表 + 筛选 + 新建 |
| `web/src/pages/IssueDetailPage.tsx` | 左文档（版本切换）/ 右任务清单 |
| `web/e2e/issues.spec.ts` | 控制台闭环 smoke |

---

### Task 1: Migration — `issues` / `tasks` / `issue_docs`

**Files:**
- Create: `migrations/0018_issues.sql`
- Modify: `internal/storage/schema/postgres.sql`（追加同样内容到文件末尾）

- [ ] **Step 1: 写迁移文件**

```sql
-- Issues & tasks: durable work items created in conversation, with versioned spec/plan docs.
-- Short keys (ISS-12 / TSK-34 / DOC-88) come from per-install sequences so humans and the
-- agent can reference work items by name; uuid stays the primary key like other tables.

CREATE SEQUENCE IF NOT EXISTS issue_key_seq;
CREATE SEQUENCE IF NOT EXISTS task_key_seq;
CREATE SEQUENCE IF NOT EXISTS issue_doc_key_seq;

CREATE TABLE IF NOT EXISTS issues (
    id           TEXT PRIMARY KEY,
    key          TEXT NOT NULL UNIQUE,
    user_id      TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    assistant_id TEXT REFERENCES assistants (id) ON DELETE SET NULL,
    session_id   TEXT NOT NULL DEFAULT '',
    title        TEXT NOT NULL,
    summary      TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'drafting'
        CHECK (status IN ('drafting', 'specced', 'planned', 'in_progress', 'done', 'cancelled')),
    origin       TEXT NOT NULL DEFAULT 'chat'
        CHECK (origin IN ('chat', 'console')),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at    TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS issues_user_status_idx ON issues (user_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS issues_assistant_idx ON issues (assistant_id, updated_at DESC);
CREATE INDEX IF NOT EXISTS issues_session_idx ON issues (session_id) WHERE session_id <> '';

CREATE TABLE IF NOT EXISTS tasks (
    id           TEXT PRIMARY KEY,
    key          TEXT NOT NULL UNIQUE,
    issue_id     TEXT NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    user_id      TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    position     INTEGER NOT NULL DEFAULT 0,
    title        TEXT NOT NULL,
    detail       TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT 'todo'
        CHECK (status IN ('todo', 'in_progress', 'done', 'blocked', 'cancelled')),
    session_id   TEXT NOT NULL DEFAULT '',
    assistant_id TEXT REFERENCES assistants (id) ON DELETE SET NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    done_at      TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS tasks_issue_position_idx ON tasks (issue_id, position, created_at);
CREATE INDEX IF NOT EXISTS tasks_user_status_idx ON tasks (user_id, status, updated_at DESC);
CREATE INDEX IF NOT EXISTS tasks_session_idx ON tasks (session_id) WHERE session_id <> '';

CREATE TABLE IF NOT EXISTS issue_docs (
    id           TEXT PRIMARY KEY,
    key          TEXT NOT NULL UNIQUE,
    issue_id     TEXT NOT NULL REFERENCES issues (id) ON DELETE CASCADE,
    task_id      TEXT REFERENCES tasks (id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('spec', 'plan')),
    version      INTEGER NOT NULL,
    status       TEXT NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft', 'current', 'superseded')),
    title        TEXT NOT NULL DEFAULT '',
    content_md   TEXT NOT NULL,
    author_type  TEXT NOT NULL DEFAULT 'assistant' CHECK (author_type IN ('user', 'assistant')),
    assistant_id TEXT REFERENCES assistants (id) ON DELETE SET NULL,
    session_id   TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (issue_id, kind, version)
);
CREATE INDEX IF NOT EXISTS issue_docs_issue_kind_idx ON issue_docs (issue_id, kind, version DESC);
CREATE INDEX IF NOT EXISTS issue_docs_task_idx ON issue_docs (task_id) WHERE task_id IS NOT NULL;

-- tasks.plan_doc_id -> issue_docs(id): added after both tables exist (circular reference).
-- ADD COLUMN IF NOT EXISTS keeps the embedded schema re-executable on every boot.
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS plan_doc_id TEXT REFERENCES issue_docs (id) ON DELETE SET NULL;
```

- [ ] **Step 2: 同步到嵌入式 schema**

把上面**同样的内容**（去掉首行 `--` 注释可保留）追加到 `internal/storage/schema/postgres.sql` 末尾。这是实际生效的 schema：`MigrateEmbedded` 每次启动整文件重放，所以每条语句必须幂等（`IF NOT EXISTS` / `ADD COLUMN IF NOT EXISTS`）。

注意顺序约束：`tasks.plan_doc_id` 必须用 `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` 加在 `issue_docs` 建表之后（`tasks` 与 `issue_docs` 互相引用，不能在建表语句里内联）。

- [ ] **Step 3: 验证幂等**

```bash
# 需要可用的 DATABASE_URL
go run ./cmd/roundpend --help >/dev/null 2>&1 || true
psql "$DATABASE_URL" -f internal/storage/schema/postgres.sql
psql "$DATABASE_URL" -f internal/storage/schema/postgres.sql   # 第二次必须也成功
psql "$DATABASE_URL" -c "\d tasks" | grep plan_doc_id
```

Expected: 两条 `psql` 都成功（无重复对象报错）；`plan_doc_id` 列存在。

- [ ] **Step 4: Commit**

```bash
git add migrations/0018_issues.sql internal/storage/schema/postgres.sql
git commit -m "Add issues, tasks and versioned issue docs tables."
```

---

### Task 2: 领域类型 + 状态机

**Files:**
- Create: `internal/issue/issue.go`
- Test: `internal/issue/issue_test.go`

- [ ] **Step 1: 写失败测试**

```go
package issue

import "testing"

func TestAdvance_ForwardOnly(t *testing.T) {
	cases := []struct {
		cur  string
		ev   Event
		want string
	}{
		{StatusDrafting, EventSpecCurrent, StatusSpecced},
		{StatusSpecced, EventSpecCurrent, StatusSpecced},   // 再写一版 spec 不回退
		{StatusDrafting, EventPlanCurrent, StatusPlanned},  // 没写 spec 也能前进
		{StatusSpecced, EventPlanCurrent, StatusPlanned},
		{StatusPlanned, EventTaskOpen, StatusInProgress},
		{StatusDone, EventTaskOpen, StatusInProgress},       // 任务重开，议题回到进行中
		{StatusInProgress, EventAllTasksDone, StatusDone},
		{StatusPlanned, EventAllTasksDone, StatusPlanned},   // 还没开工不算完成
		{StatusCancelled, EventSpecCurrent, StatusCancelled}, // 取消后不自动复活
		{StatusCancelled, EventAllTasksDone, StatusCancelled},
	}
	for _, tc := range cases {
		if got := Advance(tc.cur, tc.ev); got != tc.want {
			t.Errorf("Advance(%s, %s) = %s, want %s", tc.cur, tc.ev, got, tc.want)
		}
	}
}

func TestAllTasksDone(t *testing.T) {
	if AllTasksDone(nil) {
		t.Fatal("no tasks must not count as done")
	}
	if AllTasksDone([]Task{{Status: TaskCancelled}, {Status: TaskCancelled}}) {
		t.Fatal("all-cancelled must not count as done — issue stays in_progress for the user")
	}
	if !AllTasksDone([]Task{{Status: TaskDone}, {Status: TaskCancelled}}) {
		t.Fatal("done + cancelled counts as done")
	}
	if AllTasksDone([]Task{{Status: TaskDone}, {Status: TaskTodo}}) {
		t.Fatal("a todo task blocks completion")
	}
}

func TestValidateInputs(t *testing.T) {
	if err := ValidateTitle("  "); err == nil {
		t.Fatal("blank title must fail")
	}
	if err := ValidateKind("notes"); err == nil {
		t.Fatal("kind must be spec|plan")
	}
	if err := ValidateDocStatus("weird"); err == nil {
		t.Fatal("doc status must be draft|current|superseded")
	}
}
```

- [ ] **Step 2: 跑测试确认失败**

Run: `go test ./internal/issue/ -count=1`
Expected: FAIL（包不存在）

- [ ] **Step 3: 实现 `issue.go`**

```go
// Package issue models the user's work tracker: issues, their versioned spec/plan
// documents, and the tasks derived from those plans.
package issue

import (
	"fmt"
	"strings"
	"time"
)

const (
	StatusDrafting   = "drafting"
	StatusSpecced    = "specced"
	StatusPlanned    = "planned"
	StatusInProgress = "in_progress"
	StatusDone       = "done"
	StatusCancelled  = "cancelled"
)

const (
	TaskTodo       = "todo"
	TaskInProgress = "in_progress"
	TaskDone       = "done"
	TaskBlocked    = "blocked"
	TaskCancelled  = "cancelled"
)

const (
	DocSpec = "spec"
	DocPlan = "plan"
)

const (
	DocDraft      = "draft"
	DocCurrent    = "current"
	DocSuperseded = "superseded"
)

const (
	OriginChat    = "chat"
	OriginConsole = "console"
)

// Event drives automatic issue status advance from document and task writes.
type Event string

const (
	EventSpecCurrent  Event = "spec_current"   // spec 写入 current
	EventPlanCurrent  Event = "plan_current"   // plan 写入 current
	EventTaskOpen     Event = "task_open"      // 任务进入 in_progress，或被重新打开
	EventAllTasksDone Event = "all_tasks_done" // 全部任务 ∈ {done,cancelled} 且 ≥1 done
)

type Issue struct {
	ID          string     `json:"id"`
	Key         string     `json:"key"`
	UserID      string     `json:"userId"`
	AssistantID string     `json:"assistantId,omitempty"`
	SessionID   string     `json:"sessionId,omitempty"`
	Title       string     `json:"title"`
	Summary     string     `json:"summary"`
	Status      string     `json:"status"`
	Origin      string     `json:"origin"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	ClosedAt    *time.Time `json:"closedAt,omitempty"`
}

type Doc struct {
	ID          string    `json:"id"`
	Key         string    `json:"key"`
	IssueID     string    `json:"issueId"`
	TaskID      string    `json:"taskId,omitempty"`
	Kind        string    `json:"kind"`
	Version     int       `json:"version"`
	Status      string    `json:"status"`
	Title       string    `json:"title"`
	ContentMD   string    `json:"contentMd,omitempty"` // 列表查询不返回正文，见 ListDocs
	AuthorType  string    `json:"authorType"`
	AssistantID string    `json:"assistantId,omitempty"`
	SessionID   string    `json:"sessionId,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type Task struct {
	ID          string     `json:"id"`
	Key         string     `json:"key"`
	IssueID     string     `json:"issueId"`
	PlanDocID   string     `json:"planDocId,omitempty"`
	Position    int        `json:"position"`
	Title       string     `json:"title"`
	Detail      string     `json:"detail"`
	Status      string     `json:"status"`
	SessionID   string     `json:"sessionId,omitempty"`
	AssistantID string     `json:"assistantId,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	DoneAt      *time.Time `json:"doneAt,omitempty"`
}

// Advance returns the issue status after ev. Forward-only: a rule that does not
// apply leaves the status unchanged, and a cancelled issue never auto-revives.
func Advance(cur string, ev Event) string {
	if cur == StatusCancelled {
		return cur
	}
	switch ev {
	case EventSpecCurrent:
		if cur == StatusDrafting {
			return StatusSpecced
		}
	case EventPlanCurrent:
		if cur == StatusDrafting || cur == StatusSpecced {
			return StatusPlanned
		}
	case EventTaskOpen:
		if cur != StatusInProgress {
			return StatusInProgress
		}
	case EventAllTasksDone:
		if cur == StatusInProgress {
			return StatusDone
		}
	}
	return cur
}

// AllTasksDone reports whether the task list satisfies the completion rule:
// at least one done, and nothing left in todo/in_progress/blocked. An issue whose
// tasks were all cancelled stays in_progress for the user to decide.
func AllTasksDone(tasks []Task) bool {
	if len(tasks) == 0 {
		return false
	}
	done := 0
	for _, t := range tasks {
		switch t.Status {
		case TaskDone:
			done++
		case TaskCancelled:
		default:
			return false
		}
	}
	return done > 0
}

func ValidateTitle(title string) error {
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("title is required")
	}
	return nil
}

func ValidateKind(kind string) error {
	if kind != DocSpec && kind != DocPlan {
		return fmt.Errorf("kind must be spec or plan")
	}
	return nil
}

func ValidateDocStatus(status string) error {
	switch status {
	case DocDraft, DocCurrent, DocSuperseded:
		return nil
	}
	return fmt.Errorf("status must be draft, current or superseded")
}

func ValidateTaskStatus(status string) error {
	switch status {
	case TaskTodo, TaskInProgress, TaskDone, TaskBlocked, TaskCancelled:
		return nil
	}
	return fmt.Errorf("status must be todo, in_progress, done, blocked or cancelled")
}

func ValidateIssueStatus(status string) error {
	switch status {
	case StatusDrafting, StatusSpecced, StatusPlanned, StatusInProgress, StatusDone, StatusCancelled:
		return nil
	}
	return fmt.Errorf("invalid issue status %q", status)
}
```

- [ ] **Step 4: 跑测试确认通过**

Run: `go test ./internal/issue/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/issue/issue.go internal/issue/issue_test.go
git commit -m "Add issue domain types and forward-only status machine."
```

---

### Task 3: Store — key 生成、文档版本化、自动流转

**Files:**
- Create: `internal/issue/store.go`
- Test: `internal/issue/store_test.go`

测试用既有约定：`ROUNDPEN_TEST_DATABASE_URL` + `MigrateEmbedded`（见 `internal/assistant/store_test.go` 的 DB setup）。

- [ ] **Step 1: 写 `store.go` 骨架与创建路径**

```go
package issue

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("issue not found")

type Store struct {
	DB *sql.DB
}

const issueCols = `id, key, user_id, COALESCE(assistant_id, ''), session_id, title, summary,
	status, origin, created_at, updated_at, closed_at`

const taskCols = `id, key, issue_id, COALESCE(plan_doc_id, ''), position, title, detail,
	status, session_id, COALESCE(assistant_id, ''), created_at, updated_at, done_at`

// docCols omits content_md: list queries return indexes, not bodies.
const docCols = `id, key, issue_id, COALESCE(task_id, ''), kind, version, status, title,
	author_type, COALESCE(assistant_id, ''), session_id, created_at`

type CreateInput struct {
	Title       string
	Summary     string
	AssistantID string
	SessionID   string
	Origin      string
}

// Create inserts an issue, assigning its short key from issue_key_seq in the same
// statement so concurrent creates cannot collide.
func (s *Store) Create(ctx context.Context, userID string, in CreateInput) (*Issue, error) {
	if err := ValidateTitle(in.Title); err != nil {
		return nil, err
	}
	origin := in.Origin
	if origin == "" {
		origin = OriginChat
	}
	now := time.Now().UTC()
	it := &Issue{
		ID: uuid.NewString(), UserID: userID,
		AssistantID: in.AssistantID, SessionID: in.SessionID,
		Title: strings.TrimSpace(in.Title), Summary: strings.TrimSpace(in.Summary),
		Status: StatusDrafting, Origin: origin, CreatedAt: now, UpdatedAt: now,
	}
	err := s.DB.QueryRowContext(ctx, `
		INSERT INTO issues (id, key, user_id, assistant_id, session_id, title, summary, status, origin, created_at, updated_at)
		VALUES ($1, 'ISS-' || nextval('issue_key_seq'), $2, NULLIF($3, ''), $4, $5, $6, $7, $8, $9, $9)
		RETURNING key`,
		it.ID, userID, it.AssistantID, it.SessionID, it.Title, it.Summary, it.Status, it.Origin, now,
	).Scan(&it.Key)
	if err != nil {
		return nil, err
	}
	return it, nil
}

// resolveIssueID locks the issue row and returns its id. Used inside write
// transactions so per-issue version numbers and status transitions serialize.
func resolveIssueID(ctx context.Context, tx *sql.Tx, userID, keyOrID string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx,
		`SELECT id FROM issues WHERE (id = $2 OR key = $2) AND user_id = $1 FOR UPDATE`,
		userID, keyOrID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}
```

- [ ] **Step 2: 写 Store 测试（先失败）**

```go
func TestStore_IssueLifecycle(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	user := "issue-test-user" // 先确保用户存在（见既有 store_test 的建库/建用户 helper）

	it, err := st.Create(ctx, user, CreateInput{Title: "议题跟踪系统", Summary: "跟踪器"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(it.Key, "ISS-") {
		t.Fatalf("want ISS- key, got %q", it.Key)
	}
	if it.Status != StatusDrafting {
		t.Fatalf("new issue must start drafting, got %s", it.Status)
	}

	// spec 写入 current → specced
	doc, err := st.WriteDoc(ctx, user, it.Key, DocInput{Kind: DocSpec, ContentMD: "# Spec v1", Status: DocCurrent})
	if err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || doc.Status != DocCurrent {
		t.Fatalf("want v1 current, got v%d %s", doc.Version, doc.Status)
	}
	got, _, _, err := st.Get(ctx, user, it.Key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusSpecced {
		t.Fatalf("spec current must advance to specced, got %s", got.Status)
	}

	// spec v2 → v1 superseded，状态保持 specced
	doc2, err := st.WriteDoc(ctx, user, it.Key, DocInput{Kind: DocSpec, ContentMD: "# Spec v2", Status: DocCurrent})
	if err != nil {
		t.Fatal(err)
	}
	if doc2.Version != 2 {
		t.Fatalf("want v2, got v%d", doc2.Version)
	}
	docs, err := st.ListDocs(ctx, user, it.Key)
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) != 2 || docs[0].Status != DocCurrent || docs[1].Status != DocSuperseded {
		t.Fatalf("want v2 current + v1 superseded, got %+v", docs)
	}
	for _, d := range docs {
		if d.ContentMD != "" {
			t.Fatalf("list must not carry bodies, got %q", d.ContentMD)
		}
	}

	// plan → planned；任务开工 → in_progress；全部完成 → done
	if _, err := st.WriteDoc(ctx, user, it.Key, DocInput{Kind: DocPlan, ContentMD: "# Plan", Status: DocCurrent}); err != nil {
		t.Fatal(err)
	}
	tk, err := st.CreateTask(ctx, user, it.Key, TaskInput{Title: "加表", Position: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tk.Key, "TSK-") {
		t.Fatalf("want TSK- key, got %q", tk.Key)
	}
	if _, err := st.UpdateTask(ctx, user, tk.Key, TaskUpdateInput{Status: ptr(TaskInProgress)}); err != nil {
		t.Fatal(err)
	}
	got, _, _, _ = st.Get(ctx, user, it.Key)
	if got.Status != StatusInProgress {
		t.Fatalf("task start must advance to in_progress, got %s", got.Status)
	}
	if _, err := st.UpdateTask(ctx, user, tk.Key, TaskUpdateInput{Status: ptr(TaskDone)}); err != nil {
		t.Fatal(err)
	}
	got, _, tasks, _ := st.Get(ctx, user, it.Key)
	if got.Status != StatusDone || got.ClosedAt == nil {
		t.Fatalf("all tasks done must close the issue, got %s", got.Status)
	}
	if tasks[0].DoneAt == nil {
		t.Fatal("done task must carry done_at")
	}

	// 重开任务 → 回到 in_progress
	if _, err := st.UpdateTask(ctx, user, tk.Key, TaskUpdateInput{Status: ptr(TaskTodo)}); err != nil {
		t.Fatal(err)
	}
	got, _, _, _ = st.Get(ctx, user, it.Key)
	if got.Status != StatusInProgress || got.ClosedAt != nil {
		t.Fatalf("reopening a task must reopen the issue, got %s closed=%v", got.Status, got.ClosedAt)
	}
}

func TestStore_ScopedByUser(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	it, err := st.Create(ctx, "issue-user-a", CreateInput{Title: "A 的议题"})
	if err != nil {
		t.Fatal(err)
	}
	tk, err := st.CreateTask(ctx, "issue-user-a", it.Key, TaskInput{Title: "A 的任务"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := st.Get(ctx, "issue-user-b", it.Key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user read must be not found, got %v", err)
	}
	if _, err := st.UpdateTask(ctx, "issue-user-b", tk.Key, TaskUpdateInput{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user task update must be not found, got %v", err)
	}
	if _, err := st.WriteDoc(ctx, "issue-user-b", it.Key, DocInput{Kind: DocSpec, ContentMD: "x"}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user doc write must be not found, got %v", err)
	}
}
```

`ptr` 是测试内的小 helper：`func ptr[T any](v T) *T { return &v }`。

- [ ] **Step 3: 跑测试确认失败**

Run: `go test ./internal/issue/ -run TestStore -count=1`
Expected: FAIL（`WriteDoc` / `ListDocs` / `Get` / `CreateTask` / `UpdateTask` 未定义）

- [ ] **Step 4: 实现读路径**

```go
func (s *Store) Get(ctx context.Context, userID, keyOrID string) (*Issue, []Doc, []Task, error) {
	it, err := s.getIssue(ctx, userID, keyOrID)
	if err != nil {
		return nil, nil, nil, err
	}
	docs, err := s.ListDocs(ctx, userID, keyOrID)
	if err != nil {
		return nil, nil, nil, err
	}
	tasks, err := s.ListTasks(ctx, userID, keyOrID)
	if err != nil {
		return nil, nil, nil, err
	}
	return it, docs, tasks, nil
}

func (s *Store) getIssue(ctx context.Context, userID, keyOrID string) (*Issue, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT `+issueCols+` FROM issues WHERE (id = $2 OR key = $2) AND user_id = $1`,
		userID, keyOrID)
	return scanIssue(row)
}

type ListFilter struct {
	Status string
	Limit  int
}

func (s *Store) List(ctx context.Context, userID string, f ListFilter) ([]Issue, error) {
	limit := f.Limit
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT `+issueCols+` FROM issues
		WHERE user_id = $1 AND ($2 = '' OR status = $2)
		ORDER BY updated_at DESC LIMIT $3`, userID, f.Status, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Issue{}
	for rows.Next() {
		it, err := scanIssue(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *it)
	}
	return out, rows.Err()
}

// ListDocs returns the issue's document index, newest version first. Bodies are
// deliberately omitted — read them with GetDoc.
func (s *Store) ListDocs(ctx context.Context, userID, keyOrID string) ([]Doc, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT `+docCols+` FROM issue_docs
		WHERE issue_id = (SELECT id FROM issues WHERE (id = $2 OR key = $2) AND user_id = $1)
		  AND ($3 = '' OR kind = $3)
		ORDER BY kind, version DESC`, userID, keyOrID, "")
	...
}

// GetDoc resolves version <= 0 to the current version (falling back to the
// newest), and accepts DOC-n, a uuid, or "latest" as docKey.
func (s *Store) GetDoc(ctx context.Context, userID, keyOrID, docKey string, version int) (*Doc, error)
```

`GetDoc` 的实现：把 `docCols + ", content_md"` 加上正文；`docKey` 为 `latest`/空时按 `status='current'` 取，若无 current 则取最大 version。

- [ ] **Step 5: 实现写路径（版本化 + 自动流转）**

```go
type DocInput struct {
	Kind        string
	Title       string
	ContentMD   string
	Status      string // 缺省 current
	TaskKey     string
	AuthorType  string // 缺省 assistant
	AssistantID string
	SessionID   string
}

// WriteDoc appends a new version of the issue's spec or plan. Writing a current
// version supersedes the previous one and advances the issue status, all in one
// transaction so readers never see two current versions.
func (s *Store) WriteDoc(ctx context.Context, userID, keyOrID string, in DocInput) (*Doc, error) {
	if err := ValidateKind(in.Kind); err != nil {
		return nil, err
	}
	if strings.TrimSpace(in.ContentMD) == "" {
		return nil, fmt.Errorf("contentMd is required")
	}
	status := in.Status
	if status == "" {
		status = DocCurrent
	}
	if err := ValidateDocStatus(status); err != nil {
		return nil, err
	}
	authorType := in.AuthorType
	if authorType == "" {
		authorType = "assistant"
	}

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	issueID, err := resolveIssueID(ctx, tx, userID, keyOrID)
	if err != nil {
		return nil, err
	}
	var taskID string
	if in.TaskKey != "" {
		if err := tx.QueryRowContext(ctx, `
			SELECT id FROM tasks WHERE (id = $2 OR key = $2) AND user_id = $1 AND issue_id = $3`,
			userID, in.TaskKey, issueID).Scan(&taskID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	}
	var version int
	if err := tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(version), 0) + 1 FROM issue_docs WHERE issue_id = $1 AND kind = $2`,
		issueID, in.Kind).Scan(&version); err != nil {
		return nil, err
	}
	if status == DocCurrent {
		if _, err := tx.ExecContext(ctx, `
			UPDATE issue_docs SET status = 'superseded'
			WHERE issue_id = $1 AND kind = $2 AND status = 'current'`, issueID, in.Kind); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	doc := &Doc{
		ID: uuid.NewString(), IssueID: issueID, TaskID: taskID, Kind: in.Kind,
		Version: version, Status: status, Title: strings.TrimSpace(in.Title),
		ContentMD: in.ContentMD, AuthorType: authorType,
		AssistantID: in.AssistantID, SessionID: in.SessionID, CreatedAt: now,
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO issue_docs (id, key, issue_id, task_id, kind, version, status, title,
			content_md, author_type, assistant_id, session_id, created_at)
		VALUES ($1, 'DOC-' || nextval('issue_doc_key_seq'), $2, NULLIF($3, ''), $4, $5, $6, $7, $8, $9,
			NULLIF($10, ''), $11, $12)
		RETURNING key`,
		doc.ID, issueID, taskID, in.Kind, version, status, doc.Title, in.ContentMD,
		authorType, in.AssistantID, in.SessionID, now).Scan(&doc.Key); err != nil {
		return nil, err
	}
	if status == DocCurrent {
		ev := EventSpecCurrent
		if in.Kind == DocPlan {
			ev = EventPlanCurrent
		}
		if err := advanceIssueStatus(ctx, tx, issueID, ev); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return doc, nil
}

// advanceIssueStatus re-reads the issue under the transaction's lock, applies the
// forward-only rule and writes the result (plus closed_at bookkeeping).
func advanceIssueStatus(ctx context.Context, tx *sql.Tx, issueID string, ev Event) error {
	var cur string
	if err := tx.QueryRowContext(ctx, `SELECT status FROM issues WHERE id = $1`, issueID).Scan(&cur); err != nil {
		return err
	}
	next := Advance(cur, ev)
	if next == cur {
		return nil
	}
	if next == StatusDone || next == StatusCancelled {
		_, err := tx.ExecContext(ctx, `
			UPDATE issues SET status = $2, updated_at = now(), closed_at = COALESCE(closed_at, now())
			WHERE id = $1`, issueID, next)
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE issues SET status = $2, updated_at = now(), closed_at = NULL WHERE id = $1`, issueID, next)
	return err
}
```

- [ ] **Step 6: 实现任务写路径**

```go
type TaskInput struct {
	Title      string
	Detail     string
	Position   int
	Status     string // 缺省 todo
	PlanDocKey string
	SessionID  string
	AssistantID string
}

func (s *Store) CreateTask(ctx context.Context, userID, keyOrID string, in TaskInput) (*Task, error) {
	if err := ValidateTitle(in.Title); err != nil {
		return nil, err
	}
	status := in.Status
	if status == "" {
		status = TaskTodo
	}
	if err := ValidateTaskStatus(status); err != nil {
		return nil, err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	issueID, err := resolveIssueID(ctx, tx, userID, keyOrID)
	if err != nil {
		return nil, err
	}
	var planDocID string
	if in.PlanDocKey != "" {
		if err := tx.QueryRowContext(ctx, `
			SELECT id FROM issue_docs
			WHERE (id = $2 OR key = $2) AND issue_id = $3`, userID, in.PlanDocKey, issueID).Scan(&planDocID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	}
	position := in.Position
	if position <= 0 { // 未指定时排到末尾
		if err := tx.QueryRowContext(ctx, `
			SELECT COALESCE(MAX(position), 0) + 1 FROM tasks WHERE issue_id = $1`, issueID).Scan(&position); err != nil {
			return nil, err
		}
	}
	now := time.Now().UTC()
	task := &Task{
		ID: uuid.NewString(), IssueID: issueID, PlanDocID: planDocID, Position: position,
		Title: strings.TrimSpace(in.Title), Detail: strings.TrimSpace(in.Detail), Status: status,
		SessionID: in.SessionID, AssistantID: in.AssistantID, CreatedAt: now, UpdatedAt: now,
	}
	if status == TaskDone {
		task.DoneAt = &now
	}
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO tasks (id, key, issue_id, user_id, plan_doc_id, position, title, detail, status,
			session_id, assistant_id, created_at, updated_at, done_at)
		VALUES ($1, 'TSK-' || nextval('task_key_seq'), $2, $3, NULLIF($4, ''), $5, $6, $7, $8,
			$9, NULLIF($10, ''), $11, $11, $12)
		RETURNING key`,
		task.ID, issueID, userID, planDocID, position, task.Title, task.Detail, status,
		in.SessionID, in.AssistantID, now, task.DoneAt).Scan(&task.Key); err != nil {
		return nil, err
	}
	if status != TaskTodo { // 直接建成 in_progress/done 也算开工 / 完成
		if err := advanceForTaskChange(ctx, tx, issueID, status); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return task, nil
}

type TaskUpdateInput struct {
	Status    *string
	Title     *string
	Detail    *string
	SessionID *string
}

func (s *Store) UpdateTask(ctx context.Context, userID, keyOrID string, in TaskUpdateInput) (*Task, error) {
	if in.Status != nil {
		if err := ValidateTaskStatus(*in.Status); err != nil {
			return nil, err
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	var issueID string
	task := &Task{}
	err = tx.QueryRowContext(ctx, `
		UPDATE tasks SET
			status     = COALESCE($3, status),
			title      = COALESCE($4, title),
			detail     = COALESCE($5, detail),
			session_id = COALESCE($6, session_id),
			updated_at = now(),
			done_at    = CASE
				WHEN COALESCE($3, status) = 'done' THEN COALESCE(done_at, now())
				WHEN COALESCE($3, status) = 'cancelled' THEN done_at
				ELSE NULL END
		WHERE (id = $2 OR key = $2) AND user_id = $1
		RETURNING `+taskCols,
		userID, keyOrID, in.Status, in.Title, in.Detail, in.SessionID).Scan(
		&task.ID, &task.Key, &issueID, &task.PlanDocID, &task.Position, &task.Title, &task.Detail,
		&task.Status, &task.SessionID, &task.AssistantID, &task.CreatedAt, &task.UpdatedAt, &task.DoneAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if in.Status != nil {
		if err := advanceForTaskChange(ctx, tx, issueID, *in.Status); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return task, nil
}

// advanceForTaskChange maps a task status write to an issue event: entering
// in_progress (or being reopened) means work is live; a terminal status means
// the completion rule may now hold.
func advanceForTaskChange(ctx context.Context, tx *sql.Tx, issueID, taskStatus string) error {
	switch taskStatus {
	case TaskInProgress, TaskTodo, TaskBlocked:
		return advanceIssueStatus(ctx, tx, issueID, EventTaskOpen)
	case TaskDone, TaskCancelled:
		all, err := listTasksTx(ctx, tx, issueID)
		if err != nil {
			return err
		}
		if AllTasksDone(all) {
			return advanceIssueStatus(ctx, tx, issueID, EventAllTasksDone)
		}
	}
	return nil
}
```

`listTasksTx` 与 `ListTasks` 共用同一段 select，只是接收 `*sql.Tx` 或 `*sql.DB`（提取一个 `queryer` 接口即可）。

`Update`（议题自身的 PATCH）：

```go
type UpdateInput struct {
	Status  *string
	Title   *string
	Summary *string
}

// Update applies an explicit change. The user (or console) has final say, so no
// status machine is applied here.
func (s *Store) Update(ctx context.Context, userID, keyOrID string, in UpdateInput) (*Issue, error) {
	if in.Status != nil {
		if err := ValidateIssueStatus(*in.Status); err != nil {
			return nil, err
		}
	}
	if in.Title != nil {
		if err := ValidateTitle(*in.Title); err != nil {
			return nil, err
		}
	}
	row := s.DB.QueryRowContext(ctx, `
		UPDATE issues SET
			status     = COALESCE($3, status),
			title      = COALESCE($4, title),
			summary    = COALESCE($5, summary),
			updated_at = now(),
			closed_at  = CASE
				WHEN COALESCE($3, status) IN ('done', 'cancelled') THEN COALESCE(closed_at, now())
				ELSE NULL END
		WHERE (id = $2 OR key = $2) AND user_id = $1
		RETURNING `+issueCols,
		userID, keyOrID, in.Status, in.Title, in.Summary)
	return scanIssue(row)
}
```

`scanIssue` / `scanTask` / `scanDoc` 用 `sql.NullString` / `sql.NullTime` 承接可空列，`sql.ErrNoRows` 映射为 `ErrNotFound`（照 `assistticket/store.go` 的写法）。

- [ ] **Step 7: 跑测试确认通过**

Run: `go test ./internal/issue/ -count=1`
Expected: PASS（未设 `ROUNDPEN_TEST_DATABASE_URL` 时 Store 测试按既有约定 skip）

- [ ] **Step 8: Commit**

```bash
git add internal/issue/store.go internal/issue/store_test.go
git commit -m "Add issue store: short keys, versioned docs, auto status transitions."
```

---

### Task 4: REST API

**Files:**
- Create: `internal/issue/http.go`
- Test: `internal/issue/http_test.go`
- Modify: `cmd/roundpend/main.go`

- [ ] **Step 1: 定义 Handler 与路由**

```go
// Package issue serves the user's issue tracker: issues, their versioned
// spec/plan documents and the tasks derived from those plans.
package issue

type Handler struct {
	Store *Store
}

func (h *Handler) Mount(mux *http.ServeMux) {
	mux.HandleFunc("GET /v1/issues", h.listIssues)
	mux.HandleFunc("POST /v1/issues", h.createIssue)
	mux.HandleFunc("GET /v1/issues/{key}", h.getIssue)
	mux.HandleFunc("PATCH /v1/issues/{key}", h.patchIssue)
	mux.HandleFunc("GET /v1/issues/{key}/docs", h.listDocs)
	mux.HandleFunc("POST /v1/issues/{key}/docs", h.createDoc)
	mux.HandleFunc("GET /v1/issues/{key}/docs/{docKey}", h.getDoc)
	mux.HandleFunc("GET /v1/issues/{key}/tasks", h.listTasks)
	mux.HandleFunc("POST /v1/issues/{key}/tasks", h.createTask)
	mux.HandleFunc("PATCH /v1/tasks/{key}", h.patchTask)
}
```

- [ ] **Step 2: 实现 handler（每个都从 ctx 取用户，属主校验只在 store 之外补 admin 旁路）**

```go
func (h *Handler) listIssues(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.GetUser(r.Context())
	if !ok {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if h.Store == nil {
		writeErr(w, http.StatusServiceUnavailable, "issue store unavailable")
		return
	}
	items, err := h.Store.List(r.Context(), user.Username, ListFilter{
		Status: r.URL.Query().Get("status"),
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"issues": items})
}
```

**来源字段（`assistantId` / `sessionId`）的归属规则。** 三者来源不同，服务端分别处理：

| 调用方 | `origin` | `sessionId` | `assistantId` |
|--------|----------|-------------|---------------|
| 控制台 `POST /v1/issues` | `console` | 忽略 body 里的值，存 `''` | `''` |
| 助手工具 | `chat` | 注册期绑定的当前会话（见 Task 5） | 由服务端从该会话行反查 |

- body 里允许出现 `sessionId`，服务端**先校验归属**：`SELECT assistant_id FROM agent_sessions WHERE id = $1 AND user_id = $2`；查不到按 400 返回（`sessionId` 不属于当前用户 / 不存在），查到则用它写入 `session_id` 并从 `agent_sessions.assistant_id` 取 `assistant_id`。
- `assistantId` **不接受客户端传入**——它总是从会话行推导，避免伪造来源。
- 控制台不传 `sessionId`，因此走 `origin='console'` 分支。

- [ ] **Step 3: 错误映射**

```go
func writeIssueErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeErr(w, http.StatusNotFound, "issue not found")
	case errors.Is(err, sql.ErrNoRows):
		writeErr(w, http.StatusNotFound, "not found")
	default:
		writeErr(w, http.StatusBadRequest, err.Error())
	}
}
```

错误体用 `{"error": "..."}`（与 `internal/assistant` 一致）；创建返回 201，其余 200。

- [ ] **Step 4: handler 测试**

```go
func TestHandler_RequiresAuth(t *testing.T) {
	h := &Handler{Store: &Store{}}
	mux := http.NewServeMux()
	h.Mount(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/issues", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rec.Code)
	}
}

func TestHandler_CreateRejectsBlankTitle(t *testing.T) {
	// auth.WithUser 注入测试用户；空白 title 必须 400，且不触达 store
}
```

- [ ] **Step 5: 装配 `main.go`**

在 `ticketStore` 附近：

```go
issueStore := &issue.Store{DB: db.SQL}
(&issue.Handler{Store: issueStore}).Mount(mux)
```

必须在 `mux.Handle("/", ui.Handler())`（SPA catch-all）**之前**。

- [ ] **Step 6: 跑测试 + curl smoke**

```bash
go test ./internal/issue/ -count=1
curl -s -X POST localhost:19001/v1/issues -H "X-API-Key: $KEY" -d '{"title":"验收议题"}' | jq
curl -s localhost:19001/v1/issues -H "X-API-Key: $KEY" | jq '.issues[0].key'
```

Expected: 返回 `ISS-1` 之类的 key；列表能查到。

- [ ] **Step 7: Commit**

```bash
git add internal/issue/http.go internal/issue/http_test.go cmd/roundpend/main.go
git commit -m "Expose /v1/issues, /v1/issues/{key}/docs and /v1/tasks/{key}."
```

---

### Task 5: Agent 工具

**Files:**
- Create: `internal/acp/sysagent/tools/issues.go`
- Modify: `internal/acp/sysagent/tools/registry_surface_test.go`
- Modify: `internal/acp/manager/manager.go`

- [ ] **Step 1: 实现 `RegisterIssues`**

```go
package tools

import (
	"context"
	"encoding/json"
	"net/http"
)

// RegisterIssues adds tracker tools. They call the control-plane API as the actor
// (like ListSessions), so validation and ownership live in one place — the HTTP layer.
// sessionID binds the registration-time chat session, the way BrowserBinder does, so
// created issues inherit their assistant and session server-side.
func RegisterIssues(r *Registry, api *RoundpenHTTP, sessionID string) {
	if r == nil || api == nil {
		return
	}
	r.Register(Tool{
		Name: "CreateIssue",
		Description: "Create a work item in the user's issue tracker and return its short key (ISS-12). " +
			"Use it when the user wants a new piece of work started — not for a one-off question or a single " +
			"trivial edit. Clarify scope, direction and decisions with AskUserQuestion first.",
		Parameters: objectSchema(map[string]any{
			"title":   map[string]any{"type": "string", "description": "Short imperative title"},
			"summary": map[string]any{"type": "string", "description": "One-line summary"},
		}, "title"),
		Mutating: true,
		Call: func(ctx context.Context, actor Actor, raw json.RawMessage) (string, error) {
			var a struct {
				Title   string `json:"title"`
				Summary string `json:"summary"`
			}
			if err := json.Unmarshal(raw, &a); err != nil {
				return "", err
			}
			return api.do(ctx, actor, http.MethodPost, "/v1/issues",
				map[string]any{"title": a.Title, "summary": a.Summary, "sessionId": sessionID})
		},
	})
	r.Register(Tool{
		Name:        "UpdateIssue",
		Description: "Update an issue by key: status is one of drafting, specced, planned, in_progress, done, cancelled.",
		Parameters: objectSchema(map[string]any{
			"key":     map[string]any{"type": "string"},
			"status":  map[string]any{"type": "string"},
			"title":   map[string]any{"type": "string"},
			"summary": map[string]any{"type": "string"},
		}, "key"),
		Mutating: true,
		Call:     issuePatchCall(api, "/v1/issues/"),
	})
	// ... ListIssues / GetIssue / WriteIssueDoc / ReadIssueDoc / CreateTask / UpdateTask
}
```

工具清单（全部经 `api.do`）：

| Name | Method + path | 说明 |
|------|---------------|------|
| `CreateIssue` | `POST /v1/issues` | 返回 `{"issue":{...}}`，取 `key` 用于后续引用 |
| `ListIssues` | `GET /v1/issues?status=` | 缺省列未完成（服务端返回全部，提示词要求默认只读未完成的） |
| `GetIssue` | `GET /v1/issues/{key}` | 返回议题 + 文档索引 + 任务清单 |
| `UpdateIssue` | `PATCH /v1/issues/{key}` | 显式更状态 |
| `WriteIssueDoc` | `POST /v1/issues/{key}/docs` | `kind=spec\|plan`，返回 `key` 与 `version` |
| `ReadIssueDoc` | `GET /v1/issues/{key}/docs/{docKey}` | `docKey` 可用 `DOC-n`、uuid 或 `latest` |
| `CreateTask` | `POST /v1/issues/{key}/tasks` | `planDocKey` 标注派生来源 |
| `UpdateTask` | `PATCH /v1/tasks/{key}` | 推进状态 |

`Mutating: true` 用于全部写工具；读工具不设。

写类工具（`WriteIssueDoc` / `CreateTask`）的 body 同样带上注册期绑定的 `sessionId`，服务端按 Task 4 的归属规则校验后写入 `issue_docs.session_id` / `tasks.session_id` —— 这样「哪个会话写了这版 Spec」「哪个会话在做这个任务」无需模型传入，也不会被伪造。

- [ ] **Step 2: 工具面测试**

```go
func TestRegistrySurface_IssueTools(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterIssues(reg, &tools.RoundpenHTTP{BaseURL: "http://127.0.0.1", HTTPClient: http.DefaultClient}, "sess-1")
	for _, name := range []string{
		"CreateIssue", "ListIssues", "GetIssue", "UpdateIssue",
		"WriteIssueDoc", "ReadIssueDoc", "CreateTask", "UpdateTask",
	} {
		tool, ok := reg.Get(name)
		if !ok {
			t.Fatalf("missing tool %s", name)
		}
		wantMutating := name != "ListIssues" && name != "GetIssue" && name != "ReadIssueDoc"
		if tool.Mutating != wantMutating {
			t.Fatalf("%s Mutating = %v, want %v", name, tool.Mutating, wantMutating)
		}
	}
}
```

- [ ] **Step 3: manager 注册**

在 `manager.go` 的 `tools.RegisterRoundpen(...)` 之后：

```go
tools.RegisterIssues(reg, &tools.RoundpenHTTP{BaseURL: m.sys.LoopbackBase}, sessionID)
```

`sessionID` 是该注册块的局部变量（`RegisterBrowser` 的 `BrowserBinder` 已在用同一个值）。

- [ ] **Step 4: 跑测试**

```bash
go test ./internal/acp/sysagent/tools/ -count=1
```

- [ ] **Step 5: Commit**

```bash
git add internal/acp/sysagent/tools/issues.go internal/acp/sysagent/tools/registry_surface_test.go internal/acp/manager/manager.go
git commit -m "Add issue tracker tools for the system agent."
```

---

### Task 6: 系统提示词规范 + auto mode 放行

**Files:**
- Modify: `internal/acp/sysagent/history.go`
- Modify: `internal/automode/rules.go`
- Test: `internal/automode/rules_test.go`

- [ ] **Step 1: 提示词加一段**

在 `buildPromptMessages` 的 `system` 常量里（`Skill:` 一行之后）追加：

```
Issues: when the user starts a new piece of work (more than a one-off question or a single trivial edit), create an issue with CreateIssue; if it is ambiguous whether they want one, ask. Clarify scope, direction and decisions with AskUserQuestion before writing anything, then WriteIssueDoc kind=spec, then kind=plan, then CreateTask for each plan step. Refer to work by key (ISS-12, TSK-34) and keep task status current as you implement: UpdateTask when you start and finish each one. Every tool call must be earned - do not create issues for questions you can answer directly.
```

- [ ] **Step 2: auto mode Allow 规则**

`internal/automode/rules.go` 的 `Defaults().Allow` 增加一条：

```go
"Recording issues, plan documents and tasks in the user's own issue tracker (CreateIssue, UpdateIssue, WriteIssueDoc, CreateTask, UpdateTask). This writes only the user's own tracker rows and grants no new capability.",
```

理由：这些工具写的是用户自己的跟踪器，不触碰沙箱外文件、不产生对外副作用，属于「记录」而非「执行」；不逐次打断对话。

- [ ] **Step 3: 断言规则的测试**

```go
func TestDefaults_AllowsIssueTrackerWrites(t *testing.T) {
	for _, rule := range Defaults().Allow {
		if strings.Contains(rule, "CreateIssue") && strings.Contains(rule, "WriteIssueDoc") {
			return
		}
	}
	t.Fatal("defaults must allow recording issues, docs and tasks")
}
```

- [ ] **Step 4: 跑测试**

```bash
go test ./internal/automode/ ./internal/acp/sysagent/ -count=1
```

- [ ] **Step 5: Commit**

```bash
git add internal/acp/sysagent/history.go internal/automode/rules.go internal/automode/rules_test.go
git commit -m "Teach the system agent the issue workflow and auto-allow tracker writes."
```

---

### Task 7: Web 客户端、导航与文案

**Files:**
- Modify: `web/src/api.ts`
- Modify: `web/src/lib/appNav.ts`
- Modify: `web/src/i18n/en.ts`
- Modify: `web/src/i18n/zh_CN.ts`

- [ ] **Step 1: `api.ts` 加类型与命名空间**

```ts
export type Issue = {
  id: string
  key: string
  userId: string
  assistantId?: string
  sessionId?: string
  title: string
  summary: string
  status: 'drafting' | 'specced' | 'planned' | 'in_progress' | 'done' | 'cancelled'
  origin: 'chat' | 'console'
  createdAt: string
  updatedAt: string
  closedAt?: string
}

export type IssueDoc = {
  id: string
  key: string
  issueId: string
  taskId?: string
  kind: 'spec' | 'plan'
  version: number
  status: 'draft' | 'current' | 'superseded'
  title: string
  contentMd?: string // 索引不含正文
  authorType: 'user' | 'assistant'
  createdAt: string
}

export type IssueTask = {
  id: string
  key: string
  issueId: string
  planDocId?: string
  position: number
  title: string
  detail: string
  status: 'todo' | 'in_progress' | 'done' | 'blocked' | 'cancelled'
  sessionId?: string
  createdAt: string
  updatedAt: string
  doneAt?: string
}

export const issues = {
  list: (status?: string) =>
    api<{ issues: Issue[] }>(
      `/v1/issues${status ? `?status=${encodeURIComponent(status)}` : ''}`,
    ),
  create: (body: { title: string; summary?: string }) =>
    api<{ issue: Issue }>('/v1/issues', { method: 'POST', body: JSON.stringify(body) }),
  get: (key: string) =>
    api<{ issue: Issue; docs: IssueDoc[]; tasks: IssueTask[] }>(`/v1/issues/${key}`),
  update: (key: string, body: { status?: string; title?: string; summary?: string }) =>
    api<{ issue: Issue }>(`/v1/issues/${key}`, { method: 'PATCH', body: JSON.stringify(body) }),
  listDocs: (key: string) => api<{ docs: IssueDoc[] }>(`/v1/issues/${key}/docs`),
  getDoc: (key: string, docKey: string) =>
    api<{ doc: IssueDoc }>(`/v1/issues/${key}/docs/${docKey}`),
  writeDoc: (
    key: string,
    body: { kind: 'spec' | 'plan'; contentMd: string; title?: string; status?: string },
  ) => api<{ doc: IssueDoc }>(`/v1/issues/${key}/docs`, { method: 'POST', body: JSON.stringify(body) }),
  listTasks: (key: string) => api<{ tasks: IssueTask[] }>(`/v1/issues/${key}/tasks`),
  createTask: (key: string, body: { title: string; detail?: string; position?: number }) =>
    api<{ task: IssueTask }>(`/v1/issues/${key}/tasks`, { method: 'POST', body: JSON.stringify(body) }),
  updateTask: (key: string, body: { status?: string; title?: string; detail?: string }) =>
    api<{ task: IssueTask }>(`/v1/tasks/${key}`, { method: 'PATCH', body: JSON.stringify(body) }),
}
```

- [ ] **Step 2: 导航**

`web/src/lib/appNav.ts`：

```ts
export type PrimaryMenuId = 'assistants' | 'issues' | 'workspace' | 'settings' | 'registry'

export const PRIMARY_MENUS: PrimaryMenu[] = [
  { id: 'assistants', to: '/a', labelKey: 'nav.assistants' },
  { id: 'issues', to: '/issues', labelKey: 'nav.issues' },
  { id: 'workspace', to: '/workspace', labelKey: 'nav.workspace' },
  { id: 'settings', to: '/settings', labelKey: 'nav.settings' },
  { id: 'registry', to: '/registry', labelKey: 'nav.registry', admin: true },
]
```

`matchPrimaryMenu` 增加一行（**容易漏**，漏了导航高亮会错落到「助手」）：

```ts
if (pathname.startsWith('/issues')) return 'issues'
```

- [ ] **Step 3: 文案键**

`en.ts` 加（`MessageKey` 由 `en` 推导，`zh_CN.ts` 必须补齐同名键）：

```ts
'nav.issues': 'Issues',
'issues.title': 'Issues',
'issues.new': 'New issue',
'issues.empty': 'No issues yet. You can also ask your assistant in chat to create one.',
'issues.column.key': 'Key',
'issues.column.title': 'Title',
'issues.column.status': 'Status',
'issues.column.updated': 'Updated',
'issues.status.drafting': 'Clarifying',
'issues.status.specced': 'Spec ready',
'issues.status.planned': 'Planned',
'issues.status.in_progress': 'In progress',
'issues.status.done': 'Done',
'issues.status.cancelled': 'Cancelled',
'issues.tab.spec': 'Spec',
'issues.tab.plan': 'Plan',
'issues.tasks': 'Tasks',
'issues.task.new': 'Add task',
'issues.doc.newVersion': 'New version',
'issues.doc.version': 'Version {n}',
'issues.doc.current': 'Current',
'issues.doc.superseded': 'Superseded',
'issues.doc.draft': 'Draft',
'issues.doc.empty': 'No document yet.',
```

`zh_CN.ts` 对应：`议题`、`新建议题`、`还没有议题。也可以在对话里让助手创建。`、`澄清中` / `Spec 已定稿` / `已计划` / `进行中` / `已完成` / `已取消`、`任务`、`新增任务`、`新版本`、`当前版本`、`已被取代`、`草稿`、`还没有文档。`

- [ ] **Step 4: 提交**

```bash
git add web/src/api.ts web/src/lib/appNav.ts web/src/i18n/en.ts web/src/i18n/zh_CN.ts
git commit -m "Add issue tracker API client, nav entry and copy."
```

---

### Task 8: 议题列表页

**Files:**
- Create: `web/src/pages/IssuesPage.tsx`
- Modify: `web/src/App.tsx`

- [ ] **Step 1: 实现列表页**

- `issues.list(status)` 拉取；状态用 Semi `Select` 筛选（全部 / 未完成 / 各状态）。
- 表格列：`key`（等宽字体）、标题、状态 `Tag`（色：drafting 灰、specced/planned 蓝、in_progress 橙、done 绿、cancelled 灰）、来源助手、更新时间。
- 行点击 → `/issues/{key}`。
- 右上「新建议题」→ `SideSheet` 或 `Modal` 表单（标题必填 + 摘要）→ `issues.create` → 跳详情。
- 空态文案用 `issues.empty`。

- [ ] **Step 2: 路由**

```tsx
<Route path="/issues" element={<IssuesPage />} />
<Route path="/issues/:key" element={<IssueDetailPage />} />
```

加在 `AppShell` 的子路由里（`/workspace` 那一行附近）。

- [ ] **Step 3: 手动验证**

Run: `make dev`，打开 `/issues`，新建一个议题，确认跳详情且导航高亮在「议题」。

- [ ] **Step 4: Commit**

```bash
git add web/src/pages/IssuesPage.tsx web/src/App.tsx
git commit -m "Add issues list page with status filter and create dialog."
```

---

### Task 9: 议题详情页（文档 + 任务）

**Files:**
- Create: `web/src/pages/IssueDetailPage.tsx`

- [ ] **Step 1: 头部**

`ISS-12` + 标题 + 状态 `Tag` + 来源（有 `assistantId` 时链到 `/a/{assistantId}`，有 `sessionId` 时链到 `/a/{assistantId}/s/{sessionId}`）+ 更新时间。状态可手动改（`Select` → `issues.update`），用于用户裁决（取消 / 提前完成）。

- [ ] **Step 2: 左栏——文档**

- `issues.get(key)` 返回的 `docs` 是索引；按 `kind` 分两个 `Tab`（Spec / Plan）。
- 每个 Tab 内：版本 `Select`（`v{n}` + 状态标签），选中后懒加载 `issues.getDoc(key, docKey)` 取正文。
- 正文渲染用 Semi 的 `MarkdownRender`（`@douyinfe/semi-ui-19` 已内置，零新依赖）：

```tsx
import { MarkdownRender } from '@douyinfe/semi-ui-19'

<MarkdownRender raw={doc.contentMd ?? ''} format="md" />
```

- 「新版本」按钮 → 带 `TextArea` 的 `Modal`，提交 `issues.writeDoc(key, { kind, contentMd, status: 'current' })`，刷新后版本 +1、旧版转 `Superseded`。
- 空态用 `issues.doc.empty`。

- [ ] **Step 3: 右栏——任务**

- `tasks` 按 `position` 排列；每行 `Checkbox`（勾选 → `updateTask(status: 'done')`，取消勾选 → `'todo'`）+ `TSK-n` + 标题 + 状态 `Tag`。
- 行展开显示 `detail`；`blocked` 时高亮提示需要说明。
- 「新增任务」→ 行内输入（标题 + 可选说明）→ `createTask`。
- 勾选/取消勾选后重新 `issues.get(key)`：议题状态可能已被服务端自动推进（如全部完成 → `done`）。

- [ ] **Step 4: 手动验证**

Run: `make dev`；建议题 → 写 Spec v1 → 写 v2（确认 v1 标 `Superseded`）→ 写 Plan → 建两个任务 → 勾完第一个（状态变「进行中」）→ 勾完第二个（状态变「已完成」）。

- [ ] **Step 5: Commit**

```bash
git add web/src/pages/IssueDetailPage.tsx
git commit -m "Add issue detail page with versioned docs and task checklist."
```

---

### Task 10: e2e 与控制台文档

**Files:**
- Create: `web/e2e/issues.spec.ts`
- Modify: `README.md`（核心能力一条：议题与任务跟踪）

- [ ] **Step 1: Playwright smoke**

```ts
import { test, expect } from '@playwright/test'
import { loginAsAdmin } from './helpers'

test('create issue, write spec and finish a task', async ({ page }) => {
  await loginAsAdmin(page)
  await page.goto('/issues')
  await page.getByRole('button', { name: /新建议题|New issue/ }).click()
  await page.getByLabel(/标题|Title/).fill('e2e 议题')
  await page.getByRole('button', { name: /创建|Create/ }).click()
  await expect(page).toHaveURL(/\/issues\/ISS-/)
  await expect(page.getByText(/还没有文档|No document yet/)).toBeVisible()
})
```

选择器以 Task 8/9 实际落地的 label 为准（沿用 `web/e2e/helpers.ts` 的登录与角色定位模式）。

- [ ] **Step 2: 跑 e2e**

Run: `cd web && npx playwright test e2e/issues.spec.ts`
Expected: PASS（栈未启动时按既有约定 skip，并在提交说明里写清原因）

- [ ] **Step 3: README**

「核心能力」加一条：

```
7. **议题与任务跟踪**：对话中产生的议题落库（`ISS-n`），澄清后写版本化 Spec / Plan（`DOC-n`），拆成任务清单（`TSK-n`）逐个实现；控制台 `/issues` 可读可改
```

- [ ] **Step 4: Commit**

```bash
git add web/e2e/issues.spec.ts README.md
git commit -m "Cover the issue tracker console path with e2e and a README note."
```

---

## Plan self-review

| Spec 章节 | 落地任务 |
|-----------|----------|
| §3.1 何时建议题 | Task 6（提示词写明建/不建与「模糊时先问」） |
| §3.2 澄清三要素 | Task 6（提示词要求先 `AskUserQuestion`）+ Task 9（Spec 承载三要素） |
| §3.3 / §3.4 Spec 与 Plan | Tasks 3、4、9（`WriteIssueDoc` / 版本化 / UI） |
| §3.5 拆任务与逐个实现 | Tasks 3、5、9（`CreateTask` / `plan_doc_id` / 勾选） |
| §3.6 修订 | Task 3（新版本 + `superseded`，不覆盖） |
| §4 状态机 | Task 2（纯函数 + 单测）、Task 3（事务内自动流转） |
| §5 数据模型与短 key | Task 1（表 + 序列）、Task 3（`nextval` 生成） |
| §6 API | Task 4 |
| §7 工具面与 auto mode | Task 5、Task 6 |
| §8 控制台 | Tasks 7、8、9 |
| §10 权限与隔离 | Task 3（SQL 层 user 过滤 + 跨用户测试）、Task 4（admin 旁路） |
| §12 成功标准 | Task 10 e2e 覆盖「建 → 写 → 拆 → 完成」闭环 |

无 TBD 占位；类型名（`Issue` / `Doc` / `Task`、`spec|plan`、`draft|current|superseded`）在 store、HTTP、工具、前端四处一致。

## Execution handoff

Plan complete and saved to `docs/superpowers/plans/2026-09-20-issue-task-tracking.md`.

**Two execution options:**

1. **Subagent-Driven (recommended)** — fresh subagent per task, review between tasks
2. **Inline Execution** — execute tasks in this session with checkpoints
