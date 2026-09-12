# Assistant Setup Wizard Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend assistant creation so the wizard first ensures LLM gateway is ready, then auto-detects host gaps, builds a whitelist install plan (LLM optional, deterministic fallback), runs installs with visible progress, and only creates the assistant after the workstation is ready.

**Architecture:** New `internal/hostsetup` package owns privilege probe (`root` / `sudo -n` / `manual`), action catalog, in-memory plan+run state, runner (apt / `images/*/build.sh`), and HTTP under `/v1/setup/*`. Planner prefers LLMGW chat via `vk-roundpen-internal`; on failure uses Probe→actions mapping. Web `AssistantCreatePage` gains conditional「接通大脑」+「准备工位」steps; create API is only called after required actions succeed.

**Tech Stack:** Go control plane, existing `runtime.Probe` + LLMGW, React create wizard.

**Spec:** `docs/superpowers/specs/2026-09-12-assistant-setup-wizard-design.md`

---

## File map

| Path | Responsibility |
|------|----------------|
| `internal/hostsetup/privilege.go` | Detect `auto` vs `manual` privilege (root / `sudo -n`) |
| `internal/hostsetup/actions.go` | Whitelist action IDs, titles, sensitivity, commands |
| `internal/hostsetup/plan.go` | Deterministic plan from Probe + wizard context |
| `internal/hostsetup/llm_plan.go` | Optional LLM JSON plan; reconcile against whitelist |
| `internal/hostsetup/runner.go` | Execute actions with per-id lock, log ring buffer |
| `internal/hostsetup/store.go` | In-memory plans/runs (YAGNI: no DB) |
| `internal/hostsetup/http.go` | `/v1/setup/*` routes |
| `internal/hostsetup/*_test.go` | Unit tests |
| `cmd/roundpend/main.go` | Mount handler; inject Probe, Cfg, repo root, LLMGW |
| `web/src/api.ts` | Setup client types + methods |
| `web/src/pages/AssistantCreatePage.tsx` | Full wizard UI |
| `web/src/components/SetupWorkstation.tsx` | Action list + expand logs + confirm/recheck |

---

### Task 1: Privilege probe

**Files:**
- Create: `internal/hostsetup/privilege.go`
- Create: `internal/hostsetup/privilege_test.go`

- [ ] **Step 1: Write failing tests**

```go
package hostsetup

import "testing"

func TestClassifyPrivilegeRoot(t *testing.T) {
	if got := classifyPrivilege(true, false); got != PrivilegeAuto {
		t.Fatalf("got %s", got)
	}
}

func TestClassifyPrivilegeSudoN(t *testing.T) {
	if got := classifyPrivilege(false, true); got != PrivilegeAuto {
		t.Fatalf("got %s", got)
	}
}

func TestClassifyPrivilegeManual(t *testing.T) {
	if got := classifyPrivilege(false, false); got != PrivilegeManual {
		t.Fatalf("got %s", got)
	}
}
```

- [ ] **Step 2: Run test — expect FAIL**

Run: `go test ./internal/hostsetup/ -run TestClassifyPrivilege -count=1`

- [ ] **Step 3: Implement**

```go
package hostsetup

import (
	"os"
	"os/exec"
	"sync"
	"time"
)

type Privilege string

const (
	PrivilegeAuto   Privilege = "auto"
	PrivilegeManual Privilege = "manual"
)

func classifyPrivilege(isRoot, sudoNOK bool) Privilege {
	if isRoot || sudoNOK {
		return PrivilegeAuto
	}
	return PrivilegeManual
}

var (
	privMu     sync.Mutex
	privCached Privilege
	privAt     time.Time
)

// ProbePrivilege returns auto if euid==0 or `sudo -n true` succeeds.
// Result cached ~30s. Never prompts for a password.
func ProbePrivilege() Privilege {
	privMu.Lock()
	defer privMu.Unlock()
	if time.Since(privAt) < 30*time.Second && privCached != "" {
		return privCached
	}
	isRoot := os.Geteuid() == 0
	sudoNOK := false
	if !isRoot {
		if _, err := exec.LookPath("sudo"); err == nil {
			cmd := exec.Command("sudo", "-n", "true")
			sudoNOK = cmd.Run() == nil
		}
	}
	privCached = classifyPrivilege(isRoot, sudoNOK)
	privAt = time.Now()
	return privCached
}
```

