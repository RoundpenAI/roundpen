import type { AgentMessage } from '../api'

/** Minimal Semi AIChatDialogue Message shape (compatible with foundation Message). */
export type SemiChatMessage = {
  id: string
  role: string
  content?: string | SemiContentItem[]
  status?: string
  createdAt?: number
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

/**
 * Map Roundpen ACP / AgentMessage rows into Semi AIChatDialogue messages.
 * Unknown types degrade to system/user/assistant text so the session still renders.
 */
export function agentMessageToSemi(m: AgentMessage): SemiChatMessage {
  const typ = m.meta?.type || m.role
  const createdAt = createdAtMs(m.createdAt)

  if (m.role === 'tool' || typ === 'tool_call' || (m.meta?.toolId && typ !== 'permission')) {
    const name = m.meta?.title || m.meta?.toolId || m.id
    const args = formatUnknown(m.meta?.input)
    const output = formatUnknown(m.meta?.output ?? m.content)
    return {
      id: m.id,
      role: 'assistant',
      createdAt,
      status: mapStatus(m.meta?.status),
      content: [
        {
          type: 'function_call',
          call_id: m.meta?.toolId || m.id,
          name,
          arguments: args || undefined,
          status: m.meta?.status,
          output: output || undefined,
        },
      ],
    }
  }

  if (typ === 'thought' || typ === 'reasoning') {
    return {
      id: m.id,
      role: 'assistant',
      createdAt,
      status: mapStatus(m.meta?.status),
      content: [
        {
          type: 'reasoning',
          summary: [{ type: 'summary_text', text: m.content || '' }],
        },
      ],
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

  // assistant / agent_message / default
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

export function agentMessagesToSemi(messages: AgentMessage[]): SemiChatMessage[] {
  return messages.map(agentMessageToSemi)
}
