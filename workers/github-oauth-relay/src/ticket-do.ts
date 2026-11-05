import { decideTofu } from './tofu'
import type { Ticket } from './types'

type Stored = Ticket & { exp: number }

export class TicketDO {
  constructor(private readonly ctx: DurableObjectState) {}

  async fetch(req: Request): Promise<Response> {
    const url = new URL(req.url)
    if (req.method === 'PUT' && url.pathname === '/put') {
      const ttlSec = Number(url.searchParams.get('ttl') || '60')
      const ticket = (await req.json()) as Ticket
      const exp = Date.now() + Math.max(15, ttlSec) * 1000
      await this.ctx.storage.put<Stored>('t', { ...ticket, exp })
      await this.ctx.storage.setAlarm(exp)
      return json({ ok: true })
    }
    if (req.method === 'POST' && url.pathname === '/take') {
      const expectInstall = url.searchParams.get('install') || ''
      const expectState = url.searchParams.get('state') || ''
      const got = await this.ctx.storage.get<Stored>('t')
      if (!got || Date.now() > got.exp || !expectInstall || !expectState) {
        if (got && Date.now() > got.exp) {
          await this.ctx.storage.deleteAll()
        }
        return json({ error: 'ticket not found' }, 404)
      }
      if (got.installId !== expectInstall || got.nasState !== expectState) {
        return json({ error: 'ticket not found' }, 404)
      }
      await this.ctx.storage.deleteAll()
      try {
        await this.ctx.storage.deleteAlarm()
      } catch {
        /* no alarm */
      }
      const { exp: _exp, ...ticket } = got
      return json(ticket)
    }
    return json({ error: 'not found' }, 404)
  }

  async alarm(): Promise<void> {
    await this.ctx.storage.deleteAll()
  }
}

type RefreshBinding = { installId: string; provider: string }

export class InstallDO {
  constructor(private readonly ctx: DurableObjectState) {}

  async alarm(): Promise<void> {
    // Drop expired rate / registration counters; keep TOFU + refresh bindings.
    const all = await this.ctx.storage.list<number>({ prefix: 'rl:' })
    const nowHour = new Date().toISOString().slice(0, 13)
    const toDelete: string[] = []
    for (const [k] of all) {
      const hour = k.slice(k.lastIndexOf(':') + 1)
      if (hour < nowHour) toDelete.push(k)
    }
    const regs = await this.ctx.storage.list<number>({ prefix: 'regip:' })
    const today = new Date().toISOString().slice(0, 10)
    for (const [k] of regs) {
      const day = k.slice(k.lastIndexOf(':') + 1)
      if (day < today) toDelete.push(k)
    }
    if (toDelete.length) await this.ctx.storage.delete(toDelete)
  }

  async fetch(req: Request): Promise<Response> {
    const url = new URL(req.url)
    if (req.method === 'POST' && url.pathname === '/tofu') {
      return this.tofu(req)
    }
    if (req.method === 'POST' && url.pathname === '/reset-tofu') {
      return this.resetTofu(req)
    }
    if (req.method === 'POST' && url.pathname === '/rate') {
      return this.rate(req)
    }
    if (req.method === 'POST' && (url.pathname === '/bind' || url.pathname === '/check' || url.pathname === '/unbind')) {
      return this.refresh(req, url.pathname)
    }
    return json({ error: 'not found' }, 404)
  }

