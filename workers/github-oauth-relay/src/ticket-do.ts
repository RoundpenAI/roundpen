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

  async fetch(req: Request): Promise<Response> {
    const url = new URL(req.url)
    if (req.method === 'POST' && url.pathname === '/tofu') {
      return this.tofu(req)
    }
    if (req.method === 'POST' && (url.pathname === '/bind' || url.pathname === '/check' || url.pathname === '/unbind')) {
      return this.refresh(req, url.pathname)
    }
    return json({ error: 'not found' }, 404)
  }

  private async tofu(req: Request): Promise<Response> {
    const body = (await req.json()) as { installId?: string; publicKeyHex?: string }
    const installId = (body.installId || '').trim()
    const publicKeyHex = (body.publicKeyHex || '').trim().toLowerCase()
    if (!installId || !publicKeyHex) {
      return json({ error: 'installId and publicKeyHex required' }, 400)
    }
    const key = `pub:${installId}`
    const existing = await this.ctx.storage.get<string>(key)
    if (!existing) {
      await this.ctx.storage.put(key, publicKeyHex)
      return json({ ok: true, firstSeen: true })
    }
    if (existing !== publicKeyHex) {
      return json({ error: 'install public key does not match TOFU record' }, 403)
    }
    return json({ ok: true, firstSeen: false })
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
