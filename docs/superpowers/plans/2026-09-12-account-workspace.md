# Account Workspace (via Agent container) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ship a primary-nav **工作区** page and `/v1/me/workspace/files*` so users can browse/upload/download/delete account workspace files through their Ensure’d Docker Agent container (`roundpen-{user}`), without exposing sandbox ids.

**Architecture:** New `internal/api/workspaceapi` calls `EnsureAgent` then container-scoped file ops (Docker `CopyTo`/`CopyFrom` + `Exec` for list/rm). Agent sandboxes get a **deterministic id** = sanitized username so existing `containerName` → `roundpen-{user}`. EnsureAgent always uses Docker + `code-agent` (stop reading per-user engine prefs for Agent). UI talks to `me/workspace` APIs (patterns from `FileTree`, no sandbox id).

**Tech Stack:** Go (`roundpend`), Docker Engine API, React + Semi (`web/`), existing `userenv` / `sandbox`.

**Spec:** `docs/superpowers/specs/2026-09-12-agent-docker-workspace-design.md` (§4–§5.1, §5.3 partial)

**Follow-on:** `docs/superpowers/plans/2026-09-12-agent-docker-backend-cleanup.md` — Kern removal, Agent-QEMU demotion, runtime settings UI removal, GHCR publish + default `artifact_ref`.

---

## File map

| Path | Responsibility |
|------|----------------|
| `internal/workspace/userid.go` | `AgentSandboxID` + shared sanitize |
| `internal/sandbox/sandbox.go` + `manager.go` | Optional `CreateRequest.ID` |
| `internal/userenv/userenv.go` | Deterministic Agent id; EnsureAgent → Docker/`code-agent` |
| `internal/backend/docker/docker.go` | `CopyToContainer` / `CopyFromContainer` helpers |
| `internal/sandbox/workspace_guest.go` | List/Read/Write/Remove via guest |
| `internal/api/workspaceapi/` | `/v1/me/workspace/files*` |
| `cmd/roundpend/main.go` | Mount workspaceapi |
| `web/src/lib/appNav.ts` | Primary menu `workspace` |
| `web/src/i18n/{zh_CN,en}.ts` | Strings |
| `web/src/api.ts` | `meWorkspace` client |
| `web/src/pages/WorkspacePage.tsx` | MVP file UI |
| `web/src/App.tsx` / `AppShell.tsx` | Route + icon |
| `web/src/pages/AssistantDetailPage.tsx` | Link to `/workspace` |
| `images/code-agent/Dockerfile` | Add `python3-minimal` if list uses Python |

---

### Task 1: Deterministic Agent sandbox id → `roundpen-{user}`

**Files:**
- Modify: `internal/workspace/userid.go`, `userid_test.go`
- Modify: `internal/sandbox/sandbox.go`, `manager.go`
- Modify: `internal/userenv/userenv.go`, `userenv_test.go`

- [ ] **Step 1: Write failing test**

```go
func TestAgentSandboxID(t *testing.T) {
	if got := AgentSandboxID("Alice"); got != "alice" {
		t.Fatalf("got %q", got)
	}
	if got := AgentSandboxID("Mike.Chen"); got != "mike-chen" {
		t.Fatalf("got %q", got)
	}
	if "roundpen-"+AgentSandboxID("bob") != "roundpen-bob" {
		t.Fatal("docker name contract")
	}
}
```

- [ ] **Step 2: Run test — expect fail**

Run: `go test ./internal/workspace/ -run TestAgentSandboxID -count=1`  
Expected: FAIL (`AgentSandboxID` undefined)

- [ ] **Step 3: Implement `AgentSandboxID`**

```go
// AgentSandboxID is the stable sandbox id for the user's Agent slot.
// Docker container name is "roundpen-" + AgentSandboxID (docker.containerName).
func AgentSandboxID(username string) string {
	return sanitizeWorkspaceUser(username)
}
```

Prefer one shared sanitizer for `UserWorkspaceID` and Agent (refactor `sanitizeWorkspaceUser` if `userenv.sanitizeUser` differs — Agent id must match Docker name contract).

