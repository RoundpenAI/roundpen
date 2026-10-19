import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { selectScopes } from './scopes.ts'

const google = {
  defaultScopes:
    'openid email https://www.googleapis.com/auth/gmail.readonly https://www.googleapis.com/auth/calendar.readonly',
  allowedScopes: [
    'openid',
    'email',
    'https://www.googleapis.com/auth/gmail.readonly',
    'https://www.googleapis.com/auth/calendar.readonly',
  ],
}

describe('selectScopes', () => {
  it('uses the provider default when the NAS omits scopes', () => {
    assert.equal(selectScopes(google, ''), google.defaultScopes)
  })

  it('accepts a subset', () => {
    assert.equal(selectScopes(google, 'openid email'), 'openid email')
  })

  it('rejects a scope outside the allowlist', () => {
    assert.throws(
      () => selectScopes(google, 'openid https://www.googleapis.com/auth/gmail.send'),
      /scope not allowed/,
    )
  })
})
