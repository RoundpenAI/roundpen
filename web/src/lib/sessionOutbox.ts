/** In-memory outbox for prompts typed while the session WS is not OPEN. */

export function enqueueOutbox(queue: string[], text: string): string[] {
  const t = text.trim()
  if (!t) return queue
  return [...queue, t]
}

export function drainOutbox(queue: string[]): { remaining: string[]; items: string[] } {
  if (!queue.length) return { remaining: queue, items: [] }
  return { remaining: [], items: [...queue] }
}

export function outboxWaitingHint(queueLen: number): string | null {
  return queueLen > 0 ? '连接后发送…' : null
}
