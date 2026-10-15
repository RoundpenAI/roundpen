/* Roundpen PWA service worker — Web Push + notification click routing. */

self.addEventListener('install', (event) => {
  event.waitUntil(self.skipWaiting())
})

self.addEventListener('activate', (event) => {
  event.waitUntil(self.clients.claim())
})

self.addEventListener('push', (event) => {
  event.waitUntil(handlePush(event))
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const url = (event.notification.data && event.notification.data.url) || '/a'
  event.waitUntil(openOrFocus(url))
})

async function handlePush(event) {
  let data = { type: 'turn_done', title: 'Roundpen', body: '', url: '/a' }
  try {
    if (event.data) {
      data = { ...data, ...event.data.json() }
    }
  } catch {
    /* ignore malformed payloads */
  }

  const clients = await self.clients.matchAll({
    type: 'window',
    includeUncontrolled: true,
  })
  const focused = clients.some((c) => c.focused)
  // Skip turn_done while the user is looking at the console; still show
  // permission asks (they may be on another page).
  if (focused && data.type === 'turn_done') {
    return
  }

  const title = data.title || 'Roundpen'
  const opts = {
    body: data.body || '',
    icon: '/icon-192.png',
    badge: '/icon-192.png',
    data: { url: data.url || '/a', type: data.type || '' },
    tag: data.type === 'permission' ? 'rp-permission' : 'rp-turn-done',
    renotify: true,
  }
  await self.registration.showNotification(title, opts)
}

async function openOrFocus(url) {
  const abs = new URL(url, self.location.origin).href
  const clients = await self.clients.matchAll({
    type: 'window',
    includeUncontrolled: true,
  })
  for (const client of clients) {
    if ('focus' in client) {
      await client.focus()
      if ('navigate' in client && client.url !== abs) {
        try {
          await client.navigate(abs)
        } catch {
          /* navigate may be unsupported */
        }
      }
      return
    }
  }
  if (self.clients.openWindow) {
    await self.clients.openWindow(abs)
  }
}
