/** One queued user message as broadcast by the server's queue frames. */
export type QueueItem = {
  id: string
  text: string
  clientMsgId?: string
}

export type QueueBadge = 'queued' | 'cancelled' | null

/**
 * Match a rendered message against the queue. Optimistic rows carry the
 * clientMsgId as their message id until a refetch replaces them with the DB
 * row, so both keys must match.
 */
export function queueItemFor(
  items: QueueItem[],
  messageId: string,
): QueueItem | null {
  if (!messageId) return null
  return (
    items.find((it) => it.id === messageId || it.clientMsgId === messageId) ??
    null
  )
}

/**
 * Badge for one message id. Queued wins over cancelled: a pull-back the
 * server refused keeps the message in the queue and must not look cancelled.
 */
export function queueBadge(
  items: QueueItem[],
  cancelled: ReadonlySet<string>,
  messageId: string,
): QueueBadge {
  if (queueItemFor(items, messageId)) return 'queued'
  if (cancelled.has(messageId)) return 'cancelled'
  return null
}

/** Parse a server queue/status payload into items, tolerating junk. */
export function parseQueueItems(raw: unknown): QueueItem[] {
  if (!Array.isArray(raw)) return []
  const out: QueueItem[] = []
  for (const it of raw) {
    if (!it || typeof it !== 'object') continue
    const rec = it as Record<string, unknown>
    const id = typeof rec.id === 'string' ? rec.id : ''
    if (!id) continue
    out.push({
      id,
      text: typeof rec.text === 'string' ? rec.text : '',
      clientMsgId:
        typeof rec.clientMsgId === 'string' ? rec.clientMsgId : undefined,
    })
  }
  return out
}

let seq = 0

/** Correlation id echoing back on queue frames for optimistic rows. */
export function newClientMsgId(): string {
  seq += 1
  return `c-${Date.now().toString(36)}-${seq}`
}

export function queueHint(count: number): string | null {
  return count > 0 ? `已排队 ${count} 条` : null
}
