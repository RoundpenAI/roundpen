import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { parseFnosClientAction } from './fnosHost.ts'

describe('parseFnosClientAction', () => {
  it('reads nested _clientAction from JSON string', () => {
    const got = parseFnosClientAction(
      JSON.stringify({
        ok: true,
        path: '/vol1/a.mp4',
        _clientAction: { kind: 'fnos.open', action: 'open', path: '/vol1/a.mp4' },
      }),
    )
    assert.deepEqual(got, {
      kind: 'fnos.open',
      action: 'open',
      path: '/vol1/a.mp4',
    })
  })

  it('normalizes manager action', () => {
    const got = parseFnosClientAction({
      _clientAction: { kind: 'fnos.open', action: 'manager', path: '/vol1/x' },
    })
    assert.equal(got?.action, 'manager')
  })

  it('returns null when missing', () => {
    assert.equal(parseFnosClientAction({ ok: true }), null)
    assert.equal(parseFnosClientAction('not-json'), null)
  })
})
