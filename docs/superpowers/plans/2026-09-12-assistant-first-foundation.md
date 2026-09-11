# Assistant-First Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce a first-class **Assistant** (name, bio, identity mode, capabilities, network tier stubs, directory grants stub) and rewire the Web console so users manage assistants—not sandboxes or session lists—while keeping the existing Claude Code / ACP chat runtime.

**Architecture:** New `assistants` table + `internal/assistant` store/API sit above `agent_sessions`. Each assistant owns one primary session (`assistant_id` on sessions). UI sidebar lists assistants; create wizard is three steps; detail page edits the constitution. Policy enforcement, quiet-chat UX, and assist tickets are **out of scope** here (follow-on plans).

**Tech Stack:** Go control plane, PostgreSQL migrations, React + Vite (`web/`), existing ACP/`agentapi` session WS.

**Spec:** `docs/superpowers/specs/2026-09-12-assistant-first-ui-design.md`

**Follow-on plans (do not implement in this file):**

| Plan | Scope |
|------|--------|
| 2 | Quiet chat + 详情「此刻」activity timeline |
| 3 | 协助单 + 顶栏待处理（evolve `permission_request`） |
| 4 | Soft-deny policy, directory mounts, network egress enforcement |

---

## File map (Plan 1)

| Path | Responsibility |
|------|----------------|
| `migrations/0011_assistants.sql` | `assistants` table; `agent_sessions.assistant_id` |
| `internal/assistant/assistant.go` | Domain types, validation, capability presets |
| `internal/assistant/store.go` | CRUD + EnsurePrimarySession helpers used by API |
| `internal/assistant/store_test.go` | Store tests (Postgres) |
| `internal/assistant/http.go` | `/v1/assistants` REST |
| `internal/assistant/http_test.go` | HTTP handler tests |
| `internal/agentsession/store.go` | Add `AssistantID`; Create/List filter updates |
| `internal/api/agentapi/handler.go` | Accept/return `assistantId`; prefer assistant-scoped create |
| `cmd/roundpend/main.go` | Mount assistant handler |
| `web/src/api.ts` | `Assistant` types + client |
| `web/src/App.tsx` | Routes `/a/:assistantId`, detail, create |
| `web/src/components/AssistantLayout.tsx` | Sidebar of assistants (replace session list) |
| `web/src/pages/AssistantCreatePage.tsx` | Three-step create wizard |
| `web/src/pages/AssistantDetailPage.tsx` | Editable constitution |
| `web/src/pages/ChatSessionPage.tsx` | Minor: load via assistant primary session |
| `web/src/components/PageShell.tsx` | Primary nav: Assistants focus; demote Browser/Images |
| `web/e2e/assistants.spec.ts` | Smoke: create assistant → land in chat |

---

### Task 1: Migration — `assistants` + session FK

**Files:**
- Create: `migrations/0011_assistants.sql`
- Modify: none yet

- [ ] **Step 1: Add migration file**

```sql
-- Assistants: user-facing agent profiles (constitution). Sandboxes remain implementation detail.
CREATE TABLE IF NOT EXISTS assistants (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL REFERENCES users (username) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    bio             TEXT NOT NULL DEFAULT '',
    identity_mode   TEXT NOT NULL DEFAULT 'proxy_user'
        CHECK (identity_mode IN ('proxy_user', 'independent')),
    capabilities    JSONB NOT NULL DEFAULT '{"shell":true,"browser":false,"mobile":false,"desktop":false}',
    network_tier    TEXT NOT NULL DEFAULT 'dev_sites'
        CHECK (network_tier IN ('none', 'dev_sites', 'all')),
    network_allowlist JSONB NOT NULL DEFAULT '[]',
    directory_grants  JSONB NOT NULL DEFAULT '[]',
    status          TEXT NOT NULL DEFAULT 'active'
        CHECK (status IN ('active', 'disabled')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS assistants_user_idx ON assistants (user_id, updated_at DESC);

ALTER TABLE agent_sessions
    ADD COLUMN IF NOT EXISTS assistant_id TEXT REFERENCES assistants (id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS agent_sessions_assistant_idx
    ON agent_sessions (assistant_id, updated_at DESC);
```

