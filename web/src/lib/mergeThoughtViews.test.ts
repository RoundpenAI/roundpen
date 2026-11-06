import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { mergeThoughtViews } from './mergeThoughtViews.ts'

describe('mergeThoughtViews', () => {
  it('returns null for empty thoughts', () => {
    assert.equal(mergeThoughtViews([]), null)
  })

  it('passes through a single thought', () => {
    const one = { text: 'alone', durationMs: 1200 }
    assert.deepEqual(mergeThoughtViews([one]), one)
  })

  it('joins multiple thoughts into one 已思考 row', () => {
    const merged = mergeThoughtViews([
      { text: 'first', startedAtMs: 100, durationMs: 1000 },
      { text: 'second', startedAtMs: 200, durationMs: 500 },
    ])
    assert.equal(merged?.text, 'first\n\nsecond')
    assert.equal(merged?.startedAtMs, 100)
    assert.equal(merged?.durationMs, 1500)
    assert.equal(merged?.streaming, false)
  })

  it('keeps streaming when any segment is in progress', () => {
    const merged = mergeThoughtViews([
      { text: 'done', durationMs: 800 },
      { text: 'still going', streaming: true, startedAtMs: 50 },
    ])
    assert.equal(merged?.streaming, true)
    assert.equal(merged?.startedAtMs, 50)
    assert.equal(merged?.durationMs, undefined)
  })
})
