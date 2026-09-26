import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  newClientMsgId,
  parseQueueItems,
  queueBadge,
  queueHint,
  queueItemFor,
  type QueueItem,
} from './sessionQueue.ts'

const items: QueueItem[] = [
  { id: 'db-1', text: 'first', clientMsgId: 'c-1' },
  { id: 'db-2', text: 'second' },
]

describe('sessionQueue matching', () => {
  it('matches by db id and by optimistic clientMsgId', () => {
    assert.equal(queueItemFor(items, 'db-1')?.text, 'first')
    assert.equal(queueItemFor(items, 'c-1')?.text, 'first')
    assert.equal(queueItemFor(items, 'db-2')?.text, 'second')
    assert.equal(queueItemFor(items, 'missing'), null)
    assert.equal(queueItemFor(items, ''), null)
  })

  it('queued badge wins over a cancelled mark (refused pull-back)', () => {
    const cancelled = new Set(['db-1', 'db-9'])
    assert.equal(queueBadge(items, cancelled, 'db-1'), 'queued')
    assert.equal(queueBadge(items, cancelled, 'db-9'), 'cancelled')
    assert.equal(queueBadge(items, new Set(), 'db-2'), 'queued')
    assert.equal(queueBadge(items, new Set(), 'db-3'), null)
  })
})

describe('parseQueueItems', () => {
  it('keeps well-formed items and drops junk', () => {
    const parsed = parseQueueItems([
      { id: 'a', text: 'one', clientMsgId: 'c-1' },
      { id: 'b' },
      { text: 'no id' },
      null,
      'nope',
      42,
    ])
    assert.deepEqual(parsed, [
      { id: 'a', text: 'one', clientMsgId: 'c-1' },
      { id: 'b', text: '', clientMsgId: undefined },
    ])
  })

  it('tolerates non-array payloads', () => {
    assert.deepEqual(parseQueueItems(undefined), [])
    assert.deepEqual(parseQueueItems(null), [])
    assert.deepEqual(parseQueueItems('x'), [])
  })
})

describe('queueHint', () => {
  it('only hints while items are queued', () => {
    assert.equal(queueHint(0), null)
    assert.equal(queueHint(3), '已排队 3 条')
  })
})

describe('newClientMsgId', () => {
  it('is unique across calls', () => {
    const ids = new Set([newClientMsgId(), newClientMsgId(), newClientMsgId()])
    assert.equal(ids.size, 3)
  })
})
