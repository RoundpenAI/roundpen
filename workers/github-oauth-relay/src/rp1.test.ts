import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { parseRp1, signMessage, verifyRp1 } from './rp1.ts'

const installId = '11111111-2222-3333-4444-555555555555'
const exp = 1893456000
const pub = '207a067892821e25d770f1fba0c47c11ff4b813e54162ece9eb839e076231ab6'
const token =
  'rp1.11111111-2222-3333-4444-555555555555.1893456000.xN_w9bG-00T3TQX39B40w1DMmLkn0WEid4wGY5tSeCYwhd0wQR_6YUniaX1qkQEBPL-YK6-jrRKFroqxLzS8CA'

describe('rp1', () => {
  it('matches the installid test vector', () => {
    const msg = new TextDecoder().decode(signMessage(installId, exp))
    assert.equal(msg, 'rp1|11111111-2222-3333-4444-555555555555|1893456000')
    const parsed = parseRp1(token)
    assert.equal(parsed.installId, installId)
    assert.equal(parsed.expUnix, exp)
  })

  it('verifies the locked token', async () => {
    const got = await verifyRp1(token, pub, exp - 1)
    assert.equal(got.installId, installId)
  })

  it('rejects expiry', async () => {
    await assert.rejects(() => verifyRp1(token, pub, exp + 1), /expired/)
  })
})
