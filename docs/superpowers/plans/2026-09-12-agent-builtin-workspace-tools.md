# Agent Builtin Workspace Tools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Expose Claude Code–style `Read`/`Write`/`Edit`/`Glob`/`Grep`/`Bash` to the System Agent LLM, hide sandbox plumbing, and scrub/rename platform tools in the same cutover.

**Architecture:** Extend `AgentBinder` with workspace file APIs; add path helpers + file/search tools; rename shell to `Bash` with implicit ensure; rename Roundpen tools and strip model-facing ids; update system prompt, loop nudges, docs, and UI tool classification.

**Tech Stack:** Go (`internal/acp/sysagent/tools`), existing `sandbox.Manager` file/exec APIs, React `toolStats`, markdown docs.

**Spec:** `docs/superpowers/specs/2026-09-12-agent-builtin-workspace-tools-design.md`

---

## File map

| Path | Responsibility |
|------|----------------|
| `internal/acp/sysagent/tools/path.go` | Resolve `/workspace` paths → relative; reject escapes |
| `internal/acp/sysagent/tools/path_test.go` | Path unit tests |
| `internal/acp/sysagent/tools/files.go` | Register `Read`/`Write`/`Edit` |
| `internal/acp/sysagent/tools/files_test.go` | File tool tests (fake FS) |
| `internal/acp/sysagent/tools/search.go` | Register `Glob`/`Grep` via guest exec |
| `internal/acp/sysagent/tools/search_test.go` | Glob/Grep tests |
| `internal/acp/sysagent/tools/shell.go` | Only `Bash`; scrub descriptions; no ensure tool |
| `internal/acp/sysagent/tools/shell_test.go` | Rename tests; drop ensure tool test |
| `internal/acp/sysagent/tools/roundpen.go` | Rename List* / GetSettings; remove ensure tools; scrub env JSON |
| `internal/acp/sysagent/tools/registry_test.go` | Update Roundpen tests |
| `internal/acp/sysagent/tools/browser.go` | Scrub sandbox wording in descriptions |
| `internal/acp/sysagent/tools/cdp_error.go` | Model-safe infra errors (no ensure/sandbox) |
| `internal/acp/sysagent/tools/cdp_error_test.go` | Update expected strings |
| `internal/acp/sysagent/history.go` | System prompt rewrite |
| `internal/acp/sysagent/loop.go` | Nudges for new tool names / wording |
| `internal/acp/sysagent/loop_test.go` | Update tool name expectations |
| `internal/acp/sysagent/history_test.go` | Update if hard-coded old names matter |
| `internal/acp/manager/manager.go` | Wire `Files` on binder; `RegisterFiles` + `RegisterSearch` |
| `docs/architecture/acp-agent-ui.md` | Tool table sync |
| `web/src/lib/toolStats.ts` | Drop `roundpen_ensure_` special-case if unused; keep Bash/Read/… |

---

### Task 1: Workspace path helper

**Files:**
- Create: `internal/acp/sysagent/tools/path.go`
- Create: `internal/acp/sysagent/tools/path_test.go`

- [ ] **Step 1: Write failing tests**

```go
package tools_test

import (
	"testing"

	"github.com/RoundpenAI/roundpen/internal/acp/sysagent/tools"
)

func TestResolveWorkspacePath(t *testing.T) {
	cases := []struct {
		in, want string
		ok       bool
	}{
		{"/workspace/a.go", "a.go", true},
		{"/workspace", ".", true},
		{"a/b", "a/b", true},
		{"./x", "x", true},
		{"/etc/passwd", "", false},
		{"/workspace/../etc/passwd", "", false},
		{"../x", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, err := tools.ResolveWorkspacePath(tc.in)
		if tc.ok {
			if err != nil || got != tc.want {
				t.Fatalf("%q: got %q err=%v want %q", tc.in, got, err, tc.want)
			}
		} else if err == nil {
			t.Fatalf("%q: expected error, got %q", tc.in, got)
		}
	}
}
```

- [ ] **Step 2: Run test — expect FAIL (undefined)**

