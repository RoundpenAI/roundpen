/** Minimal tool shape (matches chat tool cards / toolStats). */
export type ToolCallLike = {
  title: string
  status: string
  kind?: string
  input?: unknown
  output?: unknown
}

/** One file touched by Edit/Write (or a unified-diff patch) in a tool wave. */
export type FileChange = {
  path: string
  /** Unified diff text for display (may be truncated in the UI). */
  unified: string
  plus: number
  minus: number
  /** write = full file contents; edit = string replace; patch = unified input */
  kind: 'edit' | 'write' | 'patch'
}

export type ChangeSet = {
  files: FileChange[]
  plus: number
  minus: number
}

const FOLD_LINE_THRESHOLD = 80

export function shouldFoldDiff(unified: string): boolean {
  return unified.split(/\r?\n/).length > FOLD_LINE_THRESHOLD
}

/** True when a tool call is represented by a ChangeSet card (Edit/Write/…). */
export function isChangeSetTool(call: ToolCallLike): boolean {
  return isEditLike(call)
}

/** Build a ChangeSet from completed Edit/Write tool calls in an activity group. */
export function buildChangeSet(calls: ToolCallLike[]): ChangeSet | null {
  const byPath = new Map<string, FileChange>()
  for (const call of calls) {
    if (call.status === 'failed' || call.status === 'pending' || call.status === 'in_progress') {
      continue
    }
    if (!isChangeSetTool(call)) continue
    const change = fileChangeFromCall(call)
    if (!change) continue
    const prev = byPath.get(change.path)
    if (!prev) {
      byPath.set(change.path, change)
      continue
    }
    // Later edits win for the same path; keep cumulative +/-.
    byPath.set(change.path, {
      ...change,
      plus: prev.plus + change.plus,
      minus: prev.minus + change.minus,
      unified: change.unified || prev.unified,
    })
  }
  const files = [...byPath.values()].sort((a, b) => a.path.localeCompare(b.path))
  if (files.length === 0) return null
  let plus = 0
  let minus = 0
  for (const f of files) {
    plus += f.plus
    minus += f.minus
  }
  return { files, plus, minus }
}

function fileChangeFromCall(call: ToolCallLike): FileChange | null {
  const rec = asRecord(call.input)
  const path =
    pickString(rec, ['path', 'file', 'file_path', 'filePath', 'target_file', 'targetFile']) ||
    pathFromTitle(call.title)
  if (!path) return null

  const oldStr = pickString(rec, ['old_string', 'oldString', 'old_str', 'oldText'])
  const newStr = pickString(rec, ['new_string', 'newString', 'new_str', 'newText'])
  if (oldStr != null && newStr != null) {
    const unified = toUnifiedDiff(path, oldStr, newStr)
    const { plus, minus } = countUnified(unified)
    return { path, unified, plus, minus, kind: 'edit' }
  }

  const contents = pickString(rec, ['contents', 'content', 'text', 'data'])
  if (contents != null && isWriteTool(call)) {
    const unified = toUnifiedDiff(path, '', contents)
    const { plus, minus } = countUnified(unified)
    return { path, unified, plus, minus, kind: 'write' }
  }

  const patch = pickString(rec, ['patch', 'diff', 'unified_diff'])
  if (patch) {
    const { plus, minus } = countUnified(patch)
    return { path, unified: patch, plus, minus, kind: 'patch' }
  }

  // Bash git diff in output — best-effort.
  const out = outputText(call.output)
  if (out && (out.includes('\n+++ ') || out.includes('\n@@ '))) {
    const { plus, minus } = countUnified(out)
    return { path, unified: out, plus, minus, kind: 'patch' }
  }
  return null
}

function isWriteTool(call: ToolCallLike): boolean {
  const name = toolName(call)
  return name === 'write' || name === 'create' || name === 'create_file'
}

function isEditLike(call: ToolCallLike): boolean {
  const kind = (call.kind || '').trim().toLowerCase()
  if (kind === 'edit' || kind === 'delete' || kind === 'move') return true
  return /^(edit|write|strreplace|str_replace|search_replace|searchreplace|apply_patch|applypatch|notebookedit|notebook_edit|delete|unlink|rm|move|mv|rename|create|create_file)$/.test(
    toolName(call),
  )
}

function toolName(call: ToolCallLike): string {
  const raw = (call.title || '').trim()
  const first = raw.split(/[\s:·]+/, 1)[0] || ''
  return first.toLowerCase().replace(/[^a-z0-9_]/g, '')
}

function pathFromTitle(title: string | undefined): string | null {
  if (!title) return null
  // e.g. "Edit · path/to/file.ts"
  const m = title.match(/[·:]\s*(.+)$/)
  const cand = (m?.[1] || '').trim()
  if (!cand || cand.includes('\n')) return null
  if (!/[\\/]/.test(cand) && !/\.[a-zA-Z0-9]{1,8}$/.test(cand)) return null
  return cand.replace(/\\/g, '/')
}

/** Minimal unified diff from old→new (whole-file hunk). */
export function toUnifiedDiff(path: string, oldText: string, newText: string): string {
  const oldLines = oldText === '' ? [] : oldText.split('\n')
  const newLines = newText === '' ? [] : newText.split('\n')
  // Drop trailing empty from split of empty string edge cases already handled.
  const lines: string[] = [
    `--- a/${path}`,
    `+++ b/${path}`,
    `@@ -1,${Math.max(oldLines.length, 1)} +1,${Math.max(newLines.length, 1)} @@`,
  ]
  for (const line of oldLines) lines.push(`-${line}`)
  for (const line of newLines) lines.push(`+${line}`)
  return lines.join('\n')
}

export function countUnified(text: string): { plus: number; minus: number } {
  let plus = 0
  let minus = 0
  for (const line of text.split(/\r?\n/)) {
    if (line.startsWith('+++') || line.startsWith('---') || line.startsWith('@@')) continue
    if (line.startsWith('+')) plus += 1
    else if (line.startsWith('-')) minus += 1
  }
  return { plus, minus }
}

/**
 * Normalize an agent/tool path to a workspace-relative path suitable for
 * /v1/me/workspace/files and /workspace?path=… deep links.
 */
export function workspaceRelPath(path: string): string {
  let p = path.trim().replace(/\\/g, '/')
  while (p.startsWith('./')) p = p.slice(2)
  if (p.startsWith('/workspace/')) p = p.slice('/workspace/'.length)
  else if (p.startsWith('workspace/')) p = p.slice('workspace/'.length)
  else if (p.startsWith('/')) p = p.slice(1)
  return p
}

/** Workspace URL that opens the Files page and previews this path. */
export function workspacePreviewHref(path: string): string {
  const clean = workspaceRelPath(path)
  const parts = clean.split('/').filter(Boolean)
  const name = parts.pop() || clean
  const dir = parts.length ? parts.join('/') : '.'
  const q = new URLSearchParams()
  q.set('path', dir)
  q.set('preview', name)
  return `/workspace?${q.toString()}`
}

function asRecord(value: unknown): Record<string, unknown> {
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    return value as Record<string, unknown>
  }
  return {}
}

function pickString(rec: Record<string, unknown>, keys: string[]): string | null {
  for (const key of keys) {
    if (typeof rec[key] === 'string') return rec[key]
  }
  return null
}

function outputText(output: unknown): string {
  if (output == null) return ''
  if (typeof output === 'string') return output
  if (typeof output === 'object') {
    const rec = output as Record<string, unknown>
    for (const key of ['content', 'text', 'output', 'result', 'stdout']) {
      if (typeof rec[key] === 'string') return rec[key]
    }
  }
  return ''
}