- [ ] **Step 4: Run tests — expect PASS**

Run: `go test ./internal/hostsetup/ -run TestClassifyPrivilege -count=1`

- [ ] **Step 5: Commit**

```bash
git add internal/hostsetup/privilege.go internal/hostsetup/privilege_test.go
git commit -m "feat(hostsetup): probe non-interactive privilege for apt"
```

---

### Task 2: Action catalog + deterministic planner

**Files:**
- Create: `internal/hostsetup/actions.go`
- Create: `internal/hostsetup/plan.go`
- Create: `internal/hostsetup/plan_test.go`

- [ ] **Step 1: Failing test — browser preset pulls browser image when missing**

```go
package hostsetup

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/runtime"
)

func TestDeterministicPlanAgentAndBrowser(t *testing.T) {
	ctx := WizardContext{Preset: "code_browser"}
	snap := runtime.Snapshot{
		Engines: []runtime.EngineStatus{{
			ID:      runtime.EngineQEMU,
			Missing: []string{"QEMU binaries (qemu-system-x86_64, qemu-img)", "agent qcow2 image", "browser qcow2 image"},
		}},
	}
	p := DeterministicPlan(ctx, snap, PrivilegeManual)
	ids := actionIDs(p)
	if !contains(ids, ActionInstallQEMU) || !contains(ids, ActionBuildAgentImage) || !contains(ids, ActionBuildBrowserImage) {
		t.Fatalf("ids=%v", ids)
	}
	for _, a := range p.Actions {
		if a.ID == ActionInstallQEMU && a.Privilege != PrivilegeManual {
			t.Fatalf("qemu privilege: %s", a.Privilege)
		}
	}
}

func TestDeterministicPlanEmptyWhenReady(t *testing.T) {
	snap := runtime.Snapshot{
		Engines: []runtime.EngineStatus{{
			ID:         runtime.EngineQEMU,
			AgentReady: true,
		}},
	}
	p := DeterministicPlan(WizardContext{Preset: "code"}, snap, PrivilegeAuto)
	if len(p.Actions) != 0 {
		t.Fatalf("want empty, got %#v", p.Actions)
	}
}
```

Helper `actionIDs` / `contains` in test file.

- [ ] **Step 2: Run — expect FAIL**

Run: `go test ./internal/hostsetup/ -run TestDeterministicPlan -count=1`

- [ ] **Step 3: Implement catalog + planner**

`actions.go` constants:

```go
const (
	ActionInstallQEMU       = "install_qemu"
	ActionBuildAgentImage   = "build_agent_image"
	ActionBuildBrowserImage = "build_browser_image"
)

type ActionDef struct {
	ID       string
	Title    string
	Sensitive bool
	// CommandHint is shown in UI / copy-paste (never model-supplied).
	CommandHint func(isRoot bool) string
}
```

Register three defs. QEMU hint: root → `apt-get install -y qemu-system-x86 qemu-utils`; else → `sudo apt-get install -y qemu-system-x86 qemu-utils`. Build hints: `make agent-image` / `make browser-image`.

`plan.go`:

```go
type WizardContext struct {
	Name         string `json:"name"`
	Bio          string `json:"bio"`
	IdentityMode string `json:"identityMode"`
	Preset       string `json:"preset"`
	NetworkTier string `json:"networkTier,omitempty"`
}

type PlannedAction struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Reason    string    `json:"reason"`
	Sensitive bool      `json:"sensitive"`
	Privilege Privilege `json:"privilege,omitempty"` // only for sensitive
	Command   string    `json:"command"`
}

type Plan struct {
	Summary string          `json:"summary"`
	Actions []PlannedAction `json:"actions"`
}

func DeterministicPlan(ctx WizardContext, snap runtime.Snapshot, priv Privilege) Plan {
	// Find qemu engine; inspect Missing strings / AgentReady / BrowserReady.
	// Always require agent image+binaries for default QEMU path unless AgentReady.
	// If preset is code_browser OR capabilities imply browser, also browser image when !BrowserReady.
	// Map missing → whitelist actions; set Privilege on install_qemu only.
	// Summary in Chinese, short.
}
```

