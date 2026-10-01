import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import type { AgentMessage } from '../../api.ts'
import {
  appendThoughtMessage,
  clearStreaming,
  finalizeOpenThoughts,
  mergeHistoryWithLive,
  reconcileRefetch,
  thoughtDurationMs,
} from './sessionChatHelpers.ts'

function thought(
  id: string,
  content: string,
  meta: AgentMessage['meta'],
  createdAt: string,
): AgentMessage {
  return {
    id,
    sessionId: 's',
    role: 'assistant',
    content,
    meta,
    createdAt,
  }
}

describe('finalizeOpenThoughts', () => {
  it('stamps duration and completes an open thought', () => {
    const start = '2026-01-01T00:00:00.000Z'
    const end = Date.parse(start) + 4500
    const prev = [
      thought('th1', 'pondering', { type: 'thought', status: 'in_progress' }, start),
    ]
    const out = finalizeOpenThoughts(prev, end)
    assert.equal(out[0]?.meta?.status, 'completed')
    assert.equal(out[0]?.meta?.durationMs, 4500)
  })

  it('leaves completed thoughts untouched', () => {
    const prev = [
      thought(
        'th1',
        'done',
        { type: 'thought', status: 'completed', durationMs: 1200 },
        '2026-01-01T00:00:00.000Z',
      ),
    ]
    const out = finalizeOpenThoughts(prev, Date.parse('2026-01-01T00:00:10.000Z'))
    assert.equal(out, prev)
    assert.equal(out[0]?.meta?.durationMs, 1200)
  })
})

describe('appendThoughtMessage', () => {
  it('starts a new block after a completed thought (reply / tool boundary)', () => {
    const prev = [
      thought(
        'th1',
        'first',
        { type: 'thought', status: 'completed', durationMs: 2000 },
        '2026-01-01T00:00:00.000Z',
      ),
      {
        id: 'a1',
        sessionId: 's',
        role: 'assistant',
        content: 'reply text',
        meta: { type: 'agent_message', status: 'in_progress' },
        createdAt: '2026-01-01T00:00:03.000Z',
      },
    ]
    const out = appendThoughtMessage(prev, 's', 'second round')
    assert.equal(out.length, 3)
    assert.equal(out[2]?.content, 'second round')
    assert.equal(out[2]?.meta?.status, 'in_progress')
    assert.equal(out[0]?.meta?.durationMs, 2000)
  })

  it('does not reopen a completed thought across tools', () => {
    const prev: AgentMessage[] = [
      thought(
        'th1',
        'first',
        { type: 'thought', status: 'completed', durationMs: 900 },
        '2026-01-01T00:00:00.000Z',
      ),
      {
        id: 'tool-1',
        sessionId: 's',
        role: 'tool',
        content: '',
        meta: { type: 'tool_call', toolId: 'c1', title: 'Bash', status: 'completed' },
        createdAt: '2026-01-01T00:00:02.000Z',
      },
    ]
    const out = appendThoughtMessage(prev, 's', 'after tools')
    assert.equal(out.length, 3)
    assert.equal(out[2]?.content, 'after tools')
    assert.notEqual(out[2]?.id, 'th1')
  })
})

describe('clearStreaming', () => {
  it('finalizes open thoughts with durationMs', () => {
    const start = '2026-01-01T00:00:00.000Z'
    const prev = [
      thought('th1', 'x', { type: 'thought', status: 'in_progress' }, start),
      {
        id: 'a1',
        sessionId: 's',
        role: 'assistant',
        content: 'hi',
        meta: { type: 'agent_message', status: 'in_progress' },
        createdAt: '2026-01-01T00:00:02.000Z',
      },
    ]
    const before = thoughtDurationMs(prev[0]!)
    const out = clearStreaming(prev)
    assert.equal(out[0]?.meta?.status, 'completed')
    assert.ok((out[0]?.meta?.durationMs ?? 0) >= before)
    assert.equal(out[1]?.meta?.status, 'completed')
  })
})