- [ ] **Step 4: `CreateRequest.ID` + manager**

Add optional `ID string` on `CreateRequest`. In `Create`:

```go
id := strings.TrimSpace(req.ID)
if id == "" {
	id = uuid.NewString()
}
```

Existing id → `ErrConflict`.

- [ ] **Step 5: userenv sets Agent id; EnsureAgent forces Docker**

```go
if slot == SlotAgent {
	create.ID = workspace.AgentSandboxID(userID)
}
```

```go
func (s *Service) EnsureAgent(ctx context.Context, userID string) (*sandbox.Sandbox, error) {
	engine := runtime.EngineDocker
	if s.Probe != nil {
		if err := s.Probe.RequireAgent(engine); err != nil {
			return nil, err
		}
	}
	templateID := "code-agent"
	if t := strings.TrimSpace(s.Config.AgentTemplate); t != "" {
		templateID = t
	}
	sb, err := s.ensure(ctx, userID, SlotAgent, templateID, "Agent", engine)
	if err != nil {
		return nil, err
	}
	s.injectGit(ctx, userID, sb.ID)
	return sb, nil
}
```

Update tests that assumed qemu/kern for Agent.

- [ ] **Step 6: Run tests — expect pass**

Run: `go test ./internal/workspace/ ./internal/userenv/ ./internal/sandbox/ -count=1`

- [ ] **Step 7: Commit**

```bash
git add internal/workspace internal/sandbox/sandbox.go internal/sandbox/manager.go internal/userenv
git commit -m "$(cat <<'EOF'
feat(userenv): stable Agent sandbox id roundpen-{user}

Sanitize username as Agent sandbox id so Docker names match
the product contract; always Ensure Agent on Docker/code-agent.
EOF
)"
```

---

### Task 2: Guest workspace file IO (container)

**Files:**
- Create: `internal/sandbox/workspace_guest.go`, `workspace_guest_test.go`
- Modify: `internal/backend/docker/docker.go`
- Modify: `images/code-agent/Dockerfile` (add `python3-minimal` if using Python list)

Do **not** extend the full `Backend` interface. Define a narrow optional interface used by `sandbox.Service`:

```go
type workspaceCopier interface {
	CopyToWorkspace(ctx context.Context, sandboxID, destRel string, r io.Reader) error
	CopyFromWorkspace(ctx context.Context, sandboxID, srcRel string) (io.ReadCloser, error)
}
```

Docker implements via `CopyToContainer` / `CopyFromContainer` (single-file tar). `multi.Backend` should forward to the docker engine for agent sandboxes.

- [ ] **Step 1: Failing path tests**

```go
func TestGuestRelRejectsEscape(t *testing.T) {
	if _, err := guestRel(".."); err == nil {
		t.Fatal("expected error")
	}
	if _, err := guestRel("docs/a.txt"); err != nil {
		t.Fatal(err)
	}
}
```

- [ ] **Step 2: Implement `guestRel` + List/Remove via Exec**

- List: `python3` one-liner under `/workspace` emitting JSON `{name,is_dir,size}[]` (add `python3-minimal` to code-agent Dockerfile).
- Remove: `rm -rf --` only when `guestRel` OK; reject empty / `.` for delete.
- Write/Read: Docker copy APIs to `/workspace/`+rel.

- [ ] **Step 3: Expose on `sandbox.Service`**

```go
func (s *Service) ListGuestFiles(ctx context.Context, id, rel string) ([]workspace.DirEntry, error)
func (s *Service) ReadGuestFile(ctx context.Context, id, rel string) (io.ReadCloser, error)
func (s *Service) WriteGuestFile(ctx context.Context, id, rel string, r io.Reader) error
func (s *Service) RemoveGuestFile(ctx context.Context, id, rel string) error
```

Require status Running; map missing copier → clear error (`guest workspace IO requires Docker`).

- [ ] **Step 4: Unit tests pass**

Run: `go test ./internal/sandbox/ -run Guest -count=1`

- [ ] **Step 5: Commit**

```bash
git commit -m "feat(sandbox): guest workspace file IO via Agent container"
```

---

