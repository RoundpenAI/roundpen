/** NAS console base. Browser will be sent back here; the worker never dials it.
 *  May include a path prefix (e.g. https://nas.example/app/roundpen) so fnOS
 *  unified-gateway routes land on the right micro-app. */

const PRIVATE_HOST = new Set(['localhost', 'localhost.'])

export function parseReturnOrigin(raw: string): string {
  let u: URL
  try {
    u = new URL(raw)
  } catch {
    throw new Error('return must be an absolute URL')
  }
  if (u.username || u.password) {
    throw new Error('return URL must not include credentials')
  }
  if (u.protocol !== 'http:' && u.protocol !== 'https:') {
    throw new Error('return URL must be http or https')
  }
  if (!u.hostname) {
    throw new Error('return URL needs a host')
  }
  if (u.protocol === 'http:' && !isLocalOrPrivateHost(u.hostname)) {
    throw new Error('http return is only allowed for localhost / LAN / .local hosts')
  }
  // fnOS unified gateway mounts each micro-app at /app/<name>/… — keep only
  // that prefix so /v1/... callbacks stay inside the right app. Any other path
  // (or :9527 direct console) collapses to origin.
  const app = /^(\/app\/[A-Za-z0-9._~-]+)/.exec(u.pathname || '')
  if (app) {
    return u.origin + app[1]
  }
  return u.origin
}

export function isLocalOrPrivateHost(hostname: string): boolean {
  const host = hostname.replace(/^\[|\]$/g, '').toLowerCase()
  if (PRIVATE_HOST.has(host) || host.endsWith('.localhost')) return true
  if (host.endsWith('.local') || host.endsWith('.lan') || host.endsWith('.home') || host.endsWith('.internal')) {
    return true
  }
  if (host === '::1' || host === '0:0:0:0:0:0:0:1') return true
  if (host.startsWith('fe80:')) return true
  if (host.startsWith('fc') || host.startsWith('fd')) return true

  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(host)
  if (!m) return false
  const oct = m.slice(1).map((s) => Number(s))
  if (oct.some((n) => n > 255)) return false
  const [a, b] = oct
  if (a === 10 || a === 127) return true
  if (a === 192 && b === 168) return true
  if (a === 172 && b >= 16 && b <= 31) return true
  if (a === 169 && b === 254) return true
  return false
}

const NAS_CALLBACKS = new Set([
  '/v1/auth/oauth/github/cloud/callback',
  '/v1/connectors/google/cloud/callback',
])

export function nasCloudCallback(base: string, callbackPath: string, query: Record<string, string>): string {
  if (!NAS_CALLBACKS.has(callbackPath)) {
    throw new Error('callback path is not registered')
  }
  // Join base (origin or origin+/app/roundpen) with an absolute callback path.
  // new URL('/v1/...', 'https://host/app/roundpen/') would drop the prefix —
  // so concatenate explicitly.
  const root = base.replace(/\/+$/, '')
  const u = new URL(root + callbackPath)
  for (const [k, v] of Object.entries(query)) {
    if (v) u.searchParams.set(k, v)
  }
  return u.toString()
}