  private async tofu(req: Request): Promise<Response> {
    const body = (await req.json()) as {
      installId?: string
      publicKeyHex?: string
      clientIP?: string
      registerPerIPPerDay?: number
    }
    const installId = (body.installId || '').trim()
    const publicKeyHex = (body.publicKeyHex || '').trim().toLowerCase()
    if (!installId || !publicKeyHex) {
      return json({ error: 'installId and publicKeyHex required' }, 400)
    }
    const key = `pub:${installId}`
    const countKey = `reinstalls:${installId}`
    const existing = await this.ctx.storage.get<string>(key)
    const reinstallCount = (await this.ctx.storage.get<number>(countKey)) || 0
    const decision = decideTofu(existing, publicKeyHex, reinstallCount)
    if (decision.kind === 'register') {
      const ipLimit = Number(body.registerPerIPPerDay || 0)
      const ip = (body.clientIP || '').trim()
      if (ipLimit > 0 && ip) {
        const day = new Date().toISOString().slice(0, 10)
        const ipKey = `regip:${ip}:${day}`
        const n = (await this.ctx.storage.get<number>(ipKey)) || 0
        if (n >= ipLimit) {
          return json({ error: 'too many new installs from this IP today' }, 429)
        }
        await this.ctx.storage.put(ipKey, n + 1)
      }
      await this.ctx.storage.put(key, publicKeyHex)
      return json({ ok: true, firstSeen: true, reinstallCount })
    }
    if (decision.kind === 'match') {
      return json({ ok: true, firstSeen: false, reinstallCount: decision.reinstallCount })
    }
    // Same install_id, new Ed25519 key → NAS reinstall/wipe. Accept and count.
    await this.ctx.storage.put(key, publicKeyHex)
    await this.ctx.storage.put(countKey, decision.reinstallCount)
    return json({
      ok: true,
      firstSeen: false,
      rotated: true,
      reinstallCount: decision.reinstallCount,
    })
  }

  /** Sliding hour bucket: {installId, limit} → allow or 429. */
  private async rate(req: Request): Promise<Response> {
    const body = (await req.json().catch(() => null)) as {
      installId?: string
      limit?: number
    } | null
    const installId = (body?.installId || '').trim()
    const limit = Number(body?.limit || 0)
    if (!installId || limit <= 0) {
      return json({ error: 'installId and positive limit required' }, 400)
    }
    const hour = new Date().toISOString().slice(0, 13) // YYYY-MM-DDTHH
    const key = `rl:${installId}:${hour}`
    const n = (await this.ctx.storage.get<number>(key)) || 0
    if (n >= limit) {
      return json({ error: 'free quota exceeded', used: n, limit }, 429)
    }
    await this.ctx.storage.put(key, n + 1)
    // Expire roughly after the hour window (+skew).
    try {
      await this.ctx.storage.setAlarm(Date.now() + 2 * 60 * 60 * 1000)
    } catch {
      /* alarm optional */
    }
    return json({ ok: true, used: n + 1, limit })
  }

  /** Maintainer: drop stored pubkey so the next request re-registers (reinstall count kept). */
  private async resetTofu(req: Request): Promise<Response> {
    const body = (await req.json().catch(() => null)) as { installId?: string } | null
    const installId = (body?.installId || '').trim()
    if (!installId) {
      return json({ error: 'installId is required' }, 400)
    }
    const key = `pub:${installId}`
    const countKey = `reinstalls:${installId}`
    const existed = (await this.ctx.storage.get<string>(key)) != null
    const reinstallCount = (await this.ctx.storage.get<number>(countKey)) || 0
    await this.ctx.storage.delete(key)
    return json({ ok: true, deleted: existed, reinstallCount })
  }

  private async refresh(req: Request, pathname: string): Promise<Response> {
    const body = (await req.json().catch(() => null)) as {
      hash?: string
      installId?: string
      provider?: string
    } | null
    const hash = (body?.hash || '').trim().toLowerCase()
    const installId = (body?.installId || '').trim()
    const provider = (body?.provider || '').trim()
    if (!/^[0-9a-f]{64}$/.test(hash) || !installId || !provider) {
      return json({ error: 'hash, installId and provider are required' }, 400)
    }
    const key = `rt:${hash}`
    const existing = await this.ctx.storage.get<RefreshBinding>(key)
    if (pathname === '/bind') {
      if (existing && (existing.installId !== installId || existing.provider !== provider)) {
        return json({ error: 'refresh token is bound to another install' }, 403)
      }
      await this.ctx.storage.put<RefreshBinding>(key, { installId, provider })
      return json({ ok: true })
    }
    if (pathname === '/check') {
      if (!existing || existing.installId !== installId || existing.provider !== provider) {
        return json({ error: 'refresh token not found' }, 404)
      }
      return json({ ok: true })
    }
    if (existing && existing.installId === installId && existing.provider === provider) {
      await this.ctx.storage.delete(key)
    }
    return json({ ok: true })
  }
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}
