/** Browser capability checks for the Web Push API (no network). */

export type PushSupport = {
  serviceWorker: boolean
  pushManager: boolean
  notification: boolean
}

export function detectPushSupport(
  g: Pick<typeof globalThis, 'navigator' | 'Notification' | 'window'> = globalThis,
): PushSupport {
  const hasSW = typeof g.navigator !== 'undefined' && 'serviceWorker' in g.navigator
  const hasNotif = typeof g.Notification !== 'undefined'
  let pushManager = false
  if (hasSW && typeof g.window !== 'undefined') {
    pushManager = 'PushManager' in g.window
  }
  return { serviceWorker: hasSW, pushManager, notification: hasNotif }
}

/** True when this browser can subscribe to Web Push (SW + PushManager + Notification). */
export function isPushSupported(support: PushSupport = detectPushSupport()): boolean {
  return support.serviceWorker && support.pushManager && support.notification
}

export function urlBase64ToUint8Array(base64String: string): Uint8Array {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4)
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/')
  const raw = atob(base64)
  const out = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) {
    out[i] = raw.charCodeAt(i)
  }
  return out
}
