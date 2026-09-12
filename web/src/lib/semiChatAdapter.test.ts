import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { agentMessageToSemi } from './semiChatAdapter.ts'
import type { AgentMessage } from '../api.ts'

describe('agentMessageToSemi', () => {
  it('maps user text', () => {
    const m: AgentMessage = {
      id: '1',
      sessionId: 's',
      role: 'user',
      content: 'hello',
      createdAt: '2026-01-01T00:00:00Z',
    }
    const out = agentMessageToSemi(m)
    assert.equal(out.role, 'user')
    assert.equal(out.id, '1')
    assert.equal(out.content, 'hello')
  })

  it('maps assistant streaming to in_progress', () => {
    const m: AgentMessage = {
      id: '2',
      sessionId: 's',
      role: 'assistant',
      content: 'partial',
      meta: { status: 'in_progress' },
      createdAt: '2026-01-01T00:00:00Z',
    }
    const out = agentMessageToSemi(m)
    assert.equal(out.role, 'assistant')
    assert.equal(out.status, 'in_progress')
  })

  it('maps thought to reasoning content', () => {
    const m: AgentMessage = {
      id: '3',
      sessionId: 's',
      role: 'assistant',
      content: 'thinking…',
      meta: { type: 'thought' },
      createdAt: '2026-01-01T00:00:00Z',
    }
    const out = agentMessageToSemi(m)
    assert.ok(Array.isArray(out.content))
    const items = out.content as { type: string }[]
    assert.equal(items[0]?.type, 'reasoning')
  })

  it('maps tool_call meta into function_call content', () => {
    const m: AgentMessage = {
      id: '4',
      sessionId: 's',
      role: 'tool',
      content: '',
      meta: {
        type: 'tool_call',
        toolId: 't1',
        title: 'Bash',
        status: 'completed',
        input: { cmd: 'ls' },
        output: 'ok',
      },
      createdAt: '2026-01-01T00:00:00Z',
    }
    const out = agentMessageToSemi(m)
    assert.equal(out.role, 'assistant')
    assert.ok(Array.isArray(out.content))
    const items = out.content as { type: string; name?: string }[]
    assert.equal(items[0]?.type, 'function_call')
    assert.equal(items[0]?.name, 'Bash')
  })

  it('degrades event types to system text', () => {
    const m: AgentMessage = {
      id: '5',
      sessionId: 's',
      role: 'system',
      content: 'notice',
      meta: { type: 'event' },
      createdAt: '2026-01-01T00:00:00Z',
    }
    const out = agentMessageToSemi(m)
    assert.equal(out.role, 'system')
    assert.equal(out.content, 'notice')
  })
})