### Task 3: HTTP `/v1/me/workspace/files*`

**Files:**
- Create: `internal/api/workspaceapi/handler.go`, `handler_test.go`
- Modify: `cmd/roundpend/main.go`

- [ ] **Step 1: Implement handler**

Routes:

| Method | Path |
|--------|------|
| GET | `/v1/me/workspace/files` |
| GET | `/v1/me/workspace/files/content` |
| POST | `/v1/me/workspace/files` |
| DELETE | `/v1/me/workspace/files` |

```go
user := auth.GetUser(r.Context())
sb, err := h.Envs.EnsureAgent(r.Context(), user.Username)
// then h.Files.ListGuestFiles(ctx, sb.ID, rel)
```

Reuse path query semantics from `httpapi` (`path` default `.`). POST/DELETE require non-empty file path. Body size limit align with existing sandbox files max. Ensure failures → 503 with readable message.

- [ ] **Step 2: Handler tests with fakes**

Unauthorized; list; upload; delete; `..` → 400.

Run: `go test ./internal/api/workspaceapi/ -count=1`

- [ ] **Step 3: Mount in `main.go` beside envapi**

- [ ] **Step 4: Commit**

```bash
git commit -m "feat(api): add /v1/me/workspace file endpoints"
```

---

### Task 4: Web — nav, client, WorkspacePage

**Files:**
- Modify: `web/src/lib/appNav.ts`, `appNav.test.ts`
- Modify: `web/src/i18n/zh_CN.ts`, `en.ts`
- Modify: `web/src/api.ts`
- Create: `web/src/pages/WorkspacePage.tsx`
- Modify: `web/src/App.tsx`, `AppShell.tsx`
- Modify: `web/src/pages/AssistantDetailPage.tsx`

- [ ] **Step 1: Primary menu**

```ts
export type PrimaryMenuId = 'assistants' | 'workspace' | 'settings' | 'registry'

export const PRIMARY_MENUS: PrimaryMenu[] = [
  { id: 'assistants', to: '/a', labelKey: 'nav.assistants' },
  { id: 'workspace', to: '/workspace', labelKey: 'nav.workspace' },
  { id: 'settings', to: '/settings', labelKey: 'nav.settings' },
  { id: 'registry', to: '/registry', labelKey: 'nav.registry', admin: true },
]
```

`matchPrimaryMenu`: `/workspace` → `workspace`.  
i18n: `nav.workspace` = `工作区` / `Workspace`.

- [ ] **Step 2: `meWorkspace` in `api.ts`**

`list` / `upload` / `remove` / content URL for download (cookie auth same as `api()`).

- [ ] **Step 3: `WorkspacePage`**

Browse current path; upload file into cwd; download; delete. Show loading「正在准备工作区…」on first Ensure latency. No editor. Empty state: all assistants share this account workspace.

- [ ] **Step 4: Wire route + icon; link from 可见范围**

`App.tsx` route `/workspace`. Soften detail copy: default materials live in account workspace (shared); link「管理账号工作区」.

- [ ] **Step 5: `appNav` test + commit**

```bash
cd web && npm test -- --run appNav
git commit -m "feat(web): primary-nav Workspace page for account files"
```

---

### Task 5: Smoke verification

- [ ] **Step 1:** `docker build -t roundpen-code-agent:local images/code-agent`

- [ ] **Step 2:** Ensure + curl list/upload; `docker ps` shows `roundpen-<user>`

- [ ] **Step 3:** UI upload; Agent `Read` sees the file

- [ ] **Step 4:** Fix any gaps; commit if needed

---

## Spec coverage (this plan)

| Spec item | Task |
|-----------|------|
| Primary nav 工作区 + MVP ops | 4–5 |
| `/v1/me/workspace/files*` + Ensure + container IO | 2–3 |
| `roundpen-{user}` + Agent Docker/`code-agent` | 1 |
| 可见范围 link / shared workspace copy | 4 |
| GHCR, Kern delete, runtime settings removal | Plan 2 |

## Out of scope

Online edit, rename/move, host-direct writes, idmapped default, Browser full workspace mount, registry publish, deleting Kern package.
