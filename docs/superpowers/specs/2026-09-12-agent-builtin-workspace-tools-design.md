# Agent builtin workspace tools (Claude Code–aligned)

Date: 2026-09-12  
Status: approved for planning  
Scope: System Agent (`internal/acp/sysagent/tools`) agent-facing surface

## Problem

System Agent currently exposes workspace work through `sandbox_exec` / `roundpen_ensure_*`, and tool descriptions plus the system prompt talk about sandboxes, sandbox ids, and QEMU. Models should not reason about isolation plumbing. There are also no first-class file tools, so agents shell their way through reads and edits.

## Goals

- Agent-visible workspace tools use Claude Code–style names: `Read`, `Write`, `Edit`, `Glob`, `Grep`, `Bash`.
- Sandbox / guest / QEMU / sandbox id are implementation details only (logs OK; tool results, descriptions, and system prompt must not surface them).
- Environment ensure is implicit on first relevant tool use.
- Platform tools are renamed and scrubbed in the same iteration.
- Paths use guest convention: working root `/workspace`.

## Non-goals (this iteration)

- Replacing tools inside coding ACP (`claude-agent-acp`) runtimes
- `NotebookEdit`, `LSP`, `WebFetch`, `WebSearch`, computer-use
- Full Claude Code parity (image Read, background Bash, identical truncation heuristics)
- Multi-root / assistant directory grants beyond `/workspace`
- Keeping legacy tool-name aliases for the LLM (`sandbox_exec`, etc.)

## Approach

Structured builtins (not shell-only wrappers). Core parameter names align with Claude Code; behavior is an MVP we own. File mutate tools use control-plane workspace file APIs; search/shell run in the agent environment via existing `Exec`.

## Agent-facing tool catalog

### Workspace builtins

| Tool | Purpose | Mutating | Parameters |
|------|---------|----------|------------|
| `Read` | Read text file | no | `file_path`, `offset?`, `limit?` |
| `Write` | Create or overwrite whole file | yes | `file_path`, `content` |
| `Edit` | Exact string replacement | yes | `file_path`, `old_string`, `new_string`, `replace_all?` |
| `Glob` | Find files by path pattern | no | `pattern`, `path?` (default `/workspace`) |
| `Grep` | Search file contents (`rg` in guest) | no | `pattern`, `path?`, `glob?`, `case_insensitive?`, `output_mode?` |
| `Bash` | Shell command (replaces `sandbox_exec`) | yes | `command`, `workdir?` (default `/workspace`), `timeout?` |

### Platform / browser (same iteration)

| Old | New | Notes |
|-----|-----|-------|
| `sandbox_exec` | `Bash` | Old name removed from registry |
| `roundpen_ensure_agent` | *(removed)* | Implicit ensure before workspace tools |
| `roundpen_ensure_browser` | *(removed)* | Implicit ensure on first `browser_*` |
| `roundpen_list_environments` | `ListEnvironments` | Slot status only. Tool handler must strip/transform control-plane fields such as `sandboxId` before returning JSON to the model; note text must not mention sandbox or ensure tools |
| `roundpen_list_templates` | `ListTemplates` | |
| `roundpen_list_agent_sessions` | `ListSessions` | |
| `roundpen_get_settings` | `GetSettings` | admin only |
| `browser_*` | keep `browser_*` | Scrub descriptions; ensure stays inside binder |

### Language rules for anything the model sees

Forbidden: `sandbox`, `sandboxId`, `QEMU`, `guest`, host bind-mount paths, credential file paths used for injection.

Allowed: workspace, working directory, `/workspace`, Browser, Agent workspace / Environment (slot status).

## Paths

1. Agent paths are absolute under `/workspace` or relative to `/workspace` (for `Bash`, relative to `workdir` when set).
2. Resolve with `filepath.Clean`; reject `..` escapes outside `/workspace`.
3. Map `/workspace/foo` → workspace-relative `foo` for `Manager.ReadFile` / `WriteFile`.
4. This iteration: only `/workspace` is in-bounds (no extra authorized roots).

## Implicit ensure

- Before `Read` / `Write` / `Edit` / `Glob` / `Grep` / `Bash`: call existing agent-slot ensure. On failure return a model-safe error such as `Agent workspace is not available`.
- Before `browser_*`: existing browser binder ensure; errors scrubbed of implementation detail.
- No ensure tools remain in the OpenAI tools list.
- System prompt: tell the model to use file/`Bash` tools directly; environment becomes ready as needed. `ListEnvironments` with `absent` means not started yet — calling a workspace tool is enough.

## Execution backends

| Tool | Backend |
|------|---------|
| `Read` / `Write` / `Edit` | Control-plane sandbox manager file APIs (existing host-side / manager path). `Edit` = read → exact match → write |
| `Glob` / `Grep` | Guest `Exec` (`find` / `rg`). If `rg` missing, `Grep` returns a clear error |
| `Bash` | Guest `Exec` `/bin/sh -c`, same gitcred env injection as today’s exec path |

### Result shapes (MVP)

- `Bash`: JSON `{exitCode, stdout, stderr}` with existing-scale truncation.
- `Read`: line-numbered text; empty-file notice; use `offset`/`limit` for large files.
- `Edit`: zero matches → error; multiple matches without `replace_all` → error asking for more context or `replace_all: true`.
- Tool results must not include `sandboxId` or other isolation identifiers. Ops may log ids server-side only.

## Permissions

Reuse the existing Mutating permission gate and session remember options (Allow once / Allow this tool / Allow all tools / Reject…).

- Non-mutating: `Read`, `Glob`, `Grep`, `ListEnvironments`, `ListTemplates`, `ListSessions`, `GetSettings`, read-only `browser_*`
- Mutating: `Write`, `Edit`, `Bash`, mutating `browser_*`

## System prompt changes

Replace Cloud Agent / `sandbox_exec` / ensure instructions with:

- Use `Read` / `Write` / `Edit` / `Glob` / `Grep` / `Bash` against `/workspace`.
- Use `browser_*` for Chrome; it cannot run shell or git.
- Use `ListEnvironments` only for status; do not stop after listing.
- Git: user-saved tokens are applied automatically for `git` inside `Bash`; do not embed tokens in URLs or inspect credential files.

## Compatibility

- Registry: hard cutover to new names (no LLM-facing aliases).
- Persisted history may still contain old tool names; replay continues to work; new turns only advertise new tools.
- Update `docs/architecture/acp-agent-ui.md` tool table.
- Update UI helpers that key off old prefixes (e.g. `web/src/lib/toolStats.ts`).

## Package / code touchpoints (expected)

- `internal/acp/sysagent/tools/` — new file tools module; rename shell registration to `Bash`; scrub `roundpen.go` / `browser.go` / registry tests
- `internal/acp/sysagent/history.go` — system prompt
- `internal/acp/sysagent/loop.go` — nudges that mention ensure / sandbox_exec
- Docs + `toolStats` as above

## Testing

- Unit: path escape rejection; `Edit` 0 / 1 / many matches; ensure invoked once per call path; registry has no `sandbox*` or `roundpen_ensure_*`; descriptions contain no `sandbox`
- Update `shell_test`, `registry_test`, history projection tests for new names
- Out of scope: image Read, notebook tools, background Bash

## Success criteria

1. System Agent tool list exposed to the LLM is Claude Code–named for workspace ops and has no ensure/sandbox-named tools.
2. Agent can read/edit files and run commands in `/workspace` without being told about sandboxes.
3. Existing permission UX still gates writes.
4. Docs and prompt match the new surface.