- [ ] **Step 2: Commit**

```bash
git add migrations/0011_assistants.sql
git commit -m "Add assistants table and agent_sessions.assistant_id."
```

---

### Task 2: Domain types + presets

**Files:**
- Create: `internal/assistant/assistant.go`
- Create: `internal/assistant/assistant_test.go`

- [ ] **Step 1: Write failing tests for presets and validation**

```go
package assistant

import "testing"

func TestApplyPreset_CodeBrowser(t *testing.T) {
	c := ApplyPreset("code_browser")
	if !c.Shell || !c.Browser {
		t.Fatalf("want shell+browser: %+v", c)
	}
	if c.Mobile || c.Desktop {
		t.Fatalf("mobile/desktop should be off: %+v", c)
	}
}

func TestValidateCreate_RequiresName(t *testing.T) {
	err := ValidateCreate(CreateInput{Name: "  ", IdentityMode: IdentityProxyUser})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateCreate_Identity(t *testing.T) {
	err := ValidateCreate(CreateInput{Name: "Ada", IdentityMode: "nope"})
	if err == nil {
		t.Fatal("expected bad identity")
	}
}
```

- [ ] **Step 2: Run tests — expect FAIL (package missing)**

Run: `go test ./internal/assistant/ -count=1`
Expected: FAIL cannot find package or undefined symbols

- [ ] **Step 3: Implement domain**

```go
package assistant

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	IdentityProxyUser   = "proxy_user"
	IdentityIndependent = "independent"

	NetworkNone     = "none"
	NetworkDevSites = "dev_sites"
	NetworkAll      = "all"

	StatusActive   = "active"
	StatusDisabled = "disabled"
)

type Capabilities struct {
	Shell   bool `json:"shell"`
	Browser bool `json:"browser"`
	Mobile  bool `json:"mobile"`
	Desktop bool `json:"desktop"`
}

type DirectoryGrant struct {
	Path      string `json:"path"`
	Mode      string `json:"mode"` // read | readwrite
	CreatedAt string `json:"createdAt,omitempty"`
}

type Assistant struct {
	ID               string           `json:"id"`
	UserID           string           `json:"userId"`
	Name             string           `json:"name"`
	Bio              string           `json:"bio"`
	IdentityMode     string           `json:"identityMode"`
	Capabilities     Capabilities     `json:"capabilities"`
	NetworkTier      string           `json:"networkTier"`
	NetworkAllowlist []string         `json:"networkAllowlist"`
	DirectoryGrants  []DirectoryGrant `json:"directoryGrants"`
	Status           string           `json:"status"`
	PrimarySessionID string           `json:"primarySessionId,omitempty"`
	CreatedAt        time.Time        `json:"createdAt"`
	UpdatedAt        time.Time        `json:"updatedAt"`
}

type CreateInput struct {
	Name         string
	Bio          string
	IdentityMode string
	Preset       string // writing | code | code_browser | custom
	Capabilities *Capabilities
}

func ApplyPreset(preset string) Capabilities {
	switch strings.TrimSpace(preset) {
	case "writing":
		return Capabilities{}
	case "code":
		return Capabilities{Shell: true}
	case "code_browser":
		return Capabilities{Shell: true, Browser: true}
	default:
		return Capabilities{Shell: true}
	}
}

func ValidateCreate(in CreateInput) error {
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("name is required")
	}
	switch in.IdentityMode {
	case IdentityProxyUser, IdentityIndependent:
	default:
		return fmt.Errorf("identityMode must be proxy_user or independent")
	}
	return nil
}

func capsJSON(c Capabilities) (json.RawMessage, error) {
	return json.Marshal(c)
}
```

- [ ] **Step 4: Run tests — expect PASS**