Run: `go test ./internal/acp/sysagent/tools/ -run TestResolveWorkspacePath -count=1`

- [ ] **Step 3: Implement**

```go
package tools

import (
	"fmt"
	"path"
	"strings"
)

const WorkspaceRoot = "/workspace"

// ResolveWorkspacePath maps an agent path to a workspace-relative path.
// Accepts absolute paths under /workspace or relative paths (relative to /workspace).
func ResolveWorkspacePath(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" {
		return "", fmt.Errorf("path is required")
	}
	var abs string
	if strings.HasPrefix(p, "/") {
		abs = path.Clean(p)
	} else {
		abs = path.Clean(path.Join(WorkspaceRoot, p))
	}
	if abs != WorkspaceRoot && !strings.HasPrefix(abs, WorkspaceRoot+"/") {
		return "", fmt.Errorf("path escapes workspace")
	}
	rel := strings.TrimPrefix(abs, WorkspaceRoot)
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return ".", nil
	}
	if rel == ".." || strings.HasPrefix(rel, "../") || strings.Contains(rel, "/../") {
		return "", fmt.Errorf("path escapes workspace")
	}
	return rel, nil
}
```

- [ ] **Step 4: Run test — expect PASS**

Run: `go test ./internal/acp/sysagent/tools/ -run TestResolveWorkspacePath -count=1`

- [ ] **Step 5: Commit**

```bash
git add internal/acp/sysagent/tools/path.go internal/acp/sysagent/tools/path_test.go
git commit -m "$(cat <<'EOF'
feat(sysagent): add /workspace path resolver for builtin tools

EOF
)"
```

---

### Task 2: `Bash` replaces `sandbox_exec`; drop ensure tool from shell

**Files:**
- Modify: `internal/acp/sysagent/tools/shell.go`
- Modify: `internal/acp/sysagent/tools/shell_test.go`

- [ ] **Step 1: Rewrite failing tests for `Bash`**

Replace `TestSandboxExecUsesAgentSlot`, `TestSandboxExecRequiresCommand`, `TestSandboxExecNonZeroExit` to call tool name `"Bash"`. Delete `TestEnsureAgentTool`. Add:

```go
func TestBashDescriptionHasNoSandbox(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterShell(reg, &tools.AgentBinder{
		Slots: &stubAgentSlots{id: "sb"},
		Exec:  &stubExec{ws: t.TempDir()},
	})
	for _, tool := range reg.List() {
		if strings.Contains(strings.ToLower(tool.Name), "sandbox") {
			t.Fatalf("name %q", tool.Name)
		}
		if strings.Contains(strings.ToLower(tool.Description), "sandbox") {
			t.Fatalf("desc %q", tool.Description)
		}
		if tool.Name == "roundpen_ensure_agent" {
			t.Fatal("ensure tool must not be registered")
		}
	}
}
```

- [ ] **Step 2: Run — expect FAIL on old `sandbox_exec` wiring**

Run: `go test ./internal/acp/sysagent/tools/ -run 'TestBash|TestSandbox' -count=1`

- [ ] **Step 3: Update `RegisterShell`**

In `shell.go`:
- Remove the `roundpen_ensure_agent` registration block entirely.
- Rename tool to `Bash`.
- Description (model-facing):

```text
Run a shell command in the Agent workspace (default cwd /workspace).
Use for git, builds, tests, and other command-line work.
The Browser cannot run shell commands.
```

- Keep `Mutating: true`, same parameters (`command`, `workdir`, `timeout`), same `ensure` + `exec` call path.
- Do **not** put `sandboxId` in any return JSON (Bash already returns only exit/stdout/stderr).

Also scrub error strings in `ensure`/`exec` helpers that say `"sandbox exec not configured"` → `"workspace exec not configured"` and `"agent environment has no sandbox"` → `"agent workspace is not available"`.

- [ ] **Step 4: Run tests — expect PASS**

Run: `go test ./internal/acp/sysagent/tools/ -run 'TestBash|TestSandbox' -count=1`

