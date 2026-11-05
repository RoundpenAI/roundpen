import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { decideTofu } from './tofu.ts'

describe('decideTofu', () => {
  it('registers when no pubkey is stored', () => {
    assert.deepEqual(decideTofu(undefined, 'aa', 0), { kind: 'register' })
  })

  it('matches the same pubkey and keeps the reinstall count', () => {
    assert.deepEqual(decideTofu('AA', 'aa', 3), { kind: 'match', reinstallCount: 3 })
  })

  it('rotates on a new pubkey and increments the reinstall count', () => {
    assert.deepEqual(decideTofu('aa', 'bb', 0), { kind: 'rotate', reinstallCount: 1 })
    assert.deepEqual(decideTofu('aa', 'bb', 4), { kind: 'rotate', reinstallCount: 5 })
  })
})
