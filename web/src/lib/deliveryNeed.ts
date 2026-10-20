import type { AgentMessage } from '../api'

/** Which delivery actions the composer bar should offer. */
export type DeliveryNeed = {
  /** Uncommitted edits in a git-backed project — commit / push / open PR. */
  openPR: boolean
  /** An open PR from this session that has not been merged yet. */
  mergePR: boolean
}

/**
 * Infer delivery actions from session tool history.
 * Show the bar when a git-backed project has uncommitted edits, and/or when
 * CreatePullRequest succeeded and MergePullRequest has not yet.
 */
export function inferDeliveryNeed(messages: AgentMessage[]): DeliveryNeed {
  const dirtyRepos = new Set<string>()
  const openPRs = new Set<string>()

  for (const m of messages) {
    if (!isToolMessage(m)) continue
    const status = (m.meta?.status || '').toLowerCase()
    if (status === 'pending' || status === 'in_progress') continue
    const ok = status !== 'failed' && status !== 'error' && status !== 'cancelled'
    const name = toolName(m)
    const repo = repoFromMessage(m)

    if (isEditLike(m)) {
      if (!ok) continue
      const path = pathFromMessage(m)
      const r = repo || (path ? repoFromPath(path) : null)
      if (r) dirtyRepos.add(r)
      continue
    }

    if (name === 'gitcommit') {
      if (ok && repo) dirtyRepos.delete(repo)
      continue
    }
    if (name === 'createpullrequest') {
      if (ok && repo) {
        dirtyRepos.delete(repo)
        openPRs.add(repo)
      }
      continue
    }
    if (name === 'mergepullrequest') {
      if (ok && repo) openPRs.delete(repo)
      continue
    }
  }

  return {
    openPR: dirtyRepos.size > 0,
    mergePR: openPRs.size > 0,
  }
}

export function hasDeliveryNeed(need: DeliveryNeed): boolean {
  return need.openPR || need.mergePR
}

function isToolMessage(m: AgentMessage): boolean {
  const typ = m.meta?.type || m.role
  return m.role === 'tool' || typ === 'tool_call' || Boolean(m.meta?.toolId && typ !== 'permission')
}

function toolName(m: AgentMessage): string {
  const raw = (m.meta?.title || m.meta?.toolId || '').trim()
  const first = raw.split(/[\s:·]+/, 1)[0] || ''
  return first.toLowerCase().replace(/[^a-z0-9_]/g, '')
}

function isEditLike(m: AgentMessage): boolean {
  const kind = (m.meta?.kind || '').trim().toLowerCase()
  if (kind === 'edit' || kind === 'delete' || kind === 'move') return true
  return /^(edit|write|strreplace|str_replace|search_replace|searchreplace|apply_patch|applypatch|notebookedit|notebook_edit|delete|unlink|rm|move|mv|rename|create|create_file)$/.test(
    toolName(m),
  )
}

function asRecord(v: unknown): Record<string, unknown> | null {
  if (!v || typeof v !== 'object' || Array.isArray(v)) return null
  return v as Record<string, unknown>
}

function pickString(rec: Record<string, unknown> | null, keys: string[]): string | null {
  if (!rec) return null
  for (const k of keys) {
    const v = rec[k]
    if (typeof v === 'string' && v.trim()) return v.trim()
  }
  return null
}

function pathFromMessage(m: AgentMessage): string | null {
  const rec = asRecord(m.meta?.input)
  const fromInput = pickString(rec, [
    'path',
    'file',
    'file_path',
    'filePath',
    'target_file',
    'targetFile',
  ])
  if (fromInput) return normalizePath(fromInput)
  const title = m.meta?.title || ''
  const match = title.match(/[·:]\s*(.+)$/)
  const cand = (match?.[1] || '').trim()
  if (!cand || cand.includes('\n')) return null
  if (!/[\\/]/.test(cand) && !/\.[a-zA-Z0-9]{1,8}$/.test(cand)) return null
  return normalizePath(cand)
}

function repoFromMessage(m: AgentMessage): string | null {
  const rec = asRecord(m.meta?.input)
  const raw = pickString(rec, ['repo_path', 'repoPath', 'cwd', 'work_dir', 'workdir'])
  if (raw) return repoFromPath(raw) || normalizePath(raw)
  // Title may be "GitCommit · projects/foo"
  const title = m.meta?.title || ''
  const match = title.match(/[·:]\s*(.+)$/)
  if (match?.[1]) return repoFromPath(match[1].trim())
  return null
}

/** Map a workspace path to its project repo root (projects/<slug>), if any. */
export function repoFromPath(path: string): string | null {
  const p = normalizePath(path)
  if (!p) return null
  // Convention: git repos live under projects/<name>/
  const m = p.match(/^(?:workspace\/)?projects\/([^/]+)/)
  if (m) return `projects/${m[1]}`
  return null
}

function normalizePath(path: string): string {
  let p = path.trim().replace(/\\/g, '/')
  while (p.startsWith('./')) p = p.slice(2)
  if (p.startsWith('/workspace/')) p = p.slice('/workspace/'.length)
  else if (p.startsWith('workspace/')) p = p.slice('workspace/'.length)
  else if (p.startsWith('/')) p = p.slice(1)
  return p
}
