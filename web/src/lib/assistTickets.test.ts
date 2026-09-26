import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import type { AssistTicket } from '../api'
import { ticketSessionPath } from './assistTickets.ts'

function ticket(over: Partial<AssistTicket>): AssistTicket {
  return {
    id: 't-1',
    userId: 'u-1',
    assistantId: 'asst-1',
    sessionId: 'sess-1',
    kind: 'permission',
    status: 'pending',
    title: '需要确认：Bash',
    reason: '',
    contextSummary: '',
    askHuman: '',
    createdAt: '',
    updatedAt: '',
    ...over,
  }
}

describe('ticketSessionPath', () => {
  it('points permission tickets at the session that raised them', () => {
    assert.equal(ticketSessionPath(ticket({})), '/a/asst-1/s/sess-1')
  })

  it('leaves policy drafts to the assistant detail page', () => {
    assert.equal(ticketSessionPath(ticket({ kind: 'policy_apply' })), null)
  })

  it('has no session to open when the ticket carries none', () => {
    assert.equal(ticketSessionPath(ticket({ sessionId: '' })), null)
  })
})
