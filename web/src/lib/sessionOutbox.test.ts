import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  drainOutbox,
  enqueueOutbox,
  outboxWaitingHint,
} from './sessionOutbox.ts'

describe('sessionOutbox', () => {
  it('enqueues trimmed text and ignores empty', () => {
    let q: string[] = []
    q = enqueueOutbox(q, '  hi  ')
    q = enqueueOutbox(q, '   ')
    q = enqueueOutbox(q, 'second')
    assert.deepEqual(q, ['hi', 'second'])
  })

  it('drain clears queue and returns items', () => {
    const { remaining, items } = drainOutbox(['a', 'b'])
    assert.deepEqual(items, ['a', 'b'])
    assert.deepEqual(remaining, [])
  })

  it('waiting hint only when queued', () => {
    assert.equal(outboxWaitingHint(0), null)
    assert.equal(outboxWaitingHint(1), '连接后发送…')
  })
})
