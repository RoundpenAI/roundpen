import type { ReactNode } from 'react'
import {
  ThoughtBlock,
  ToolCallGroup,
  type ToolCallData,
} from '../components/ToolCallCard'
import {
  isActivityMessage,
  type SemiChatMessage,
  type SemiContentItem,
} from '../lib/semiChatAdapter'

type ContentProps = {
  message?: SemiChatMessage
  defaultContent?: ReactNode | ReactNode[]
  className?: string
}

function parseArgs(raw: string | undefined): unknown {
  if (!raw) return undefined
  try {
    return JSON.parse(raw)
  } catch {
    return raw
  }
}

function activityFromMessage(message: SemiChatMessage): {
  thoughts: { text: string; streaming?: boolean }[]
  tools: ToolCallData[]
} {
  const items = Array.isArray(message.content)
    ? (message.content as SemiContentItem[])
    : []
  const thoughts: { text: string; streaming?: boolean }[] = []
  const tools: ToolCallData[] = []
  for (const item of items) {
    if (item.type === 'reasoning') {
      const text = item.summary?.map((s) => s.text).join('\n') || ''
      thoughts.push({
        text,
        streaming: message.status === 'in_progress',
      })
    } else if (item.type === 'function_call') {
      tools.push({
        toolId: item.call_id || item.name || 'tool',
        title: item.name || 'tool',
        status: item.status || 'completed',
        input: parseArgs(item.arguments),
        output: item.output,
      })
    }
  }
  return { thoughts, tools }
}

export function renderActivityContent(message: SemiChatMessage): ReactNode {
  const { thoughts, tools } = activityFromMessage(message)
  const thoughtText = thoughts.map((t) => t.text).filter(Boolean).join('\n\n')
  const streaming = thoughts.some((t) => t.streaming)
  return (
    <div className="chat-activity">
      {thoughtText ? (
        <ThoughtBlock text={thoughtText} streaming={streaming} />
      ) : null}
      {tools.length > 0 ? <ToolCallGroup calls={tools} alwaysStats /> : null}
    </div>
  )
}

/** Hide avatar/title; collapse thought+tool activity into one compact block.
 *  Important: Semi already puts the bubble class on nodes inside `defaultContent`.
 *  Do NOT re-apply `className` (wrapCls) around it — that creates a double card. */
export function chatDialogueRenderConfig() {
  return {
    renderDialogueAvatar: () => null,
    renderDialogueTitle: () => null,
    renderDialogueAction: () => null,
    renderDialogueContent: ({ message, defaultContent }: ContentProps) => {
      if (message && isActivityMessage(message)) {
        return (
          <div className="chat-activity-wrap">{renderActivityContent(message)}</div>
        )
      }
      return defaultContent
    },
  }
}
