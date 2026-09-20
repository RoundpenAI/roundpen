import type { AgentCommand, AgentMessage } from '../../api'
import { chatDialogueRenderConfig } from '../../components/chatDialogueRender'

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

export const DIALOGUE_RENDER = chatDialogueRenderConfig()

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

export function clearStreaming(prev: AgentMessage[]): AgentMessage[] {
  return prev.map((m) =>
    isStreamingMessage(m)
      ? { ...m, meta: { ...m.meta, status: 'completed' } }
      : m,
  )
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
    if (typ === 'thought' || m.role === 'thought') {
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
