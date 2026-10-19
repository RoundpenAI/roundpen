import { browserError } from './html'
import { pkcePair } from './github'
import {
  providerFromCallback,
  providerFromRefresh,
  providerFromStart,
  resolveProvider,
  selectScopes,
  type Provider,
} from './providers'
import { nasCloudCallback, parseReturnOrigin } from './return-url'
import { verifyRp1 } from './rp1'
import { openState, sealState } from './state'
import { InstallDO, TicketDO } from './ticket-do'
import type { Env, StartBody, Ticket } from './types'

export { InstallDO, TicketDO }

export default {
  async fetch(req: Request, env: Env): Promise<Response> {
    try {
      return await route(req, env)
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'internal error'
      const url = new URL(req.url)
      if (providerFromCallback(url.pathname)) {
        return browserError('Authorization failed', msg)
      }
      return json({ error: msg }, 400)
    }
  },
}

async function route(req: Request, env: Env): Promise<Response> {
  const url = new URL(req.url)
  if (req.method === 'GET' && url.pathname === '/health') {
    return json({ ok: true })
  }
  const startId = providerFromStart(url.pathname)
  if (startId && (req.method === 'POST' || req.method === 'GET')) {
    return start(req, env, startId)
  }
  const callbackId = providerFromCallback(url.pathname)
  if (callbackId && req.method === 'GET') {
    return callback(req, env, callbackId)
  }
  const refreshId = providerFromRefresh(url.pathname)
  if (refreshId && req.method === 'POST') {
    return refresh(req, env, refreshId)
  }
  const take = /^\/tickets\/([^/]+)$/.exec(url.pathname)
  if (take && req.method === 'GET') {
    return takeTicket(req, env, take[1])
  }
  return json({ error: 'not found' }, 404)
}

async function start(req: Request, env: Env, providerId: string): Promise<Response> {
  const provider = loadProvider(env, providerId)
  const inst = await verifyInstall(req, env)
  const body = await readStart(req)
  const origin = parseReturnOrigin(body.return)
  const state = (body.state || '').trim()
  if (!state || state.length > 256) {
    throw new Error('state is required (max 256 chars)')
  }
  const scopes = selectScopes(provider, body.scopes || '')

  const { verifier, challenge } = await pkcePair()
  const sealed = await sealState(env.STATE_SECRET, {
    v: 1,
    inst: inst.installId,
    ret: origin,
    st: state,
    exp: Math.floor(Date.now() / 1000) + 10 * 60,
    cv: verifier,
    pv: provider.id,
    cb: provider.nasCallbackPath,
  })
  return json({
    authorizeUrl: provider.authorize({
      clientId: provider.clientId,
      redirectUri: publicCallback(env, provider),
      state: sealed,
      scopes,
      challenge,
    }),
  })
}

async function callback(req: Request, env: Env, providerId: string): Promise<Response> {
  const provider = loadProvider(env, providerId)
  const url = new URL(req.url)
  const upstreamErr = url.searchParams.get('error')
  const rawState = url.searchParams.get('state') || ''
  let st: Awaited<ReturnType<typeof openState>> | null = null
  if (rawState) {
    try {
      st = await openState(env.STATE_SECRET, rawState, Math.floor(Date.now() / 1000))
    } catch {
      st = null
    }
  }
  if (st && st.pv !== provider.id) {
    return browserError('Authorization failed', 'This callback does not match the authorization that was started.')
  }
  if (upstreamErr) {
    if (st) {
      return redirect(
        nasCloudCallback(st.ret, st.cb || provider.nasCallbackPath, {
          error: upstreamErr === 'access_denied' ? 'denied' : 'failed',
          state: st.st,
        }),
      )
    }
    return browserError('Authorization cancelled', 'The authorization was denied or failed.')
  }

  const code = url.searchParams.get('code') || ''
  if (!code || !st) {
    return browserError('Authorization failed', 'Missing authorization code or state.')
  }

  const token = await provider.exchange({
    clientId: provider.clientId,
    clientSecret: provider.clientSecret,
    redirectUri: publicCallback(env, provider),
    code,
    verifier: st.cv,
  })
  const profile = await provider.profile(token.access_token)
  const ticket: Ticket = {
    installId: st.inst,
    nasState: st.st,
    provider: provider.id,
    subject: profile.subject,
    login: profile.login,
    name: profile.name,
    email: profile.email,
    accessToken: token.access_token,
    tokenType: token.token_type || 'bearer',
    scope: token.scope || '',
    refreshToken: token.refresh_token,
    expiresIn: token.expires_in,
  }

  if (ticket.refreshToken) {
    const bound = await bindRefresh(env, ticket.refreshToken, ticket.installId, provider.id)
    if (!bound.ok) {
      return browserError('Authorization failed', 'Could not bind the refresh token to this install.')
    }
  }

  const ticketId = randomId()
  const ttl = Number(env.TICKET_TTL_SECONDS || '60')
  const stub = env.TICKETS.get(env.TICKETS.idFromName(ticketId))
  const put = await stub.fetch(
    new Request(`https://tickets/put?ttl=${encodeURIComponent(String(ttl))}`, {
      method: 'PUT',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(ticket),
    }),
  )
  if (!put.ok) {
    return browserError('Authorization failed', 'Could not store the authorization ticket.')
  }

  return redirect(nasCloudCallback(st.ret, st.cb || provider.nasCallbackPath, { ticket: ticketId, state: st.st }))
}

