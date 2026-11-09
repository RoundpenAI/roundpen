import type { SemiChatMessage } from '../../lib/semiChatAdapter'

function isClearDivider(message: SemiChatMessage): boolean {
  return message.model === 'roundpen-clear-divider'
}

/**
 * Split a flat Semi dialogue list into Q&A turns so each turn can own its
 * sticky user question. A new turn starts at every user message after one
 * already appears in the current turn; /clear dividers flush and stand alone.
 */
export function groupChatTurns(chats: SemiChatMessage[]): SemiChatMessage[][] {
  const turns: SemiChatMessage[][] = []
  let cur: SemiChatMessage[] = []
  let curHasUser = false

  const flush = () => {
    if (cur.length === 0) return
    turns.push(cur)
    cur = []
    curHasUser = false
  }

  for (const m of chats) {
    if (isClearDivider(m)) {
      flush()
      turns.push([m])
      continue
    }
    if (m.role === 'user' && curHasUser) {
      flush()
    }
    cur.push(m)
    if (m.role === 'user') curHasUser = true
  }
  flush()
  return turns
}