Match Probe missing text loosely (`strings.Contains`) or better: add exported helpers on Probe later if needed. Prefer checking `AgentReady`/`BrowserReady` and separately `qemu.BinariesAvailable()` / `qemu.ValidateImage` via injecting callbacks on `Planner` struct to keep tests fakeable:

```go
type HostFacts struct {
	BinariesOK   bool
	AgentImageOK bool
	BrowserImageOK bool
}

func PlanFromFacts(ctx WizardContext, f HostFacts, priv Privilege) Plan
```

`DeterministicPlan` can build `HostFacts` from Probe in production code.

- [ ] **Step 4: Tests PASS**

Run: `go test ./internal/hostsetup/ -run TestDeterministicPlan -count=1`

- [ ] **Step 5: Commit**

```bash
git add internal/hostsetup/actions.go internal/hostsetup/plan.go internal/hostsetup/plan_test.go
git commit -m "feat(hostsetup): deterministic workstation plan from host facts"
```

---

### Task 3: Runner (execute whitelist actions)

**Files:**
- Create: `internal/hostsetup/runner.go`
- Create: `internal/hostsetup/runner_test.go`

- [ ] **Step 1: Test lock + log append with fake exec**

```go
func TestRunnerRejectsUnknownAction(t *testing.T) {
	r := NewRunner(RunnerConfig{RepoRoot: t.TempDir()})
	err := r.Run(context.Background(), "nope", PrivilegeAuto, io.Discard)
	if err == nil {
		t.Fatal("expected error")
	}
}
```

Use an `execFunc` field on Runner for tests of `install_qemu` building argv:

```go
func TestInstallQEMUArgvRoot(t *testing.T) {
	argv := installQEMUArgv(true) // []string{"apt-get", "install", "-y", "qemu-system-x86", "qemu-utils"}
	// assert
}
func TestInstallQEMUArgvSudoN(t *testing.T) {
	argv := installQEMUArgv(false) // sudo -n apt-get ...
}
```

- [ ] **Step 2: Implement runner**

```go
type Runner struct {
	RepoRoot string
	mu       sync.Mutex
	locks    map[string]*sync.Mutex // per action id
	// LookPath / Command overridable in tests if needed
}

func (r *Runner) Run(ctx context.Context, actionID string, priv Privilege, log io.Writer) error {
	// acquire per-id lock
	switch actionID {
	case ActionInstallQEMU:
		if priv != PrivilegeAuto {
			return fmt.Errorf("install_qemu requires privilege=auto (use manual copy-paste flow)")
		}
		argv := installQEMUArgv(os.Geteuid() == 0)
		return r.runCmd(ctx, argv[0], argv[1:], log)
	case ActionBuildAgentImage:
		return r.runCmd(ctx, "bash", []string{filepath.Join(r.RepoRoot, "images/agent-qemu/build.sh")}, log)
	case ActionBuildBrowserImage:
		return r.runCmd(ctx, "bash", []string{filepath.Join(r.RepoRoot, "images/browser-qemu/build.sh")}, log)
	default:
		return fmt.Errorf("unknown action %q", actionID)
	}
}
```

`runCmd`: `exec.CommandContext`, stdout+stderr to `log`, return error on non-zero.

Repo root: from `ROUNDPEN_REPO_ROOT` or walk up from cwd for `images/agent-qemu/build.sh`.

- [ ] **Step 3: Tests PASS + commit**

```bash
git add internal/hostsetup/runner.go internal/hostsetup/runner_test.go
git commit -m "feat(hostsetup): run whitelist setup actions with logging"
```

---

### Task 4: In-memory plan store + action run state machine

**Files:**
- Create: `internal/hostsetup/store.go`
- Create: `internal/hostsetup/store_test.go`

- [ ] **Step 1: Tests for confirm / recheck / auto-start**

States: `pending_confirm` | `pending_manual` | `queued` | `running` | `succeeded` | `failed` | `skipped`

