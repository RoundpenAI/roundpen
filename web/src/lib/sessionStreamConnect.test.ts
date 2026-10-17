import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  STREAM_CONNECTING,
  STREAM_OPEN,
  shouldApplyStreamOpen,
  streamLooksOpen,
  type ChatStreamHandle,
} from './sessionStreamConnect.ts'

describe('shouldApplyStreamOpen', () => {
  it('ignores open from an orphaned (replaced) stream', () => {
    const a = { readyState: STREAM_OPEN, close() {} } as ChatStreamHandle
    const b = { readyState: STREAM_OPEN, close() {} } as ChatStreamHandle
    assert.equal(shouldApplyStreamOpen(false, b, a), false)
    assert.equal(shouldApplyStreamOpen(false, b, b), true)
  })

  it('ignores open after effect dispose', () => {
    const a = { readyState: STREAM_OPEN, close() {} } as ChatStreamHandle
    assert.equal(shouldApplyStreamOpen(true, a, a), false)
  })
})

describe('streamLooksOpen', () => {
  it('uses readyState OPEN only', () => {
    assert.equal(streamLooksOpen(null), false)
    assert.equal(
      streamLooksOpen({ readyState: STREAM_CONNECTING, close() {} }),
      false,
    )
    assert.equal(
      streamLooksOpen({ readyState: STREAM_OPEN, close() {} }),
      true,
    )
  })
})
