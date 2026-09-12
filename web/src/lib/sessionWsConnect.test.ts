import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  shouldApplySocketOpen,
  socketLooksOpen,
} from './sessionWsConnect.ts'

describe('shouldApplySocketOpen', () => {
  it('ignores open from an orphaned (replaced) socket', () => {
    const a = { id: 'a' } as unknown as WebSocket
    const b = { id: 'b' } as unknown as WebSocket
    assert.equal(shouldApplySocketOpen(false, b, a), false)
    assert.equal(shouldApplySocketOpen(false, b, b), true)
  })

  it('ignores open after effect dispose', () => {
    const a = { id: 'a' } as unknown as WebSocket
    assert.equal(shouldApplySocketOpen(true, a, a), false)
  })
})

describe('socketLooksOpen', () => {
  it('uses readyState OPEN only', () => {
    assert.equal(socketLooksOpen(null), false)
    assert.equal(
      socketLooksOpen({ readyState: 0 } as unknown as WebSocket),
      false,
    )
    assert.equal(
      socketLooksOpen({ readyState: 1 } as unknown as WebSocket),
      true,
    )
  })
})