```go
func TestCreatePlanStartsNonSensitive(t *testing.T) {
	// plan with only build_agent_image → status becomes running/queued then succeeded with fake runner
}

func TestSensitiveManualStaysPendingManual(t *testing.T) {
	// install_qemu + PrivilegeManual → pending_manual; Confirm is no-op or returns error directing to Recheck
}

func TestSensitiveAutoConfirmRuns(t *testing.T) {
	// Confirm → running → succeeded via fake
}

func TestRecheckSucceedsWhenFactOK(t *testing.T) {
	// pending_manual; after facts say binaries OK → succeeded
}
```

- [ ] **Step 2: Implement `Service`**

```go
type Service struct {
	Probe    *runtime.Probe
	Runner   *Runner
	Facts    func() HostFacts // default from Probe+qemu
	PlanLLM  func(ctx context.Context, w WizardContext, f HostFacts) (Plan, error) // optional
	plans    sync.Map // id → *PlanRecord
}

type ActionRun struct {
	ActionID  string    `json:"actionId"`
	Title     string    `json:"title"`
	Reason    string    `json:"reason"`
	Status    string    `json:"status"`
	Privilege Privilege `json:"privilege,omitempty"`
	Command   string    `json:"command"`
	Error     string    `json:"error,omitempty"`
	Log       string    `json:"log,omitempty"` // last N KB
}

type PlanRecord struct {
	ID        string      `json:"id"`
	User      string      `json:"-"`
	Summary   string      `json:"summary"`
	Context   WizardContext `json:"context"`
	Actions   []ActionRun `json:"actions"`
	CreatedAt time.Time   `json:"createdAt"`
}
```

`CreatePlan`: facts → try PlanLLM → on err/empty invalid use `PlanFromFacts` → reconcile → spawn goroutines for non-sensitive / auto-queued after create.

`Confirm(planID, actionID)`: only if `pending_confirm` && privilege auto.

`Recheck(planID, actionID)`: refresh facts; if satisfied mark succeeded.

`Retry`: failed → re-queue.

`Get` / `Ready()`: all required succeeded.

Log: append to strings.Builder capped at ~256KiB per action.

- [ ] **Step 3: PASS + commit**

```bash
git add internal/hostsetup/store.go internal/hostsetup/store_test.go
git commit -m "feat(hostsetup): in-memory setup plan runs and confirm/recheck"
```

---

### Task 5: LLM readiness + HTTP API

**Files:**
- Create: `internal/hostsetup/llm_ready.go`
- Create: `internal/hostsetup/http.go`
- Create: `internal/hostsetup/http_test.go`
- Modify: `cmd/roundpend/main.go` (wire)

- [ ] **Step 1: `LLMReady(cfg)` helper**

```go
func LLMReady(cfg *config.Config) (ready bool, reason string) {
	if cfg == nil || !cfg.LLMGW.Enabled {
		return false, "LLM gateway 未启用"
	}
	hasOpenAI := cfg.LLMGW.OpenAI != nil && strings.TrimSpace(cfg.LLMGW.OpenAI.BaseURL) != "" && strings.TrimSpace(cfg.LLMGW.OpenAI.APIKey) != ""
	hasAnthropic := cfg.LLMGW.Anthropic != nil && strings.TrimSpace(cfg.LLMGW.Anthropic.BaseURL) != "" && strings.TrimSpace(cfg.LLMGW.Anthropic.APIKey) != ""
	if !hasOpenAI && !hasAnthropic {
		return false, "尚未配置上游模型地址与密钥"
	}
	if strings.TrimSpace(cfg.LLMGW.DefaultModel) == "" {
		return false, "尚未设置默认模型"
	}
	return true, ""
}
```

- [ ] **Step 2: HTTP routes**

```
GET  /v1/setup/llm-ready          → { ready, reason }
POST /v1/setup/plans              body WizardContext → PlanRecord (202/200)
GET  /v1/setup/plans/{id}         → PlanRecord
POST /v1/setup/plans/{id}/actions/{actionId}/confirm
POST /v1/setup/plans/{id}/actions/{actionId}/recheck
POST /v1/setup/plans/{id}/actions/{actionId}/retry
```

Auth: same as other `/v1/*` — `auth.GetUser`; plans owned by username.

httptest```go
func TestLLMReadyUnauthorized(t *testing.T) { /* 401 */ }
func TestCreatePlanEmptyActions(t *testing.T) {
	// Service with Facts all OK → actions []
}
```

- [ ] **Step 3: Mount in `main.go`**