async function refresh(req: Request, env: Env, providerId: string): Promise<Response> {
  const provider = loadProvider(env, providerId)
  const inst = await verifyInstall(req, env)
  const raw = await req.json().catch(() => null)
  const refreshToken =
    raw && typeof raw === 'object' ? String((raw as { refreshToken?: unknown }).refreshToken || '') : ''
  if (!refreshToken) {
    return json({ error: 'refreshToken is required' }, 400)
  }
  const hash = await tokenHash(refreshToken)
  const check = await installFetch(env, '/check', { hash, installId: inst.installId, provider: provider.id })
  if (check.status === 404) {
    return json({ error: 'refresh token not found' }, 404)
  }
  if (!check.ok) {
    return json({ error: 'refresh token unavailable' }, check.status === 403 ? 403 : 400)
  }
  const token = await provider.refresh({
    clientId: provider.clientId,
    clientSecret: provider.clientSecret,
    refreshToken,
  })
  const rotated = token.refresh_token && token.refresh_token !== refreshToken ? token.refresh_token : ''
  if (rotated) {
    const bound = await bindRefresh(env, rotated, inst.installId, provider.id)
    if (!bound.ok) {
      return json({ error: 'could not bind the rotated refresh token' }, 400)
    }
    await installFetch(env, '/unbind', { hash, installId: inst.installId, provider: provider.id })
  }
  return json({
    accessToken: token.access_token,
    tokenType: token.token_type || 'bearer',
    scope: token.scope || '',
    refreshToken: rotated,
    expiresIn: token.expires_in,
  })
}

async function takeTicket(req: Request, env: Env, ticketId: string): Promise<Response> {
  if (!/^[0-9a-f]{32}$/.test(ticketId)) {
    return json({ error: 'ticket not found' }, 404)
  }
  const inst = await verifyInstall(req, env)
  const url = new URL(req.url)
  const state = (url.searchParams.get('state') || '').trim()
  if (!state) {
    return json({ error: 'ticket not found' }, 404)
  }
  const stub = env.TICKETS.get(env.TICKETS.idFromName(ticketId))
  const takeURL = `https://tickets/take?install=${encodeURIComponent(inst.installId)}&state=${encodeURIComponent(state)}`
  const res = await stub.fetch(new Request(takeURL, { method: 'POST' }))
  if (res.status === 404) {
    return json({ error: 'ticket not found' }, 404)
  }
  if (!res.ok) {
    return json({ error: 'ticket unavailable' }, 400)
  }
  const ticket = (await res.json()) as Ticket
  return json({
    provider: ticket.provider,
    subject: ticket.subject,
    login: ticket.login,
    name: ticket.name,
    email: ticket.email,
    accessToken: ticket.accessToken,
    tokenType: ticket.tokenType,
    scope: ticket.scope,
    refreshToken: ticket.refreshToken,
    expiresIn: ticket.expiresIn,
  })
}

async function readStart(req: Request): Promise<StartBody> {
  if (req.method === 'GET') {
    const url = new URL(req.url)
    return {
      return: url.searchParams.get('return') || '',
      state: url.searchParams.get('state') || '',
      scopes: url.searchParams.get('scopes') || '',
    }
  }
  const raw = await req.json().catch(() => null)
  if (!raw || typeof raw !== 'object') {
    throw new Error('JSON body required')
  }
  const body = raw as Record<string, unknown>
  return {
    return: String(body.return || ''),
    state: String(body.state || ''),
    scopes: String(body.scopes || ''),
  }
}

async function verifyInstall(req: Request, env: Env): Promise<{ installId: string }> {
  const token = bearer(req)
  const pub = (req.headers.get('X-Roundpen-Install-Pub') || '').trim()
  if (!token || !pub) {
    throw new Error('Authorization Bearer rp1 token and X-Roundpen-Install-Pub are required')
  }
  const parsed = await verifyRp1(token, pub, Math.floor(Date.now() / 1000))
  const tofu = await installFetch(env, '/tofu', { installId: parsed.installId, publicKeyHex: pub })
  if (tofu.status === 403) {
    throw new Error('this install_id is already bound to a different public key')
  }
  if (!tofu.ok) {
    throw new Error('could not register install identity')
  }
  return { installId: parsed.installId }
}

async function bindRefresh(
  env: Env,
  refreshToken: string,
  installId: string,
  provider: string,
): Promise<{ ok: boolean }> {
  const hash = await tokenHash(refreshToken)
  const res = await installFetch(env, '/bind', { hash, installId, provider })
  return { ok: res.ok }
}

function installStub(env: Env): DurableObjectStub {
  return env.INSTALLS.get(env.INSTALLS.idFromName('installs'))
}

function installFetch(env: Env, pathname: string, body: unknown): Promise<Response> {
  return installStub(env).fetch(
    new Request(`https://installs${pathname}`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
    }),
  )
}

async function tokenHash(token: string): Promise<string> {
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(token))
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, '0')).join('')
}

function loadProvider(env: Env, id: string): Provider {
  const provider = resolveProvider(id, env)
  if (!provider) {
    throw new Error('unknown provider')
  }
  if (!provider.clientId || !provider.clientSecret || !env.STATE_SECRET) {
    throw new Error(`worker secrets for ${provider.id} and STATE_SECRET are not set`)
  }
  return provider
}

function bearer(req: Request): string {
  const h = req.headers.get('Authorization') || ''
  const m = /^Bearer\s+(\S+)/i.exec(h)
  return m ? m[1] : ''
}

function publicCallback(env: Env, provider: Provider): string {
  return `${env.PUBLIC_BASE.replace(/\/$/, '')}${provider.workerCallbackPath}`
}

function randomId(): string {
  const b = new Uint8Array(16)
  crypto.getRandomValues(b)
  return [...b].map((x) => x.toString(16).padStart(2, '0')).join('')
}

function redirect(to: string): Response {
  return new Response(null, { status: 302, headers: { location: to } })
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json', 'cache-control': 'no-store' },
  })
}
