import type { AssistTicket } from '../api'

// ticketSessionPath returns the chat session that can answer a pending ticket,
// or null when the ticket is handled elsewhere (policy drafts are applied from
// the assistant detail page, and a ticket without a session has no transcript).
export function ticketSessionPath(t: AssistTicket): string | null {
  if (t.kind === 'policy_apply' || !t.sessionId) return null
  return `/a/${t.assistantId}/s/${t.sessionId}`
}
