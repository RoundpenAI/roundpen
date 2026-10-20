/** Browser helpers for Web Push subscription against /v1/me/push/*. */

import { apiUrl } from '../api/client'
import { isPushSupported, urlBase64ToUint8Array } from './pushSupport'

export type { PushSupport } from './pushSupport'
export { detectPushSupport, isPushSupported, urlBase64ToUint8Array } from './pushSupport'

export type PushStatus = {
  configured: boolean
  subscribed: boolean
}

export async function fetchPushStatus(): Promise<PushStatus> {
  const res = await fetch(apiUrl('/v1/me/push/status'), {
    credentials: 'include',
  })
  if (!res.ok) {
    throw new Error(`push status ${res.status}`)
  }
  return (await res.json()) as PushStatus
}

export async function fetchVapidPublicKey(): Promise<string> {
  const res = await fetch(apiUrl('/v1/me/push/vapid-key'), {
    credentials: 'include',
  })
  if (!res.ok) {
    throw new Error(`vapid key ${res.status}`)
  }
  const body = (await res.json()) as { publicKey?: string }
  if (!body.publicKey) throw new Error('missing publicKey')
  return body.publicKey
}

export async function registerServiceWorker(
  path = apiUrl('/sw.js'),
): Promise<ServiceWorkerRegistration | null> {
  if (!('serviceWorker' in navigator)) return null
  return navigator.serviceWorker.register(path)
}

export async function enablePushNotifications(): Promise<void> {
  if (!isPushSupported()) {
    throw new Error('此浏览器不支持系统通知')
  }
  const perm = await Notification.requestPermission()
  if (perm !== 'granted') {
    throw new Error('通知权限未授予')
  }
  const reg = await registerServiceWorker()
  if (!reg) throw new Error('Service Worker 注册失败')
  await navigator.serviceWorker.ready
  const publicKey = await fetchVapidPublicKey()
  const sub = await reg.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(publicKey) as BufferSource,
  })
  const json = sub.toJSON()
  const res = await fetch(apiUrl('/v1/me/push/subscriptions'), {
    method: 'POST',
    credentials: 'include',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      endpoint: json.endpoint,
      keys: { p256dh: json.keys?.p256dh, auth: json.keys?.auth },
    }),
  })
  if (!res.ok) {
    throw new Error(`订阅失败 ${res.status}`)
  }
}

export async function disablePushNotifications(): Promise<void> {
  if (!('serviceWorker' in navigator)) return
  const reg = await navigator.serviceWorker.getRegistration()
  const sub = await reg?.pushManager.getSubscription()
  if (sub) {
    const endpoint = sub.endpoint
    await sub.unsubscribe().catch(() => undefined)
    await fetch(apiUrl('/v1/me/push/subscriptions'), {
      method: 'DELETE',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ endpoint }),
    }).catch(() => undefined)
  }
}
