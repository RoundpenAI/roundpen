import assert from 'node:assert/strict'
import { describe, it, beforeEach } from 'node:test'
import {
  clearOnboardingDone,
  isOnboardingDone,
  markOnboardingDone,
  onboardingStorageKey,
} from './onboarding.ts'

function mockLocalStorage() {
  const map = new Map<string, string>()
  const store = {
    getItem: (k: string) => (map.has(k) ? map.get(k)! : null),
    setItem: (k: string, v: string) => {
      map.set(k, String(v))
    },
    removeItem: (k: string) => {
      map.delete(k)
    },
  }
  Object.defineProperty(globalThis, 'localStorage', {
    value: store,
    configurable: true,
  })
}

describe('onboarding storage', () => {
  beforeEach(() => {
    mockLocalStorage()
  })

  it('keys by username', () => {
    assert.equal(onboardingStorageKey('mike'), 'roundpen.onboarding.v1.mike')
    assert.equal(
      onboardingStorageKey('fnos-1000'),
      'roundpen.onboarding.v1.fnos-1000',
    )
  })

  it('marks done per user', () => {
    assert.equal(isOnboardingDone('mike'), false)
    markOnboardingDone('mike')
    assert.equal(isOnboardingDone('mike'), true)
    assert.equal(isOnboardingDone('fnos-1000'), false)
    clearOnboardingDone('mike')
    assert.equal(isOnboardingDone('mike'), false)
  })
})
