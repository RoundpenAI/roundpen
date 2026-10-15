import type { AgentCommand, AgentMessage } from '../../api'

export type PermReq = {
  requestId: string
  title: string
  options: { optionId: string; name: string; kind?: string }[]
  ticketId?: string
}

export const ROLE_CONFIG = {
  user: { name: '' },
  assistant: { name: '' },
  system: { name: '' },
}

export function readAutoMode(): boolean {
  try {
    const v = localStorage.getItem('roundpen.chat.auto')
    return v !== '0'
  } catch {
    return true
  }
}

export function isBrowserTool(title: string): boolean {
  return title.startsWith('browser_')
}

export function nowIso(): string {
  return new Date().toISOString()
}

export function isStreamingMessage(m: AgentMessage): boolean {
  return m.meta?.status === 'in_progress' || m.meta?.status === 'pending'
}

function isThoughtMessage(m: AgentMessage): boolean {
  const typ = m.meta?.type || m.role
  return typ === 'thought' || typ === 'reasoning' || m.role === 'thought'
}

/** Elapsed ms from the thought's createdAt to now (or a provided end). */
export function thoughtDurationMs(m: AgentMessage, endMs = Date.now()): number {
  const start = Date.parse(m.createdAt)
  if (!Number.isFinite(start)) return 0
  return Math.max(0, endMs - start)
}

/**
 * Close any open thought blocks and stamp durationMs. Called at boundaries
 * (reply text / tool / turn end) so the live timer stops when thinking ends.
 */
export function finalizeOpenThoughts(
  prev: AgentMessage[],
  endMs = Date.now(),
): AgentMessage[] {
  let changed = false
  const next = prev.map((m) => {
    if (!isThoughtMessage(m) || !isStreamingMessage(m)) return m
    changed = true
    return {
      ...m,
      meta: {
        ...m.meta,
        type: 'thought',
        status: 'completed',
        durationMs: m.meta?.durationMs ?? thoughtDurationMs(m, endMs),
      },
    }
  })
  return changed ? next : prev
}

export function clearStreaming(prev: AgentMessage[]): AgentMessage[] {
  return finalizeOpenThoughts(
    prev.map((m) =>
      isStreamingMessage(m) && !isThoughtMessage(m)
        ? { ...m, meta: { ...m.meta, status: 'completed' } }
        : m,
    ),
  )
}

/**
 * After a refresh/reconnect, history may arrive after the WS status snapshot
 * already planted an in-progress reply/thought. Use persisted history as the
 * base and keep ephemeral live overlays (stream-/thought-/tool- rows that are
 * not yet in the DB) on top so we never drop either side.
 */
export function mergeHistoryWithLive(
  hist: AgentMessage[],
  live: AgentMessage[],
  sessionId: string,
): AgentMessage[] {
  const histIds = new Set(hist.map((m) => m.id))
  const histToolIds = new Set(
    hist
      .map((m) => m.meta?.toolId)
      .filter((id): id is string => typeof id === 'string' && id.length > 0),
  )
  const overlays: AgentMessage[] = []
  for (const m of live) {
    if (m.sessionId && m.sessionId !== sessionId) continue
    if (!isStreamingMessage(m)) continue
    if (histIds.has(m.id)) continue
    const toolId = m.meta?.toolId
    if (typeof toolId === 'string' && toolId && histToolIds.has(toolId)) {
      continue
    }
    overlays.push(m)
  }
  if (overlays.length === 0) return hist
  return [...hist, ...overlays]
}

/**
 * Reconcile a recent-window refetch with already-loaded older pages so a
 * turn-end refresh does not wipe scroll-loaded history.
 */
export function reconcileRefetch(
  prev: AgentMessage[],
  fresh: AgentMessage[],
): AgentMessage[] {
  if (fresh.length === 0) return prev
  const freshIds = new Set(fresh.map((m) => m.id))
  const oldestFresh = fresh[0]!
  const older = prev.filter((m) => {
    if (freshIds.has(m.id)) return false
    if (isStreamingMessage(m)) return false
    if (m.createdAt < oldestFresh.createdAt) return true
    if (m.createdAt === oldestFresh.createdAt && m.id < oldestFresh.id) {
      return true
    }
    return false
  })
  const live = prev.filter((m) => isStreamingMessage(m) && !freshIds.has(m.id))
  return [...older, ...fresh, ...live]
}