- [ ] **Step 5: Commit**

```bash
git add internal/acp/sysagent/tools/shell.go internal/acp/sysagent/tools/shell_test.go
git commit -m "$(cat <<'EOF'
feat(sysagent): rename sandbox_exec to Bash and drop ensure tool

EOF
)"
```

---

### Task 3: Extend binder with file I/O + `Read`/`Write`/`Edit`

**Files:**
- Modify: `internal/acp/sysagent/tools/shell.go` (add `WorkspaceFiles` on `AgentBinder`)
- Create: `internal/acp/sysagent/tools/files.go`
- Create: `internal/acp/sysagent/tools/files_test.go`
- Modify: `internal/acp/manager/manager.go` (pass `Files: m.sandboxes`)

- [ ] **Step 1: Add interface + stub in tests**

In `shell.go` (or keep interface next to binder):

```go
type WorkspaceFiles interface {
	ReadFile(ctx context.Context, id, relPath string) (io.ReadCloser, error)
	WriteFile(ctx context.Context, id, relPath string, r io.Reader) error
}

type AgentBinder struct {
	Slots AgentSlot
	Exec  SandboxExec
	Files WorkspaceFiles
}
```

Add `ensureID` helper used by file/search tools:

```go
func (b *AgentBinder) ensureID(ctx context.Context, actor Actor) (string, error) {
	sb, err := b.ensure(ctx, actor)
	if err != nil {
		return "", err
	}
	return sb.ID, nil
}
```

(Keep `ensure` private; file tools call `ensureID`.)

- [ ] **Step 2: Write failing file tool tests**

```go
package tools_test

// stubFiles map[string][]byte with ReadFile/WriteFile
// RegisterFiles(reg, binder)
// TestReadWriteEditHappyPath
// TestEditZeroMatches / TestEditMultipleMatches
// TestReadPathEscape
```

Concrete happy-path sketch:

```go
func TestReadWriteEdit(t *testing.T) {
	fs := &memFiles{data: map[string][]byte{}}
	slots := &stubAgentSlots{id: "sb1"}
	reg := tools.NewRegistry()
	tools.RegisterFiles(reg, &tools.AgentBinder{Slots: slots, Files: fs})

	actor := tools.Actor{Username: "alice"}
	_, err := reg.Call(context.Background(), actor, "Write", json.RawMessage(
		`{"file_path":"/workspace/a.txt","content":"hello world"}`))
	if err != nil {
		t.Fatal(err)
	}
	out, err := reg.Call(context.Background(), actor, "Read", json.RawMessage(
		`{"file_path":"a.txt"}`))
	if err != nil || !strings.Contains(out, "hello world") {
		t.Fatalf("read=%q err=%v", out, err)
	}
	_, err = reg.Call(context.Background(), actor, "Edit", json.RawMessage(
		`{"file_path":"/workspace/a.txt","old_string":"world","new_string":"there"}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(fs.data["a.txt"]) != "hello there" {
		t.Fatalf("got %q", fs.data["a.txt"])
	}
}
```

- [ ] **Step 3: Run — expect FAIL**

Run: `go test ./internal/acp/sysagent/tools/ -run 'TestReadWriteEdit|TestEdit' -count=1`

- [ ] **Step 4: Implement `RegisterFiles`**

`files.go` behaviors:
- `Read`: resolve path → `Files.ReadFile` → if empty, return notice; else return line-numbered text (`fmt.Sprintf("%6d|%s", n, line)`). Support `offset` (1-based start line) and `limit`. Cap total output ~100KiB.
- `Write`: resolve → `WriteFile` with content reader. Mutating.
- `Edit`: read all → `strings.Count(old)`; 0 → error; >1 && !replace_all → error; else replace → write. Mutating.
- All three call `ensureID` first.
- Descriptions must not contain `sandbox`.

Wire manager:

```go
tools.RegisterShell(reg, &tools.AgentBinder{
	Slots: m.sys.AgentSlots,
	Exec:  m.sandboxes,
	Files: m.sandboxes,
})
tools.RegisterFiles(reg, /* same binder pointer or reconstruct with Files */)
```

Prefer one binder value shared by RegisterShell/RegisterFiles/RegisterSearch.

- [ ] **Step 5: Run tests — PASS**

Run: `go test ./internal/acp/sysagent/tools/ -count=1`

- [ ] **Step 6: Commit**

```bash
git add internal/acp/sysagent/tools/shell.go internal/acp/sysagent/tools/files.go \
  internal/acp/sysagent/tools/files_test.go internal/acp/manager/manager.go