Run: `go test ./internal/assistant/ -count=1`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/assistant/assistant.go internal/assistant/assistant_test.go
git commit -m "Add assistant domain types and capability presets."
```

---

### Task 3: Assistant store CRUD

**Files:**
- Create: `internal/assistant/store.go`
- Create: `internal/assistant/store_test.go`
- Modify: `internal/agentsession/store.go` (add `AssistantID` field + Create with assistantID)

- [ ] **Step 1: Extend `agentsession.Session` and `Create`**

In `internal/agentsession/store.go`, add field:

```go
AssistantID string `json:"assistantId,omitempty"`
```

Change `Create` signature to:

```go
func (s *Store) Create(ctx context.Context, userID, title, providerID, sandboxID, assistantID string) (*Session, error)
```

Update INSERT to include `assistant_id`. Update all call sites (`agentapi`, tests) to pass `""` or the real id.

Add:

```go
func (s *Store) ListByAssistant(ctx context.Context, userID, assistantID string) ([]Session, error)
```

filtering `assistant_id=$1 AND user_id=$2 AND status <> 'stopped'`.

- [ ] **Step 2: Write store test `TestStore_CreateListUpdate`**

Mirror `agentsession/store_test.go` DB setup (`ROUNDPEN_TEST_DATABASE_URL` / migrate). Assert Create → Get → ListByUser → Update bio/capabilities → soft disable.

- [ ] **Step 3: Run test — expect FAIL**

Run: `go test ./internal/assistant/ -run TestStore_CreateListUpdate -count=1`
Expected: FAIL undefined Store

- [ ] **Step 4: Implement `store.go`**

```go
package assistant

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type Store struct {
	DB *sql.DB
}

