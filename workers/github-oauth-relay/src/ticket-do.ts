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
      const got = await this.ctx.storage.get<Stored>('t')
      await this.ctx.storage.deleteAll()
      try {
        await this.ctx.storage.deleteAlarm()
      } catch {
        /* no alarm */
      }
      if (!got || Date.now() > got.exp) {
        return json({ error: 'ticket not found' }, 404)
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

export class InstallDO {
  constructor(private readonly ctx: DurableObjectState) {}

  async fetch(req: Request): Promise<Response> {
    const url = new URL(req.url)
    if (req.method !== 'POST' || url.pathname !== '/tofu') {
      return json({ error: 'not found' }, 404)
    }
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
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'content-type': 'application/json' },
  })
}
