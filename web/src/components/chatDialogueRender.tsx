import type { ReactNode } from 'react'
import { ChangeSetCard } from '../components/ChangeSetCard'
import {
  ThoughtBlock,
  ToolCallGroup,
  type ToolCallData,
} from '../components/ToolCallCard'
import { buildChangeSet, isChangeSetTool } from '../lib/changeSet'
import {
  isActivityMessage,
  isClearDivider,
  type SemiContentItem,
} from '../lib/semiChatAdapter'
import type { QueueBadge } from '../pages/chat/sessionQueue'

// Structural view of Semi's Message; the library type is wider than the
// adapter's SemiChatMessage, so render hooks take the minimal shape they need.
type DialogueMessage = {
  id?: string
  model?: string
  content?: unknown
  status?: string
  createdAt?: number
}

type ContentProps = {
  message?: DialogueMessage
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

function activityFromMessage(message: DialogueMessage): {
  thoughts: {
    text: string
    streaming?: boolean
    startedAtMs?: number
    durationMs?: number
  }[]
  tools: ToolCallData[]
} {
  const items = Array.isArray(message.content)
    ? (message.content as SemiContentItem[])
    : []
  const thoughts: {
    text: string
    streaming?: boolean
    startedAtMs?: number
    durationMs?: number
  }[] = []
  const tools: ToolCallData[] = []
  for (const item of items) {
    if (item.type === 'reasoning') {
      const text = item.summary?.map((s) => s.text).join('\n') || ''
      thoughts.push({
        text,
        startedAtMs: item.createdAt,
        durationMs: item.durationMs,
        // Per-thought status: a finished segment must stop the timer even if
        // the surrounding activity turn is still in progress (tools / later text).
        streaming: item.status === 'in_progress',
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

export function renderActivityContent(message: DialogueMessage): ReactNode {
  const { thoughts, tools } = activityFromMessage(message)
  const changeSet = buildChangeSet(tools)
  // ChangeSet already surfaces completed Edit/Write; keep other tools (and
  // still-running / failed edits) so cards do not repeat the same diffs.
  const otherTools = changeSet
    ? tools.filter((t) => {
        if (!isChangeSetTool(t)) return true
        return (
          t.status === 'pending' ||
          t.status === 'in_progress' ||
          t.status === 'failed'
        )
      })
    : tools
  return (
    <div className="chat-activity">
      {thoughts.map((t, i) =>
        t.text ? (
          <ThoughtBlock
            key={i}
            text={t.text}
            streaming={t.streaming}
            startedAtMs={t.startedAtMs}
            durationMs={t.durationMs}
          />
        ) : null,
      )}
      {otherTools.length > 0 ? (
        <ToolCallGroup calls={otherTools} alwaysStats />
      ) : null}
      {changeSet ? <ChangeSetCard changeSet={changeSet} /> : null}
    </div>
  )
}

export type DialogueQueueDeps = {
  badgeFor: (messageId: string) => QueueBadge
  onUnqueue: (messageId: string) => void
}

/** Hide avatar/title; collapse thought+tool activity into one compact block.
 *  Important: Semi already puts the bubble class on nodes inside `defaultContent`.
 *  Do NOT re-apply `className` (wrapCls) around it — that creates a double card. */
export function chatDialogueRenderConfig(queue?: DialogueQueueDeps) {
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
      if (message && isClearDivider(message)) {
        return (
          <div className="chat-clear-divider">
            <span>上下文已清空</span>
          </div>
        )
      }
      const messageId = message?.id ?? ''
      const badge = queue && messageId ? queue.badgeFor(messageId) : null
      if (badge === 'queued' && queue) {
        return (
          <div className="chat-queue-wrap">
            <span className="chat-queue-line">
              <span className="chat-queue-tag">排队中</span>
              <button
                type="button"
                className="chat-queue-undo"
                title="撤回这条排队消息"
                onClick={() => queue.onUnqueue(messageId)}
              >
                撤回
              </button>
            </span>
            {defaultContent}
          </div>
        )
      }
      if (badge === 'cancelled') {
        return (
          <div className="chat-queue-wrap is-cancelled">
            <span className="chat-queue-line">
              <span className="chat-queue-tag">已撤回</span>
            </span>
            {defaultContent}
          </div>
        )
      }
      return defaultContent
    },
  }
}