func (s *Store) Create(ctx context.Context, userID string, in CreateInput) (*Assistant, error) {
	if err := ValidateCreate(in); err != nil {
		return nil, err
	}
	caps := ApplyPreset(in.Preset)
	if in.Capabilities != nil {
		caps = *in.Capabilities
	}
	// Mobile/Desktop not ready — force off
	caps.Mobile = false
	caps.Desktop = false

	now := time.Now().UTC()
	a := &Assistant{
		ID:               uuid.NewString(),
		UserID:           userID,
		Name:             strings.TrimSpace(in.Name),
		Bio:              strings.TrimSpace(in.Bio),
		IdentityMode:     in.IdentityMode,
		Capabilities:     caps,
		NetworkTier:      NetworkDevSites,
		NetworkAllowlist: []string{},
		DirectoryGrants:  []DirectoryGrant{},
		Status:           StatusActive,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	capRaw, err := capsJSON(caps)
	if err != nil {
		return nil, err
	}
	allowRaw, _ := json.Marshal(a.NetworkAllowlist)
	dirRaw, _ := json.Marshal(a.DirectoryGrants)
	_, err = s.DB.ExecContext(ctx, `
		INSERT INTO assistants (
			id, user_id, name, bio, identity_mode, capabilities,
			network_tier, network_allowlist, directory_grants, status, created_at, updated_at
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
		a.ID, a.UserID, a.Name, a.Bio, a.IdentityMode, capRaw,
		a.NetworkTier, allowRaw, dirRaw, a.Status, a.CreatedAt, a.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// Get, ListByUser, Update (partial), scan helpers — implement fully in this task.
// Update must reject unknown identity_mode; treat identity mode change as allowed but API will require confirm flag later.
```

Implement `Get`, `ListByUser`, `Update` completely (no stubs). `Update` accepts a struct with pointers for optional fields: Name, Bio, IdentityMode, Capabilities, NetworkTier, NetworkAllowlist, DirectoryGrants, Status.

- [ ] **Step 5: Fix agentsession call sites; run tests**

Run:

```bash
go test ./internal/agentsession/ ./internal/assistant/ ./internal/api/agentapi/ ./internal/agentenv/ -count=1
```

Expected: PASS (skip DB tests if no DSN is OK for unit-only; store tests need DSN)

- [ ] **Step 6: Commit**

```bash
git add internal/assistant/store.go internal/assistant/store_test.go internal/agentsession/store.go internal/api/agentapi/ internal/agentenv/
git commit -m "Persist assistants and link agent_sessions.assistant_id."
```

---

### Task 4: HTTP API `/v1/assistants`

**Files:**
- Create: `internal/assistant/http.go`
- Create: `internal/assistant/http_test.go`
- Modify: `cmd/roundpend/main.go`
- Modify: `internal/api/agentapi/handler.go` (create session requires assistantId OR create-via-assistant endpoint)

- [ ] **Step 1: Define routes in `http.go`**

| Method | Path | Behavior |
|--------|------|----------|
| GET | `/v1/assistants` | List current user's assistants |
| POST | `/v1/assistants` | Create from `{ name, bio, identityMode, preset?, capabilities? }` |
| GET | `/v1/assistants/{id}` | Get; include `primarySessionId` if any active session |
| PATCH | `/v1/assistants/{id}` | Update constitution fields |
| POST | `/v1/assistants/{id}/ensure-session` | Return existing primary session or create one (`providerId=claude` default, title=assistant name) |

Auth: reuse same cookie/API-key middleware pattern as `gitcred.Handler` / `agentapi` (read `authz.Actor` from context). Copy the Mount style from `internal/gitcred/http.go`.

Ensure-session logic:

1. `ListByAssistant`; if any `active`, return newest.
2. Else `agentsession.Create(..., assistant.ID)` + existing `agentenv.Provisioner` path used by `agentapi.createSession` — **prefer calling shared helper** extracted from `agentapi` rather than duplicating provision. Minimal approach: have `ensure-session` HTTP handler live in `agentapi` that depends on assistant store, OR assistant handler receives `SessionStarter` interface:

```go
type SessionStarter interface {
	StartForAssistant(ctx context.Context, userID, assistantID, title string) (sessionID string, err error)
}
```

Implement `SessionStarter` in `agentapi` wrapping current create+provision+acp start.

- [ ] **Step 2: Handler test for create+list (table-driven or httptestUse `httptest` + auth test helper from `internal/api/auth/session_handler_test.go` if available; otherwise store-level integration is enough and add a thin `TestValidatePatch` for JSON decode.

At minimum:

```go
func TestCreateRequestDecode(t *testing.T) {
	// decode JSON body into CreateInput; reject empty name
}
```

- [ ] **Step 3: Wire `main.go`**

```go
assistantStore := &assistant.Store{DB: db.SQL}
(&assistant.Handler{
	Store:   assistantStore,
	Sessions: agentStore,
	Starter: agentSessionStarter, // from agentapi refactor
}).Mount(mux)
```

- [ ] **Step 4: Backfill on list (optional one-shot)**

When `GET /v1/assistants` returns empty but user has orphan `agent_sessions`, create a default assistant "默认助手" and attach sessions (`UPDATE agent_sessions SET assistant_id=... WHERE user_id=... AND assistant_id IS NULL`). Keep in `Handler.List` behind clear comment; cover with store test `TestBackfillOrphanSessions`.

- [ ] **Step 5: `go test` + manual curl smoke**

```bash
go test ./internal/assistant/ ./internal/api/agentapi/ -count=1
```

- [ ] **Step 6: Commit**

```bash
git add internal/assistant/http.go internal/assistant/http_test.go cmd/roundpend/main.go internal/api/agentapi/
git commit -m "Expose /v1/assistants CRUD and ensure-session."
```

---

### Task 5: Web API client + types

**Files:**
- Modify: `web/src/api.ts`

- [ ] **Step 1: Add types and client**

```ts
export type AssistantCapabilities = {
  shell: boolean
  browser: boolean
  mobile: boolean
  desktop: boolean
}

export type Assistant = {
  id: string
  userId: string
  name: string
  bio: string
  identityMode: 'proxy_user' | 'independent'
  capabilities: AssistantCapabilities
  networkTier: 'none' | 'dev_sites' | 'all'
  networkAllowlist: string[]
  directoryGrants: { path: string; mode: 'read' | 'readwrite'; createdAt?: string }[]
  status: 'active' | 'disabled'
  primarySessionId?: string
  createdAt: string
  updatedAt: string
}

export const assistants = {
  list: () => api<{ assistants: Assistant[] }>('/v1/assistants'),
  create: (body: {
    name: string
    bio?: string
    identityMode: 'proxy_user' | 'independent'
    preset?: string
    capabilities?: AssistantCapabilities
  }) =>
    api<Assistant>('/v1/assistants', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  get: (id: string) => api<Assistant>(`/v1/assistants/${id}`),
  update: (id: string, body: Partial<Assistant> & { confirmIdentityChange?: boolean }) =>
    api<Assistant>(`/v1/assistants/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),
  ensureSession: (id: string) =>
    api<{ sessionId: string }>(`/v1/assistants/${id}/ensure-session`, {
      method: 'POST',
      body: '{}',
    }),
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/api.ts
git commit -m "Add web client for assistants API."
```

---

### Task 6: Assistant layout + routing

**Files:**
- Create: `web/src/components/AssistantLayout.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/components/PageShell.tsx`
- Modify: `web/src/pages/ChatSessionPage.tsx` (keep working under new routes)
- Modify or retire: `web/src/components/ChatLayout.tsx`, `web/src/pages/ChatsPage.tsx`

- [ ] **Step 1: Update primary NAV**

In `PageShell.tsx`:

```ts
export type AppSection = 'assistants' | 'settings' | 'templates' | 'browser' | 'sandboxes'

export const NAV: { id: AppSection; to: string; label: string; admin?: boolean; advanced?: boolean }[] = [
  { id: 'assistants', to: '/a', label: '助手' },
  { id: 'settings', to: '/settings', label: '设置' },
  // Keep browser/registry reachable but mark advanced — render only if user opens 设置→高级 or admin:
  { id: 'browser', to: '/browser', label: '浏览器', advanced: true },
  { id: 'templates', to: '/registry', label: '镜像', admin: true },
]
```

Primary header shows only non-advanced items for normal users.

- [ ] **Step 2: `AssistantLayout` sidebar**

- Load `assistants.list()`.
- Each row: name + bio one-line (or「补充简介以便派活」if bio empty).
- Click row → `assistants.ensureSession(id)` → `navigate(/a/${id}/chat)` which renders existing `ChatSessionPage` with session id from ensure.
- Header link on assistant name → `/a/${id}` detail.
- 「新建助手」→ `/a/new`.
- On `/a` index: if assistants exist, redirect to first/most recently updated; else redirect `/a/new`.

Route shape:

```tsx
<Route path="/a" element={<RequireAuth><AssistantLayout /></RequireAuth>}>
  <Route index element={<AssistantsIndexRedirect />} />
  <Route path="new" element={<AssistantCreatePage />} />
  <Route path=":assistantId" element={<AssistantDetailPage />} />
  <Route path=":assistantId/chat" element={<AssistantChatPage />} />
</Route>
<Route path="/chats/*" element={<Navigate to="/a" replace />} />
```

`AssistantChatPage`: read `assistantId`, call `ensureSession` once, then reuse `ChatSessionPage` logic (extract session id prop or nested route).

- [ ] **Step 3: Manual UI check**

Run: `make dev` (or existing web+API). Open `/a` — sidebar lists assistants after create.

- [ ] **Step 4: Commit**

```bash
git add web/src/App.tsx web/src/components/AssistantLayout.tsx web/src/components/PageShell.tsx web/src/pages/
git commit -m "Route console around assistants instead of raw chat sessions."
```

---

### Task 7: Create wizard (三问)

**Files:**
- Create: `web/src/pages/AssistantCreatePage.tsx`

- [ ] **Step 1: Implement three steps in one page component**

1. Name (required) + Bio (textarea with help:「简介用于派活时匹配最合适的助手」).
2. Identity: two large cards `proxy_user` / `independent` with consequence one-liners in Chinese.
3. Preset radios: `writing` | `code` | `code_browser` + optional expand for capability checkboxes (mobile/desktop disabled +「即将推出」).

Submit → `assistants.create` → `ensureSession` → `navigate(/a/${id}/chat)`.

Do **not** show provider/runtime picker.

- [ ] **Step 2: Commit**

```bash
git add web/src/pages/AssistantCreatePage.tsx
git commit -m "Add three-step assistant create wizard."
```

---

### Task 8: Assistant detail page

**Files:**
- Create: `web/src/pages/AssistantDetailPage.tsx`

- [ ] **Step 1: Sections matching spec §4**

| Section | Behavior |
|---------|----------|
| 基本信息 | Edit name/bio; show status |
| 此刻 | Placeholder panel:「活动时间线将在后续版本提供」+ link to chat if working |
| 可见范围 | List `directoryGrants`; add/remove UI that PATCHes JSON only (no real mount yet) |
| 能力清单 | Toggles shell/browser; mobile/desktop disabled |
| 网络 | Tier radios; allowlist textarea (comma/newline domains) |
| 身份绑定 | Show mode; if changing mode, `window.confirm` then PATCH with `confirmIdentityChange: true`. Show link to Settings git credentials for 代理我; for 独立 show「助手专用账号将在后续版本连接」 |

Copy style from existing `SettingsPage` / dialogs — keep minimal CSS consistent with current app.

- [ ] **Step 2: Commit**

```bash
git add web/src/pages/AssistantDetailPage.tsx
git commit -m "Add editable assistant detail (constitution) page."
```

---

### Task 9: E2E smoke + docs pointer

**Files:**
- Create: `web/e2e/assistants.spec.ts`
- Modify: `README.md` (one paragraph under Web 控制台: Assistants)
- Modify: `docs/architecture/acp-agent-ui.md` (short note: sessions belong to assistants)

- [ ] **Step 1: Playwright smoke**

```ts
import { test, expect } from '@playwright/test'
import { loginAsAdmin } from './helpers' // use existing helper names from web/e2e/helpers.ts

test('create assistant and open chat', async ({ page }) => {
  await loginAsAdmin(page)
  await page.goto('/a/new')
  await page.getByLabel(/名称|Name/i).fill('测试助手')
  await page.getByRole('button', { name: /下一步|继续/i }).click()
  await page.getByText(/代理我/).click()
  await page.getByRole('button', { name: /下一步|继续/i }).click()
  await page.getByText(/代码/).first().click()
  await page.getByRole('button', { name: /创建/i }).click()
  await expect(page).toHaveURL(/\/a\/.+\/chat/)
})
```

Adjust selectors to match actual labels you ship in Task 7.

- [ ] **Step 2: Run e2e if CI DB available**

Run: `cd web && npx playwright test e2e/assistants.spec.ts`
Expected: PASS (or skip with clear reason if stack not up)

- [ ] **Step 3: Commit**

```bash
git add web/e2e/assistants.spec.ts README.md docs/architecture/acp-agent-ui.md
git commit -m "Cover assistant create path with e2e and doc notes."
```

---

## Plan self-review

| Spec item | Task |
|-----------|------|
| 助手优先导航 | Task 6 |
| 创建三问 + 简介重要性 | Task 7 |
| 身份二选一 | Tasks 2, 7, 8 |
| 可见范围/能力/网络可编辑 | Task 8 (persist); enforcement → Plan 4 |
| 不选 runtime | Task 7 |
| 详情可看在干嘛 | Placeholder in Task 8 → Plan 2 |
| 安静对话 / 协助单 | Plans 2–3 |
| 软拒绝可申请 | Plan 4 |
| 平台自带 Claude runtime | ensure-session uses `claude` provider (Task 4) |

No TBD placeholders left in steps. Types use `proxy_user` / `independent` and `dev_sites` consistently.

---

## Execution handoff

Plan complete and saved to `docs/superpowers/plans/2026-09-12-assistant-first-foundation.md`.

**Two execution options:**

1. **Subagent-Driven (recommended)** — fresh subagent per task, review between tasks  
2. **Inline Execution** — execute tasks in this session with checkpoints  

Which approach?
