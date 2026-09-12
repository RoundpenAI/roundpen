import type { AgentMessage } from '../api'

/** Marker on Semi messages that only carry thought + tool activity (no bubble chrome). */
export const ACTIVITY_MODEL = 'roundpen-activity'

/** Minimal Semi AIChatDialogue Message shape (compatible with foundation Message). */
export type SemiChatMessage = {
  id: string
  role: string
  content?: string | SemiContentItem[]
  status?: string
  createdAt?: number
  model?: string
}

export type SemiContentItem =
  | {
      type: 'message'
      role?: string
      content?: Array<{ type: 'output_text' | 'input_text'; text: string }>
    }
  | {
      type: 'reasoning'
      summary: Array<{ type: 'summary_text'; text: string }>
    }
  | {
      type: 'function_call'
      call_id?: string
      name?: string
      arguments?: string
      status?: string
      output?: string
    }

function createdAtMs(iso: string | undefined): number | undefined {
  if (!iso) return undefined
  const t = Date.parse(iso)
  return Number.isFinite(t) ? t : undefined
}

function formatUnknown(value: unknown): string {
  if (value == null) return ''
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function mapStatus(metaStatus: string | undefined, streaming?: boolean): string | undefined {
  if (streaming) return 'in_progress'
  if (!metaStatus) return undefined
  if (metaStatus === 'pending' || metaStatus === 'in_progress') return 'in_progress'
  if (metaStatus === 'failed' || metaStatus === 'error') return 'failed'
  if (metaStatus === 'cancelled') return 'cancelled'
  return 'completed'
}

function isThought(m: AgentMessage): boolean {
  const typ = m.meta?.type || m.role
  return typ === 'thought' || typ === 'reasoning' || m.role === 'thought'
}

function isTool(m: AgentMessage): boolean {
  const typ = m.meta?.type || m.role
  return m.role === 'tool' || typ === 'tool_call' || Boolean(m.meta?.toolId && typ !== 'permission')
}

function thoughtItem(m: AgentMessage): SemiContentItem {
  return {
    type: 'reasoning',
    summary: [{ type: 'summary_text', text: m.content || '' }],
  }
}

function toolItem(m: AgentMessage): SemiContentItem {
  const name = m.meta?.title || m.meta?.toolId || m.id
  const args = formatUnknown(m.meta?.input)
  const output = formatUnknown(m.meta?.output ?? m.content)
  return {
    type: 'function_call',
    call_id: m.meta?.toolId || m.id,
    name,
    arguments: args || undefined,
    status: m.meta?.status,
    output: output || undefined,
  }
}

/**
 * Map Roundpen ACP / AgentMessage rows into Semi AIChatDialogue messages.
 * Unknown types degrade to system/user/assistant text so the session still renders.
 */
export function agentMessageToSemi(m: AgentMessage): SemiChatMessage {
  const typ = m.meta?.type || m.role
  const createdAt = createdAtMs(m.createdAt)

  if (isTool(m)) {
    return {
      id: m.id,
      role: 'assistant',
      createdAt,
      status: mapStatus(m.meta?.status),
      content: [toolItem(m)],
    }
  }

  if (isThought(m)) {
    return {
      id: m.id,
      role: 'assistant',
      createdAt,
      status: mapStatus(m.meta?.status),
      content: [thoughtItem(m)],
    }
  }

  if (m.role === 'user' || typ === 'user') {
    return {
      id: m.id,
      role: 'user',
      content: m.content,
      createdAt,
      status: 'completed',
    }
  }

  if (typ === 'permission') {
    const title = m.meta?.title ? `${m.meta.title}: ` : ''
    return {
      id: m.id,
      role: 'system',
      content: `${title}${m.content || '需要确认'}`,
      createdAt,
      status: mapStatus(m.meta?.status) ?? 'completed',
    }
  }

  if (typ === 'event' || typ === 'system' || m.role === 'system') {
    return {
      id: m.id,
      role: 'system',
      content: m.content || typ,
      createdAt,
      status: 'completed',
    }
  }

  const streaming =
    m.meta?.status === 'in_progress' || m.meta?.status === 'pending'
  return {
    id: m.id,
    role: 'assistant',
    content: m.content,
    createdAt,
    status: mapStatus(m.meta?.status, streaming) ?? 'completed',
  }
}

function activityStatus(items: SemiContentItem[]): string {
  for (const item of items) {
    if (item.type === 'function_call' && (item.status === 'pending' || item.status === 'in_progress')) {
      return 'in_progress'
    }
  }
  return 'completed'
}

/**
 * Collapse consecutive thought + tool rows into one activity message so the UI
 * can render a single collapsible group instead of one bubble per tool.
 */
export function agentMessagesToSemi(messages: AgentMessage[]): SemiChatMessage[] {
  const out: SemiChatMessage[] = []
  let turnItems: SemiContentItem[] = []
  let turnId = ''
  let turnCreated: number | undefined

  const flushTurn = () => {
    if (turnItems.length === 0) return
    out.push({
      id: turnId || `turn-${out.length}`,
      role: 'assistant',
      model: ACTIVITY_MODEL,
      createdAt: turnCreated,
      status: activityStatus(turnItems),
      content: turnItems,
    })
    turnItems = []
    turnId = ''
    turnCreated = undefined
  }

  for (const m of messages) {
    if (isThought(m) || isTool(m)) {
      if (turnItems.length === 0) {
        turnId = `turn-${m.id}`
        turnCreated = createdAtMs(m.createdAt)
      }
      turnItems.push(isThought(m) ? thoughtItem(m) : toolItem(m))
      continue
    }
    flushTurn()
    out.push(agentMessageToSemi(m))
  }
  flushTurn()
  return out
}

export function isActivityMessage(message: { model?: string } | null | undefined): boolean {
  return message?.model === ACTIVITY_MODEL
}