/** Default page size for chat transcript fetches. */
export const MESSAGE_PAGE_SIZE = 80

/** True when the row is a client-only ephemeral bubble (not a DB uuid). */
export function isEphemeralMessageId(id: string): boolean {
  return /^(stream|thought|tool|perm)-/.test(id)
}

export function upsertToolMessage(
  prev: AgentMessage[],
  sessionId: string,
  patch: {
    toolId: string
    title?: string
    status?: string
    kind?: string
    input?: unknown
    output?: unknown
  },
): AgentMessage[] {
  const idx = prev.findIndex((m) => m.meta?.toolId === patch.toolId)
  if (idx >= 0) {
    const cur = prev[idx]
    const next = [...prev]
    next[idx] = {
      ...cur,
      content:
        typeof patch.output === 'string'
          ? patch.output
          : patch.output !== undefined
            ? formatUnknown(patch.output)
            : cur.content,
      meta: {
        ...cur.meta,
        type: 'tool_call',
        toolId: patch.toolId,
        title: patch.title || cur.meta?.title,
        status: patch.status || cur.meta?.status,
        kind: patch.kind || cur.meta?.kind,
        input: patch.input !== undefined ? patch.input : cur.meta?.input,
        output: patch.output !== undefined ? patch.output : cur.meta?.output,
      },
    }
    return next
  }
  return [
    ...prev,
    {
      id: `tool-${patch.toolId}`,
      sessionId,
      role: 'tool',
      content:
        typeof patch.output === 'string'
          ? patch.output
          : patch.output !== undefined
            ? formatUnknown(patch.output)
            : '',
      meta: {
        type: 'tool_call',
        toolId: patch.toolId,
        title: patch.title || patch.toolId,
        status: patch.status || 'pending',
        kind: patch.kind,
        input: patch.input,
        output: patch.output,
      },
      createdAt: nowIso(),
    },
  ]
}

export function formatUnknown(value: unknown): string {
  if (value == null) return ''
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

export function appendThoughtMessage(
  prev: AgentMessage[],
  sessionId: string,
  text: string,
): AgentMessage[] {
  for (let i = prev.length - 1; i >= 0; i--) {
    const m = prev[i]
    const typ = m.meta?.type || m.role
    if (
      m.role === 'user' ||
      typ === 'agent_message' ||
      (m.role === 'assistant' && typ !== 'thought' && typ !== 'reasoning')
    ) {
      break
    }
    if (isThoughtMessage(m)) {
      // A completed thought is a finished segment; start a new block so the
      // next round of thinking gets its own timer.
      if (m.meta?.status === 'completed') break
      const next = [...prev]
      next[i] = {
        ...m,
        content: m.content + text,
        meta: { ...m.meta, type: 'thought', status: 'in_progress' },
      }
      return next
    }
  }
  return [
    ...prev,
    {
      id: `thought-${Date.now()}`,
      sessionId,
      role: 'assistant',
      content: text,
      meta: { type: 'thought', status: 'in_progress' },
      createdAt: nowIso(),
    },
  ]
}

export function shouldShowInDialogue(m: AgentMessage): boolean {
  const typ = m.meta?.type || m.role
  if (typ === 'permission' || m.role === 'permission') {
    const outcome = m.meta?.outcome
    if (outcome === 'requested' || outcome === 'auto') return false
  }
  return true
}

/** Semi skill item for the "/" menu; extra fields ride along for renderSkillItem. */
export function toSkillItem(c: AgentCommand) {
  return {
    value: c.name,
    label: c.name,
    description: c.description,
    kind: c.kind,
    source: c.source,
  }
}

export const sentPending = new Set<string>()

export function pendingKey(sessionId: string) {
  return `roundpen.pendingPrompt.${sessionId}`
}

export function stashPendingPrompt(sessionId: string, text: string) {
  if (!sessionId || !text) return
  try {
    sessionStorage.setItem(pendingKey(sessionId), text)
  } catch {
    /* ignore */
  }
}

export function readPendingPrompt(sessionId: string): string {
  try {
    return sessionStorage.getItem(pendingKey(sessionId)) ?? ''
  } catch {
    return ''
  }
}

export function clearPendingPrompt(sessionId: string) {
  try {
    sessionStorage.removeItem(pendingKey(sessionId))
  } catch {
    /* ignore */
  }
}
