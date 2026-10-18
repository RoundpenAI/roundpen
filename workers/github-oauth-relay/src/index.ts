import { authorizeURL, exchangeCode, githubProfile, pkcePair } from './github'
import { browserError } from './html'
import { nasCloudCallback, parseReturnOrigin } from './return-url'
import { verifyRp1 } from './rp1'
import { openState, sealState } from './state'
import { InstallDO, TicketDO } from './ticket-do'
import type { Env, StartBody, Ticket } from './types'

export { InstallDO, TicketDO }

const CALLBACK_PATH = '/github/callback'

export default {
  async fetch(req: Request, env: Env): Promise<Response> {
    try {
      return await route(req, env)
    } catch (err) {
      const msg = err instanceof Error ? err.message : 'internal error'
      const url = new URL(req.url)
      if (url.pathname === CALLBACK_PATH) {
        return browserError('GitHub login failed', msg)
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
  if (url.pathname === '/github/start' && (req.method === 'POST' || req.method === 'GET')) {
    return start(req, env)
  }
  if (req.method === 'GET' && url.pathname === CALLBACK_PATH) {
    return callback(req, env)
  }
  const take = /^\/tickets\/([^/]+)$/.exec(url.pathname)
  if (take && req.method === 'GET') {
    return takeTicket(req, env, take[1])
  }
  return json({ error: 'not found' }, 404)
}

async function start(req: Request, env: Env): Promise<Response> {
  requireSecrets(env)
  const inst = await verifyInstall(req, env)
  const body = await readStart(req)
  const origin = parseReturnOrigin(body.return)
  const state = (body.state || '').trim()
  if (!state || state.length > 256) {
    throw new Error('state is required (max 256 chars)')
  }

  const { verifier, challenge } = await pkcePair()
  const sealed = await sealState(env.STATE_SECRET, {
    v: 1,
    inst: inst.installId,
    ret: origin,
    st: state,
    exp: Math.floor(Date.now() / 1000) + 10 * 60,
    cv: verifier,
  })
  return json({
    authorizeUrl: authorizeURL({
      clientId: env.GITHUB_CLIENT_ID,
      redirectUri: publicCallback(env),
      state: sealed,
      scopes: env.GITHUB_SCOPES || 'read:user user:email',
      challenge,
    }),
  })
}

async function callback(req: Request, env: Env): Promise<Response> {
  requireSecrets(env)
  const url = new URL(req.url)
  const ghErr = url.searchParams.get('error')
  const rawState = url.searchParams.get('state') || ''
  let st: Awaited<ReturnType<typeof openState>> | null = null
  if (rawState) {
    try {
      st = await openState(env.STATE_SECRET, rawState, Math.floor(Date.now() / 1000))
    } catch {
      st = null
    }
  }
  if (ghErr) {
    if (st) {
      return redirect(
        nasCloudCallback(st.ret, { error: ghErr === 'access_denied' ? 'denied' : 'failed', state: st.st }),
      )
    }
    return browserError('GitHub login cancelled', 'The GitHub authorization was denied or failed.')
  }

  const code = url.searchParams.get('code') || ''
  if (!code || !st) {
    return browserError('GitHub login failed', 'Missing authorization code or state.')
  }

  const token = await exchangeCode({
    clientId: env.GITHUB_CLIENT_ID,
    clientSecret: env.GITHUB_CLIENT_SECRET,
    redirectUri: publicCallback(env),
    code,
    verifier: st.cv,
  })
  const profile = await githubProfile(token.access_token)
  const ticket: Ticket = {
    installId: st.inst,
    login: profile.login,
    name: profile.name,
    email: profile.email,
    accessToken: token.access_token,
    tokenType: token.token_type || 'bearer',
    scope: token.scope || '',
    refreshToken: token.refresh_token,
    expiresIn: token.expires_in,
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
    return browserError('GitHub login failed', 'Could not store the login ticket.')
  }

  return redirect(nasCloudCallback(st.ret, { ticket: ticketId, state: st.st }))
}

async function takeTicket(req: Request, env: Env, ticketId: string): Promise<Response> {
  if (!/^[0-9a-f]{32}$/.test(ticketId)) {
    return json({ error: 'ticket not found' }, 404)
  }
  const inst = await verifyInstall(req, env)
  const stub = env.TICKETS.get(env.TICKETS.idFromName(ticketId))
  const res = await stub.fetch(new Request('https://tickets/take', { method: 'POST' }))
  if (res.status === 404) {
    return json({ error: 'ticket not found' }, 404)
  }
  if (!res.ok) {
    return json({ error: 'ticket unavailable' }, 400)
  }
  const ticket = (await res.json()) as Ticket
  if (ticket.installId !== inst.installId) {
    return json({ error: 'ticket not found' }, 404)
  }
  return json({
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
  }
}

async function verifyInstall(req: Request, env: Env): Promise<{ installId: string }> {
  const token = bearer(req)
  const pub = (req.headers.get('X-Roundpen-Install-Pub') || '').trim()
  if (!token || !pub) {
    throw new Error('Authorization Bearer rp1 token and X-Roundpen-Install-Pub are required')
  }
  const parsed = await verifyRp1(token, pub, Math.floor(Date.now() / 1000))
  const tofu = await env.INSTALLS.get(env.INSTALLS.idFromName('installs')).fetch(
    new Request('https://installs/tofu', {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ installId: parsed.installId, publicKeyHex: pub }),
    }),
  )
  if (tofu.status === 403) {
    throw new Error('this install_id is already bound to a different public key')
  }
  if (!tofu.ok) {
    throw new Error('could not register install identity')
  }
  return { installId: parsed.installId }
}

function bearer(req: Request): string {
  const h = req.headers.get('Authorization') || ''
  const m = /^Bearer\s+(\S+)/i.exec(h)
  return m ? m[1] : ''
}

function publicCallback(env: Env): string {
  return `${env.PUBLIC_BASE.replace(/\/$/, '')}${CALLBACK_PATH}`
}

function requireSecrets(env: Env): void {
  if (!env.GITHUB_CLIENT_ID || !env.GITHUB_CLIENT_SECRET || !env.STATE_SECRET) {
    throw new Error('worker secrets GITHUB_CLIENT_ID, GITHUB_CLIENT_SECRET, STATE_SECRET are not set')
  }
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
