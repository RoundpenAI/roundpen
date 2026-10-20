import type { OAuthState } from './types'
import { b64urlDecode, b64urlEncode } from './rp1'

export async function sealState(secret: string, payload: OAuthState): Promise<string> {
  const body = b64urlEncode(new TextEncoder().encode(JSON.stringify(payload)))
  const sig = await hmac(secret, body)
  return `${body}.${sig}`
}

export async function openState(secret: string, raw: string, nowUnix: number): Promise<OAuthState> {
  const dot = raw.lastIndexOf('.')
  if (dot <= 0) throw new Error('oauth state is malformed')
  const body = raw.slice(0, dot)
  const sig = raw.slice(dot + 1)
  const expect = await hmac(secret, body)
  if (!timingSafeEqual(sig, expect)) {
    throw new Error('oauth state is invalid')
  }
  let parsed: OAuthState
  try {
    parsed = JSON.parse(new TextDecoder().decode(b64urlDecode(body))) as OAuthState
  } catch {
    throw new Error('oauth state is malformed')
  }
  if (parsed.v !== 1 || !parsed.inst || !parsed.ret || !parsed.cv) {
    throw new Error('oauth state is malformed')
  }
  if (!parsed.pv) parsed.pv = 'github'
  if (!parsed.cb) parsed.cb = '/v1/auth/oauth/github/cloud/callback'
  if (nowUnix > parsed.exp) {
    throw new Error('oauth state expired')
  }
  return parsed
}

async function hmac(secret: string, body: string): Promise<string> {
  const key = await crypto.subtle.importKey(
    'raw',
    new TextEncoder().encode(secret),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign'],
  )
  const sig = await crypto.subtle.sign('HMAC', key, new TextEncoder().encode(body))
  return b64urlEncode(sig)
}

function timingSafeEqual(a: string, b: string): boolean {
  if (a.length !== b.length) return false
  let out = 0
  for (let i = 0; i < a.length; i++) out |= a.charCodeAt(i) ^ b.charCodeAt(i)
  return out === 0
}