```go
setupSvc := &hostsetup.Service{
	Probe:  probe,
	Runner: hostsetup.NewRunner(hostsetup.RunnerConfig{RepoRoot: mustRepoRoot()}),
	Cfg:    cfg,
}
(&hostsetup.Handler{Svc: setupSvc, Cfg: cfg}).Mount(mux)
```

- [ ] **Step 4: `go test ./internal/hostsetup/ -count=1` PASS + commit**

```bash
git add internal/hostsetup/llm_ready.go internal/hostsetup/http.go internal/hostsetup/http_test.go cmd/roundpend/main.go
git commit -m "feat(hostsetup): expose /v1/setup LLM-ready and plan APIs"
```

---

### Task 6: Optional LLM planner (with fallback)

**Files:**
- Create: `internal/hostsetup/llm_plan.go`
- Create: `internal/hostsetup/llm_plan_test.go`
- Modify: `internal/hostsetup/store.go` (wire PlanLLM)
- Modify: `cmd/roundpend/main.go`

- [ ] **Step 1: Test reconcile drops unknown ids and fills commands from catalog**

```go
func TestReconcilePlanDropsUnknown(t *testing.T) {
	raw := Plan{Summary: "x", Actions: []PlannedAction{{ID: "rm_rf", Title: "bad"}, {ID: ActionBuildAgentImage, Title: "盘"}}}
	out := Reconcile(raw, HostFacts{BinariesOK: true, AgentImageOK: false}, PrivilegeAuto, WizardContext{Preset: "code"})
	if len(out.Actions) != 1 || out.Actions[0].ID != ActionBuildAgentImage {
		t.Fatalf("%#v", out.Actions)
	}
	if out.Actions[0].Command == "" {
		t.Fatal("command required from catalog")
	}
}
```

- [ ] **Step 2: Implement `PlanWithLLM`**

POST to local LLMGW OpenAI-compatible chat (reuse pattern from `llmgw.Embed`: HTTP to `g.publicBase()/v1/chat/completions` with `Authorization: Bearer vk-roundpen-internal`). If Anthropic-only gateway, either skip LLM or use Anthropic messages — **YAGNI**: if OpenAI upstream missing, return error and let CreatePlan fall back.

System prompt (constant): Chinese summary; only emit JSON `{summary,actions:[{id,title,reason}]}`; ids must be from whitelist list embedded in prompt; no shell.

Parse JSON (strip markdown fences if present). `Reconcile` overwrites title/command/sensitive/privilege from catalog + facts (drop actions already satisfied).

- [ ] **Step 3: Wire `Service.PlanLLM` in main when gateway enabled**

- [ ] **Step 4: Tests PASS + commit**

```bash
git add internal/hostsetup/llm_plan.go internal/hostsetup/llm_plan_test.go internal/hostsetup/store.go cmd/roundpend/main.go
git commit -m "feat(hostsetup): optional LLM plan with whitelist reconcile"
```

---

### Task 7: Web API client

**Files:**
- Modify: `web/src/api.ts`

- [ ] **Step 1: Add types + `setupApi`**

```ts
export type SetupPrivilege = 'auto' | 'manual'

export type SetupActionRun = {
  actionId: string
  title: string
  reason: string
  status: string
  privilege?: SetupPrivilege
  command: string
  error?: string
  log?: string
}

export type SetupPlan = {
  id: string
  summary: string
  context: {
    name: string
    bio: string
    identityMode: string
    preset: string
  }
  actions: SetupActionRun[]
  createdAt: string
}

export const setupApi = {
  llmReady: () => api<{ ready: boolean; reason?: string }>('/v1/setup/llm-ready'),
  createPlan: (body: SetupPlan['context']) =>
    api<SetupPlan>('/v1/setup/plans', { method: 'POST', body: JSON.stringify(body) }),
  getPlan: (id: string) => api<SetupPlan>(`/v1/setup/plans/${id}`),
  confirm: (planId: string, actionId: string) =>
    api<SetupPlan>(`/v1/setup/plans/${planId}/actions/${actionId}/confirm`, { method: 'POST' }),
  recheck: (planId: string, actionId: string) =>
    api<SetupPlan>(`/v1/setup/plans/${planId}/actions/${actionId}/recheck`, { method: 'POST' }),
  retry: (planId: string, actionId: string) =>
    api<SetupPlan>(`/v1/setup/plans/${planId}/actions/${actionId}/retry`, { method: 'POST' }),
}
```

