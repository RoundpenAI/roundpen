import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { mergeThoughtSegments } from './mergeThoughtSegments.ts'

describe('mergeThoughtSegments', () => {
  it('returns a single segment unchanged', () => {
    const one = [
      { text: 'plan', durationMs: 1200, startedAtMs: 1000, streaming: false },
    ]
    assert.deepEqual(mergeThoughtSegments(one), one)
  })

  it('folds completed segments into one block with summed duration', () => {
    const out = mergeThoughtSegments([
      { text: 'first', durationMs: 1000, startedAtMs: 1000 },
      { text: 'second', durationMs: 2000, startedAtMs: 2500 },
      { text: '  ', durationMs: 999 },
    ])
    assert.equal(out.length, 1)
    assert.equal(out[0]?.text, 'first\n\nsecond')
    assert.equal(out[0]?.durationMs, 3000)
    assert.equal(out[0]?.startedAtMs, 1000)
    assert.equal(out[0]?.streaming, false)
  })

  it('keeps a live timer when any segment is still streaming', () => {
    const out = mergeThoughtSegments([
      { text: 'done bit', durationMs: 800, startedAtMs: 1000 },
      { text: 'still going', streaming: true, startedAtMs: 2000 },
    ])
    assert.equal(out.length, 1)
    assert.equal(out[0]?.streaming, true)
    assert.equal(out[0]?.startedAtMs, 1000)
    assert.equal(out[0]?.durationMs, undefined)
    assert.equal(out[0]?.text, 'done bit\n\nstill going')
  })
})
