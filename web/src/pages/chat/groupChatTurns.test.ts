import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { CLEAR_DIVIDER_MODEL, type SemiChatMessage } from '../../lib/semiChatAdapter.ts'
import { groupChatTurns } from './groupChatTurns.ts'

function msg(
  id: string,
  role: string,
  extra?: Partial<SemiChatMessage>,
): SemiChatMessage {
  return { id, role, content: id, ...extra }
}

describe('groupChatTurns', () => {
  it('groups assistant replies under the preceding user message', () => {
    const turns = groupChatTurns([
      msg('u1', 'user'),
      msg('a1', 'assistant'),
      msg('a2', 'assistant'),
      msg('u2', 'user'),
      msg('a3', 'assistant'),
    ])
    assert.deepEqual(
      turns.map((t) => t.map((m) => m.id)),
      [
        ['u1', 'a1', 'a2'],
        ['u2', 'a3'],
      ],
    )
  })

  it('keeps a preamble before the first user message', () => {
    const turns = groupChatTurns([
      msg('s1', 'system'),
      msg('u1', 'user'),
      msg('a1', 'assistant'),
    ])
    assert.deepEqual(
      turns.map((t) => t.map((m) => m.id)),
      [['s1', 'u1', 'a1']],
    )
  })

  it('starts a new turn on consecutive user messages', () => {
    const turns = groupChatTurns([msg('u1', 'user'), msg('u2', 'user')])
    assert.deepEqual(
      turns.map((t) => t.map((m) => m.id)),
      [['u1'], ['u2']],
    )
  })

  it('isolates clear dividers between turns', () => {
    const turns = groupChatTurns([
      msg('u1', 'user'),
      msg('a1', 'assistant'),
      msg('c1', 'system', { model: CLEAR_DIVIDER_MODEL }),
      msg('u2', 'user'),
    ])
    assert.deepEqual(
      turns.map((t) => t.map((m) => m.id)),
      [['u1', 'a1'], ['c1'], ['u2']],
    )
  })
})