describe('mergeHistoryWithLive', () => {
  it('keeps a live streaming reply on top of persisted history', () => {
    const hist: AgentMessage[] = [
      {
        id: 'u1',
        sessionId: 's',
        role: 'user',
        content: 'hello',
        createdAt: '2026-01-01T00:00:00.000Z',
      },
      {
        id: 'a0',
        sessionId: 's',
        role: 'assistant',
        content: 'earlier',
        createdAt: '2026-01-01T00:00:01.000Z',
      },
    ]
    const live: AgentMessage[] = [
      {
        id: 'stream-1',
        sessionId: 's',
        role: 'assistant',
        content: 'half reply',
        meta: { type: 'agent_message', status: 'in_progress' },
        createdAt: '2026-01-01T00:00:02.000Z',
      },
    ]
    const out = mergeHistoryWithLive(hist, live, 's')
    assert.equal(out.length, 3)
    assert.equal(out[0]?.id, 'u1')
    assert.equal(out[1]?.id, 'a0')
    assert.equal(out[2]?.content, 'half reply')
    assert.equal(out[2]?.meta?.status, 'in_progress')
  })

  it('returns hist alone when nothing is streaming live', () => {
    const hist: AgentMessage[] = [
      {
        id: 'u1',
        sessionId: 's',
        role: 'user',
        content: 'hi',
        createdAt: '2026-01-01T00:00:00.000Z',
      },
    ]
    const out = mergeHistoryWithLive(hist, hist, 's')
    assert.equal(out, hist)
  })

  it('ignores live overlays from another session', () => {
    const hist: AgentMessage[] = [
      {
        id: 'u1',
        sessionId: 's',
        role: 'user',
        content: 'hi',
        createdAt: '2026-01-01T00:00:00.000Z',
      },
    ]
    const live: AgentMessage[] = [
      {
        id: 'stream-x',
        sessionId: 'other',
        role: 'assistant',
        content: 'nope',
        meta: { type: 'agent_message', status: 'in_progress' },
        createdAt: '2026-01-01T00:00:02.000Z',
      },
    ]
    const out = mergeHistoryWithLive(hist, live, 's')
    assert.equal(out, hist)
  })

  it('does not duplicate tools already persisted in history', () => {
    const hist: AgentMessage[] = [
      {
        id: 'tool-row',
        sessionId: 's',
        role: 'tool',
        content: 'ok',
        meta: {
          type: 'tool_call',
          toolId: 'c1',
          title: 'Bash',
          status: 'completed',
        },
        createdAt: '2026-01-01T00:00:01.000Z',
      },
    ]
    const live: AgentMessage[] = [
      {
        id: 'tool-c1',
        sessionId: 's',
        role: 'tool',
        content: '',
        meta: {
          type: 'tool_call',
          toolId: 'c1',
          title: 'Bash',
          status: 'in_progress',
        },
        createdAt: '2026-01-01T00:00:01.000Z',
      },
      {
        id: 'stream-1',
        sessionId: 's',
        role: 'assistant',
        content: 'partial',
        meta: { type: 'agent_message', status: 'in_progress' },
        createdAt: '2026-01-01T00:00:02.000Z',
      },
    ]
    const out = mergeHistoryWithLive(hist, live, 's')
    assert.equal(out.length, 2)
    assert.equal(out[0]?.id, 'tool-row')
    assert.equal(out[1]?.id, 'stream-1')
  })
})

describe('reconcileRefetch', () => {
  it('keeps older pages when the recent window is refreshed', () => {
    const older: AgentMessage = {
      id: 'old',
      sessionId: 's',
      role: 'user',
      content: 'earlier',
      createdAt: '2026-01-01T00:00:00.000Z',
    }
    const mid: AgentMessage = {
      id: 'mid',
      sessionId: 's',
      role: 'assistant',
      content: 'mid',
      createdAt: '2026-01-01T00:00:01.000Z',
    }
    const newest: AgentMessage = {
      id: 'new',
      sessionId: 's',
      role: 'assistant',
      content: 'fresh',
      createdAt: '2026-01-01T00:00:02.000Z',
    }
    const prev = [older, mid]
    const fresh = [
      { ...mid, content: 'mid updated' },
      newest,
    ]
    const out = reconcileRefetch(prev, fresh)
    assert.equal(out.length, 3)
    assert.equal(out[0]?.id, 'old')
    assert.equal(out[1]?.content, 'mid updated')
    assert.equal(out[2]?.id, 'new')
  })
})