- [ ] **Step 2: Commit**

```bash
git add web/src/api.ts
git commit -m "feat(web): setup wizard API client"
```

---

### Task 8: `SetupWorkstation` UI component

**Files:**
- Create: `web/src/components/SetupWorkstation.tsx`

- [ ] **Step 1: Implement presentational + polling**

Props: `planId: string`, `onReady: () => void`, `onError?: (msg: string) => void`

- On mount + every 1.5s: `setupApi.getPlan(planId)` until all actions `succeeded` or `skipped` (or actions empty).  
- Render `summary`.  
- Each action row: title, status badge, buttons:
  - `pending_confirm` + auto → 「允许并安装」→ confirm  
  - `pending_manual` → copy `command` + 「我已装好，重新检测」→ recheck  
  - `failed` → 「重试」→ retry  
- Expandable `<details>`: command + `<pre>` log + error  
- When ready, call `onReady` once.

Use existing Tailwind/daisy patterns from create page (no new card chrome beyond borders already used).

- [ ] **Step 2: Commit**

```bash
git add web/src/components/SetupWorkstation.tsx
git commit -m "feat(web): setup workstation progress UI"
```

---

### Task 9: Expand `AssistantCreatePage` wizard

**Files:**
- Modify: `web/src/pages/AssistantCreatePage.tsx`

- [ ] **Step 1: Step machine**

```ts
type Step = 'llm' | 'name' | 'identity' | 'preset' | 'setup' | 'done'
```

- On mount: `setupApi.llmReady()` → if !ready start at `llm`, else `name`.  
- `llm` step: explain + link to `/settings` (admin) or 「请联系管理员」; button「已配置，重新检查」.  
- Keep existing name / identity / preset UI; update step indicator (动态总数).  
- After preset: `createPlan({ name, bio, identityMode, preset })` → go `setup` with plan id.  
- `setup`: `<SetupWorkstation planId onReady={() => void finalize()} />`.  
- `finalize`: existing `assistantsApi.create` + `ensureSession` + navigate (only once).  
- Do **not** create assistant before setup ready.

- [ ] **Step 2: Manual smoke**

With LLM configured and missing qcow2: wizard shows build action running; after image exists, create succeeds.

- [ ] **Step 3: Commit**

```bash
git add web/src/pages/AssistantCreatePage.tsx
git commit -m "feat(web): assistant create wizard with LLM gate and workstation"
```

---

### Task 10: Harden create path + docs touch

**Files:**
- Modify: `internal/assistant/http.go` (optional): if desired, reject create when default engine not agent-ready — **prefer UI gate only** this plan to avoid breaking API scripts; document that.  
- Modify: `README.md` or `docs/architecture/qemu-agent.md`: one paragraph pointing at wizard auto-build + sudo probe behavior.

- [ ] **Step 1: Short doc note under qemu-agent “首次使用”**

- [ ] **Step 2: Full test**

```bash
go test ./internal/hostsetup/ ./internal/runtime/ -count=1
cd web && npx tsc --noEmit
```

- [ ] **Step 3: Commit**

```bash
git add docs/architecture/qemu-agent.md
git commit -m "docs: note setup wizard auto-prepares QEMU workstation"
```

---

## Spec coverage checklist

| Spec item | Task |
|-----------|------|
| 接通大脑门禁 | 5, 9 |
| 无创建聊天 | 9 (forms only) |
| Probe → plan → exec | 2–4 |
| 白名单动作 | 2, 3 |
| sudo 探测 auto/manual | 1, 4, 8 |
| 可见进度 + 展开日志 | 8 |
| LLM 规划 + 确定性回退 | 6, 4 |
| 创建前工位就绪 | 9 |
| 不收集 sudo 密码 | 1, 3, 8 |

## Self-review notes

- No TBD placeholders; in-memory store is an explicit YAGNI choice (spec §10).  
- Action IDs stable across Go/TS.  
- LLM path optional; wizard works with deterministic planner alone if LLMGW chat fails.
