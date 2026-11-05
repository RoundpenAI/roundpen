# NAS 内置助手 Implementation Plan

> **For agentic workers:** Implement milestone-by-milestone. Checkboxes track progress.

**Goal:** Ship NAS scene-pack assistants (photos / files / movies / music) on top of host write, BatchPlan, and download backends — per [spec](../specs/2026-11-05-nas-builtin-agents-design.md).

**Architecture:** Extend `hostaccess` + System Agent tools for granted-path mutations; BatchPlan as `HostApplyPlan`; scene pack via `EnsureNASScenePack` on fnOS login; download backends later (M3).

**Tech Stack:** Go (`internal/hostaccess`, `internal/acp/sysagent/tools`, `internal/automode`), React settings (`web/src`), Postgres only when indexes land (M1+).

**Spec:** `docs/superpowers/specs/2026-11-05-nas-builtin-agents-design.md`

---

## Milestones

| ID | Scope | Status |
|----|--------|--------|
| M0 | Host write + `HostApplyPlan` | done |
| M1 | Photo on-this-day index + copy | pending |
| M2 | Scene pack ensure + settings | done (ensure + settings UI) |
| M3 | Download backend (qBittorrent) | pending |
| M4 | Library gap scan + search→submit skill | pending |
| M5 | fnOS download/open APIs | pending |

---

## M0 File map

| Path | Responsibility |
|------|----------------|
| `internal/hostaccess/hostaccess.go` | `authorize` mode-aware; Rename/Move/Copy/Mkdir/Delete |
| `internal/hostaccess/hostaccess_test.go` | Write grant / deny / escape tests |
| `internal/acp/sysagent/tools/hostfiles.go` | Extend `HostFiles` interface |
| `internal/acp/sysagent/tools/hostwrite.go` | Register Host* + HostApplyPlan |
| `internal/acp/sysagent/tools/hostwrite_test.go` | Tool tests with fake writer |
| `internal/acp/sysagent/tools/session_surface.go` | Register host write on default (and implementer) |
| `internal/automode/gate.go` | L2 RequiresConfirm for Host* |
| `internal/acp/sysagent/profiles.go` | Prompt: host mutation tools |
| `docs/architecture/acp-agent-ui.md` | Tool table (if present) |

### M0 tasks

- [x] Hostaccess authorize(read\|readwrite) + create-target parent resolve
- [x] Rename / Move / Copy / Mkdir / Delete (+ recursive) with tests
- [x] Tools: `HostRename` `HostMove` `HostCopy` `HostMkdir` `HostDelete` `HostApplyPlan` (≤100 items)
- [x] Automode L2 confirm for all Host* mutators
- [x] Prompt + ListDirectoryGrants copy mentions readwrite

### M0 acceptance

Agent with `readwrite` grant can dry-run a rename list via `HostApplyPlan` after user confirm; `read` grant cannot write.

---

## Later (outline only)

**M1:** Extract OnThisDay scanner for tools; EXIF date index table; HostCopy into memories dir; optional routine.

**M2:** `nas` settings section; `EnsureNASScenePack` when `AuthProvider==fnos`; four assistant templates + skills.

**M3–M5:** per spec §7.