git commit -m "$(cat <<'EOF'
feat(sysagent): add Read, Write, and Edit workspace tools

EOF
)"
```

---

### Task 4: `Glob` and `Grep`

**Files:**
- Create: `internal/acp/sysagent/tools/search.go`
- Create: `internal/acp/sysagent/tools/search_test.go`
- Modify: `internal/acp/manager/manager.go` (`RegisterSearch`)

- [ ] **Step 1: Failing tests with `stubExec` capturing commands**

```go
func TestGlobRunsFind(t *testing.T) {
	slots := &stubAgentSlots{id: "sb"}
	ex := &stubExec{ws: t.TempDir(), res: &sandbox.ExecResult{
		ExitCode: 0, Stdout: []byte("/workspace/src/a.ts\n/workspace/src/b.ts\n"),
	}}
	reg := tools.NewRegistry()
	tools.RegisterSearch(reg, &tools.AgentBinder{Slots: slots, Exec: ex})
	out, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "Glob",
		json.RawMessage(`{"pattern":"**/*.ts"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "a.ts") {
		t.Fatalf("out=%s", out)
	}
	if slots.calls != 1 {
		t.Fatalf("ensure calls=%d", slots.calls)
	}
}

func TestGrepRequiresPattern(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterSearch(reg, &tools.AgentBinder{
		Slots: &stubAgentSlots{id: "sb"},
		Exec:  &stubExec{ws: t.TempDir()},
	})
	if _, err := reg.Call(context.Background(), tools.Actor{Username: "u"}, "Grep",
		json.RawMessage(`{}`)); err == nil {
		t.Fatal("expected error")
	}
}
```

- [ ] **Step 2: Implement**

`Glob`:
- Resolve optional `path` (default `/workspace`) to abs guest path for exec cwd/root.
- Run via `Exec`: `/bin/sh -c` script that prefers `rg --files -g PATTERN -- PATH` and falls back to `find PATH -type f`.
- Return newline-separated paths (truncate ~32KiB like shell).

`Grep`:
- Required `pattern`; optional `path` (default `/workspace`), `glob`, `case_insensitive`, `output_mode` (`content` default | `files_with_matches` | `count`).
- Build `rg` argv; if exit 2 (rg missing / error), return model-safe `"rg is not available in the Agent workspace"`.
- Exit 1 (no matches) → empty result string / `"No matches found"`, not a hard tool error.

Descriptions: no `sandbox` / `guest`.

- [ ] **Step 3: Register in manager next to shell/files**

- [ ] **Step 4: Tests PASS + commit**

```bash
git add internal/acp/sysagent/tools/search.go internal/acp/sysagent/tools/search_test.go \
  internal/acp/manager/manager.go
git commit -m "$(cat <<'EOF'
feat(sysagent): add Glob and Grep workspace tools

EOF
)"
```

---

### Task 5: Platform tools rename + scrub

**Files:**
- Modify: `internal/acp/sysagent/tools/roundpen.go`
- Modify: `internal/acp/sysagent/tools/registry_test.go`

- [ ] **Step 1: Update `TestRoundpenHTTP_List`**

```go
out, err := reg.Call(..., "ListEnvironments", nil)
// must contain environments
// must NOT contain roundpen_ensure_agent, sandboxId, or "sandbox"
// ensure tools must be unknown:
if _, err := reg.Call(..., "roundpen_ensure_agent", nil); err == nil {
	t.Fatal("ensure must be gone")
}
```

httptest fixture: return JSON that includes `sandboxId` so the test proves scrubbing:

```json
{"environments":[{"slot":"agent","status":"absent","sandboxId":"sb-secret"}]}
```

Expect tool output has no `sb-secret` / `sandboxId`.

- [ ] **Step 2: Implement renames in `RegisterRoundpen`**

| New name | HTTP |
|----------|------|
| `ListEnvironments` | GET `/v1/me/environments` then `scrubEnvList` |
| `ListTemplates` | GET `/v1/templates` |
| `ListSessions` | GET `/v1/agent-sessions` |
| `GetSettings` | GET `/v1/admin/settings` (admin) |

Remove `roundpen_ensure_agent` and `roundpen_ensure_browser` registrations.

`scrubEnvList`:
- Unmarshal JSON
- For each environment object, delete keys `sandboxId`, `SandboxID`, `id` if it looks like sandbox id (prefer deleting only `sandboxId`)
- Set `note` to: `agent status=absent means not started yet — call Bash or file tools (Read/Write/Edit). Browser cannot run git or shell.`
- Remarshal

Descriptions without sandbox/ensure wording.

- [ ] **Step 3: Tests PASS + commit**

```bash
git add internal/acp/sysagent/tools/roundpen.go internal/acp/sysagent/tools/registry_test.go
git commit -m "$(cat <<'EOF'
feat(sysagent): rename platform tools and scrub environment JSON

EOF
)"
```

---

### Task 6: Browser / CDP / loop / prompt language scrub

**Files:**
- Modify: `internal/acp/sysagent/tools/cdp_error.go`
- Modify: `internal/acp/sysagent/tools/cdp_error_test.go`
- Modify: `internal/acp/sysagent/tools/browser.go` (descriptions only if they mention sandbox)
- Modify: `internal/acp/sysagent/history.go`
- Modify: `internal/acp/sysagent/loop.go`
- Modify: `internal/acp/sysagent/loop_test.go`

- [ ] **Step 1: Update CDP message**

```go
Msg: "Browser Chrome DevTools is not reachable. " +
	"This is an environment problem, not a page problem. " +
	"Do not keep retrying page tools; wait briefly and try browser_* once more, " +
	"or report that the Browser is not ready. (" + shortInfra(msg) + ")",
```

- [ ] **Step 2: Rewrite system prompt in `history.go`**

```text
You are Roundpen System Agent. You help the signed-in user manage Roundpen resources they are allowed to access.
Use tools for factual actions. Do not invent API results. Prefer concise answers.

Workspace tools (working directory /workspace): Read, Write, Edit, Glob, Grep, Bash.
Browser tools (browser_*): Chrome only — cannot run git or shell.

If ListEnvironments shows agent status=absent, the Agent workspace is simply not started yet.
Call Bash or a file tool; the environment starts as needed. Do not stop after listing.
A running Browser is not a substitute for the Agent workspace.
When the user asks to clone a repo, run commands, or work in files, use Bash and the file tools.
Git auth: if the user saved a personal token (Settings → Git), git inside Bash uses it automatically.
Do not put tokens in clone URLs, and do not inspect credential files.
Prior user messages, your replies, and tool calls/results are included when this session has history.
```

- [ ] **Step 3: Update loop constants + `agentAbsentOnly`**

```go
infraNudgeText = "Browser Chrome is not ready. Do not keep clicking page tools. Wait, then retry browser_* once. If it still fails, report that the Browser environment is not available."
infraStopText  = "Stopped: Browser DevTools is not reachable. Retrying page tools will not help."
agentAbsentNudgeText = "Agent workspace is not started (status=absent). That is not a missing capability. Call Bash or a file tool now. Do not stop after listing, and do not use the Browser for git or shell."
```

```go
func agentAbsentOnly(calls []toolCallRec) bool {
	if len(calls) != 1 || calls[0].name != "ListEnvironments" {
		return false
	}
	...
}
```

Update `loop_test.go` fixtures that use `roundpen_list_environments`.

- [ ] **Step 4: Run**

`go test ./internal/acp/sysagent/... -count=1`

- [ ] **Step 5: Commit**

```bash
git add internal/acp/sysagent/tools/cdp_error.go internal/acp/sysagent/tools/cdp_error_test.go \
  internal/acp/sysagent/tools/browser.go internal/acp/sysagent/history.go \
  internal/acp/sysagent/loop.go internal/acp/sysagent/loop_test.go
git commit -m "$(cat <<'EOF'
fix(sysagent): scrub sandbox language from prompt, nudges, and CDP errors

EOF
)"
```

---

### Task 7: Docs + UI toolStats + registry surface assertion

**Files:**
- Modify: `docs/architecture/acp-agent-ui.md`
- Modify: `web/src/lib/toolStats.ts` (optional cleanup of `roundpen_ensure_` branch)
- Create or extend: `internal/acp/sysagent/tools/registry_surface_test.go`

- [ ] **Step 1: Update architecture doc tool table** to match the spec catalog (Read/Write/Edit/Glob/Grep/Bash, ListEnvironments, …; note implicit ensure).

- [ ] **Step 2: Add surface test**

```go
func TestWorkspaceToolSurface(t *testing.T) {
	reg := tools.NewRegistry()
	tools.RegisterRoundpen(reg, &tools.RoundpenHTTP{BaseURL: "http://127.0.0.1"})
	tools.RegisterShell(reg, &tools.AgentBinder{Slots: &stubAgentSlots{id: "x"}, Exec: &stubExec{}})
	tools.RegisterFiles(reg, &tools.AgentBinder{Slots: &stubAgentSlots{id: "x"}, Files: &memFiles{data: map[string][]byte{}}})
	tools.RegisterSearch(reg, &tools.AgentBinder{Slots: &stubAgentSlots{id: "x"}, Exec: &stubExec{}})

	want := []string{"Bash", "Edit", "GetSettings", "Glob", "Grep", "ListEnvironments", "ListSessions", "ListTemplates", "Read", "Write"}
	// assert every want present; assert no name contains "sandbox" or "ensure" or "roundpen_"
	for _, tool := range reg.List() {
		d := strings.ToLower(tool.Description)
		if strings.Contains(d, "sandbox") {
			t.Fatalf("%s description mentions sandbox", tool.Name)
		}
	}
}
```

- [ ] **Step 3: `npm test` / vitest for toolStats if changed**

Run: `cd web && npx vitest run src/lib/toolStats.test.ts`

- [ ] **Step 4: Full Go package test**

Run: `go test ./internal/acp/... -count=1`

- [ ] **Step 5: Commit**

```bash
git add docs/architecture/acp-agent-ui.md web/src/lib/toolStats.ts \
  internal/acp/sysagent/tools/registry_surface_test.go
git commit -m "$(cat <<'EOF'
docs(sysagent): document Claude Code–aligned builtin tools surface

EOF
)"
```

---

## Spec coverage checklist

| Spec item | Task |
|-----------|------|
| Read/Write/Edit/Glob/Grep/Bash | 2–4 |
| Implicit ensure | 2–4 (ensure before each tool) |
| `/workspace` paths + escape rejection | 1, 3 |
| Remove ensure tools | 2, 5 |
| Rename platform tools + scrub `sandboxId` | 5 |
| Keep `browser_*`, scrub language | 6 |
| System prompt + loop nudges | 6 |
| Mutating flags unchanged | 2–4 (Write/Edit/Bash mutating) |
| Docs + toolStats | 7 |
| No LLM aliases for old names | 2, 5, 7 |
| Non-goals (ACP coding agent, NotebookEdit, …) | not planned |

## Placeholder / consistency notes

- Tool names are PascalCase exactly as Claude Code: `Read`, `Write`, `Edit`, `Glob`, `Grep`, `Bash`, `ListEnvironments`, …
- Shared `AgentBinder` fields: `Slots`, `Exec`, `Files`
- Register order in manager: Roundpen → Browser → Shell → Files → Search (order does not matter for OpenAI list sorting by name)
