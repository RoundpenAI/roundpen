# Agent Docker backend cleanup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Finish the architecture lock from the Agent=Docker workspace spec: remove Kern and Agent-QEMU as default paths, drop Agent runtime picker UI/API, publish/pull official `code-agent` OCI, and update docs.

**Architecture:** Control plane assumes Docker for Agent and QEMU only for Browser/Desktop/Mobile. `multi` backend routes agent→docker, browser→qemu. Defaults and Probe speak “Docker ready + image pull”, not three engines. CI pushes `ghcr.io/<org>/code-agent:<tag>`; config default `artifact_ref` points there (overridable).

**Tech Stack:** Go, Docker/GHCR CI, React settings UI, existing `template` / `runtime` packages.

**Spec:** `docs/superpowers/specs/2026-09-12-agent-docker-workspace-design.md` (§2, §5.5, §6 items 3–7)

**Depends on:** Prefer landing `2026-09-12-account-workspace.md` first (EnsureAgent already Docker-only). This plan can start in parallel on docs/CI once org/image name is chosen.

**Prerequisite decision (do in Task 1 before coding defaults):** Set concrete image ref, e.g. `ghcr.io/roundpenai/code-agent` (confirm GitHub org). Use that string everywhere below as `OFFICIAL_AGENT_IMAGE`.

---

## File map

| Path | Responsibility |
|------|----------------|
| `internal/runtime/` | Remove engine picker snapshot fields; Probe Docker+pull only for Agent |
| `internal/runtime/http.go` + web `RuntimePanel` | Delete or replace with Docker status panel |
| `web/src/lib/appNav.ts` / Settings | Drop `runtime` section or repurpose |
| `internal/backend/kern/` | Delete package; remove from `multi` / `main` / tests |
| `internal/template/store.go` | Drop `host` / default `agent-claude` seed for agent; keep browser-desktop |
| `internal/config` + `.env.example` | Default backend docker; `ROUNDPEN_AGENT_IMAGE` → registry tag |
| `.github/workflows/` or existing CI | Build/push code-agent |
| `README.md`, `docs/architecture/*`, compose notes | Slot table + install surfaces |
| `migrations/` | Optional: stop writing `user_runtime` prefs (table can remain unused) |

---

### Task 1: Pin official image name + config default

**Files:**
- Modify: `internal/config/config.go` (or equivalent), `.env.example`
- Modify: `internal/template/store.go` `builtinEntries` for `code-agent` ArtifactRef

- [ ] **Step 1: Choose and document `OFFICIAL_AGENT_IMAGE`**

Example: `ghcr.io/roundpenai/code-agent:0.1.0` and floating `:latest` for dev — pick one semver policy in a short comment in `.env.example`.

- [ ] **Step 2: Default ArtifactRef**

```go
ArtifactRef: getenv("ROUNDPEN_AGENT_IMAGE", "ghcr.io/roundpenai/code-agent:0.1.0"),
```

for `code-agent` seed (not local-only). Keep local override via env for developers: `roundpen-code-agent:local`.

- [ ] **Step 3: Commit**

```bash
git commit -m "chore(config): default Agent image to official registry ref"
```

---

### Task 2: Remove Agent runtime picker (API + UI)

**Files:**
- Modify: `internal/runtime/probe.go`, `http.go`, tests
- Modify: `web/src/components/RuntimePanel.tsx` or replace
- Modify: `web/src/pages/SettingsPage.tsx`, `appNav.ts`, i18n

- [ ] **Step 1: Probe snapshot**

Replace three-engine list with Agent readiness:

```go
type Snapshot struct {
	DockerReady bool     `json:"dockerReady"`
	DockerErr   string   `json:"dockerErr,omitempty"`
	AgentImage  string   `json:"agentImage"`
	ImageLocal  bool     `json:"agentImageLocal"`
	Missing     []string `json:"missing,omitempty"`
	Setup       []SetupStep `json:"setup,omitempty"`
}
```

Setup steps: `docker pull $AGENT_IMAGE` and offline `docker load` doc link/command. Browser readiness stays under browser settings / existing QEMU probe if needed — not as “pick Agent engine”.

- [ ] **Step 2: Stop persisting user Agent engine preference**

`EnsureAgent` already ignores prefs (Plan 1). Remove or no-op `PUT` agent-engine endpoints; delete Settings「Agent 运行时」section or convert to read-only Docker status.

Default settings landing: change `resolveSettingsSection` fallback from `runtime` → `git` or `general`.

- [ ] **Step 3: Tests + commit**

```bash
go test ./internal/runtime/ -count=1
git commit -m "feat(runtime): replace Agent engine picker with Docker status"
```

---

### Task 3: Remove Kern backend

**Files:**
- Delete: `internal/backend/kern/`
- Modify: `internal/backend/multi/multi.go`, `cmd/roundpend/main.go`, `internal/config`
- Modify: integration harness / tests importing kern
- Modify: template seed `host` entry — remove or mark unavailable

- [ ] **Step 1: Make `go test ./...` fail by removing kern from multi wiring first, then delete package**

Default `ROUNDPEN_BACKEND=docker` (or `multi` with docker+qemu only). Reject `backend=kern` at config load with clear error.

- [ ] **Step 2: Fix all compile errors; update security.md / README that mention Kern as default**

- [ ] **Step 3: Commit**

```bash
git commit -m "refactor: remove Kern backend; Agent requires Docker"
```

---

### Task 4: Demote Agent-QEMU defaults

**Files:**
- Modify: `internal/template/store.go` — remove or un-seed `agent-claude` as agent default
- Modify: `internal/runtime/engine.go` — delete qemu→agent-claude mapping if unused
- Modify: `docs/architecture/qemu-agent.md` — mark historical / unsupported default
- Keep: `images/agent-qemu/` tree optional for now (delete in a later PR if desired) **or** delete in this task if no references remain

- [ ] **Step 1: Grep for `agent-claude` / `EngineQEMU` agent paths; remove Agent QEMU Ensure branches**

Browser must still use QEMU.

- [ ] **Step 2: Commit**

```bash
git commit -m "chore: demote Agent-QEMU; Browser remains on QEMU"
```

---

### Task 5: CI push code-agent + docs install surfaces

**Files:**
- Create/modify: `.github/workflows/code-agent-image.yml` (or project’s CI)
- Modify: `README.md`, `deploy/compose/README.md`, `.env.example`
- Modify: Probe setup strings to `docker pull …` / `docker load -i code-agent.tar`

- [ ] **Step 1: Workflow**

On tag/release: `docker build -t $OFFICIAL_AGENT_IMAGE images/code-agent && docker push`

- [ ] **Step 2: README slots table**

| Slot | Backend |
|------|---------|
| Agent | Docker (official OCI pull) |
| Browser / Desktop / Mobile | QEMU |

Install: Compose / single binary + Docker pull / offline load.

- [ ] **Step 3: Commit**

```bash
git commit -m "ci(docs): publish code-agent image and document pull/load install"
```

---

## Spec coverage

| Spec §6 item | Task |
|--------------|------|
| 3 Remove runtime selection; Docker Ensure | 2 (+ Plan 1) |
| 4 Delete Kern | 3 |
| 5 Agent-QEMU demote | 4 |
| 6 CI + artifact_ref + Probe pull/load | 1, 5 |
| 7 README / architecture | 5 |

## Out of scope

Workspace UI (Plan 1), idmapped mounts, Browser workspace sharing, Kaniko removal beyond “not required for Agent happy path” (admin builds settings can be a later trim).
