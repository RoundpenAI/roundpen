import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { isSystemAssistant, pickHomeAssistant } from './assistants.ts'

describe('pickHomeAssistant', () => {
  it('prefers system over newer user assistants', () => {
    const home = pickHomeAssistant([
      { id: 'u1', kind: 'user' },
      { id: 's1', kind: 'system' },
    ])
    assert.equal(home?.id, 's1')
  })

  it('falls back to first when no system', () => {
    const home = pickHomeAssistant([{ id: 'u1', kind: 'user' }])
    assert.equal(home?.id, 'u1')
  })

  it('returns undefined for empty', () => {
    assert.equal(pickHomeAssistant([]), undefined)
  })
})

describe('isSystemAssistant', () => {
  it('detects system kind', () => {
    assert.equal(isSystemAssistant({ kind: 'system' }), true)
    assert.equal(isSystemAssistant({ kind: 'user' }), false)
    assert.equal(isSystemAssistant(null), false)
  })
})
