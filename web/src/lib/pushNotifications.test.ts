import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { detectPushSupport, urlBase64ToUint8Array } from './pushNotifications.ts'

describe('urlBase64ToUint8Array', () => {
  it('decodes padded URL-safe base64', () => {
    // "hi" in standard base64 is aGk=
    const bytes = urlBase64ToUint8Array('aGk')
    assert.deepEqual(Array.from(bytes), [104, 105])
  })

  it('accepts URL-safe alphabet', () => {
    // "-_" are the URL-safe replacements for "+/"
    const fromURL = urlBase64ToUint8Array('-_8')
    const fromStd = urlBase64ToUint8Array('+/8=')
    assert.deepEqual(Array.from(fromURL), Array.from(fromStd))
  })
})

describe('detectPushSupport', () => {
  it('reports missing APIs when globals are absent', () => {
    const got = detectPushSupport({
      navigator: {} as Navigator,
      Notification: undefined as unknown as typeof Notification,
      window: {} as Window & typeof globalThis,
    })
    assert.deepEqual(got, {
      serviceWorker: false,
      pushManager: false,
      notification: false,
    })
  })

  it('detects serviceWorker + PushManager + Notification', () => {
    const got = detectPushSupport({
      navigator: { serviceWorker: {} } as Navigator,
      Notification: function Notification() {} as unknown as typeof Notification,
      window: { PushManager: function PushManager() {} } as unknown as Window &
        typeof globalThis,
    })
    assert.equal(got.serviceWorker, true)
    assert.equal(got.pushManager, true)
    assert.equal(got.notification, true)
  })
})
