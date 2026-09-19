import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  ACTIVITY_MODEL,
  CLEAR_DIVIDER_MODEL,
  agentMessageToSemi,
  agentMessagesToSemi,
  isActivityMessage,
  isClearDivider,
} from './semiChatAdapter.ts'
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

  it('shows the typed text for slash command turns', () => {
    const m: AgentMessage = {
      id: 'cmd',
      sessionId: 's',
      role: 'user',
      content: 'Running skill "review" (builtin).\n\nReview the most recent changes…',
      meta: { type: 'user', command: 'review', commandArgs: '关注并发', display: '/review 关注并发' },
      createdAt: '2026-01-01T00:00:00Z',
    }
    const out = agentMessageToSemi(m)
    assert.equal(out.role, 'user')
    assert.equal(out.content, '/review 关注并发')
  })

  it('marks /clear markers as a divider', () => {
    const m: AgentMessage = {
      id: 'clear',
      sessionId: 's',
      role: 'event',
      content: '上下文已清空',
      meta: { type: 'clear' },
      createdAt: '2026-01-01T00:00:00Z',
    }
    const out = agentMessageToSemi(m)
    assert.equal(out.model, CLEAR_DIVIDER_MODEL)
    assert.equal(isClearDivider(out), true)
    assert.equal(isActivityMessage(out), false)
  })
})

describe('agentMessagesToSemi', () => {
  it('groups consecutive thoughts and tools into one activity message', () => {
    const rows: AgentMessage[] = [
      {
        id: 'u1',
        sessionId: 's',
        role: 'user',
        content: 'go',
        createdAt: '2026-01-01T00:00:00Z',
      },
      {
        id: 'th1',
        sessionId: 's',
        role: 'assistant',
        content: 'plan',
        meta: { type: 'thought' },
        createdAt: '2026-01-01T00:00:01Z',
      },
      {
        id: 't1',
        sessionId: 's',
        role: 'tool',
        content: '',
        meta: {
          type: 'tool_call',
          toolId: 'c1',
          title: 'Read',
          status: 'completed',
        },
        createdAt: '2026-01-01T00:00:02Z',
      },
      {
        id: 't2',
        sessionId: 's',
        role: 'tool',
        content: '',
        meta: {
          type: 'tool_call',
          toolId: 'c2',
          title: 'Bash',
          status: 'completed',
        },
        createdAt: '2026-01-01T00:00:03Z',
      },
      {
        id: 'a1',
        sessionId: 's',
        role: 'assistant',
        content: 'done',
        createdAt: '2026-01-01T00:00:04Z',
      },
    ]
    const out = agentMessagesToSemi(rows)
    assert.equal(out.length, 3)
    assert.equal(out[0]?.role, 'user')
    assert.equal(out[1]?.model, ACTIVITY_MODEL)
    assert.ok(Array.isArray(out[1]?.content))
    assert.equal((out[1]?.content as unknown[]).length, 3)
    assert.equal(out[2]?.content, 'done')
  })

  it('marks an activity turn in_progress while a thought streams', () => {
    const rows: AgentMessage[] = [
      {
        id: 'th1',
        sessionId: 's',
        role: 'assistant',
        content: 'checking git status',
        meta: { type: 'thought', status: 'in_progress' },
        createdAt: '2026-01-01T00:00:00Z',
      },
      {
        id: 't1',
        sessionId: 's',
        role: 'tool',
        content: '',
        meta: {
          type: 'tool_call',
          toolId: 'c1',
          title: 'Bash',
          status: 'completed',
        },
        createdAt: '2026-01-01T00:00:00Z',
      },
    ]
    const out = agentMessagesToSemi(rows)
    assert.equal(out.length, 1)
    assert.equal(out[0]?.model, ACTIVITY_MODEL)
    assert.equal(out[0]?.status, 'in_progress')
  })
})
