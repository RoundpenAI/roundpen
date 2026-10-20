import type { Profile, TokenResult } from './types'

const AUTH_URL = 'https://accounts.google.com/o/oauth2/v2/auth'
const TOKEN_URL = 'https://oauth2.googleapis.com/token'
const USERINFO_URL = 'https://openidconnect.googleapis.com/v1/userinfo'

export function authorizeURL(opts: {
  clientId: string
  redirectUri: string
  state: string
  scopes: string
  challenge: string
}): string {
  const u = new URL(AUTH_URL)
  u.searchParams.set('client_id', opts.clientId)
  u.searchParams.set('redirect_uri', opts.redirectUri)
  u.searchParams.set('response_type', 'code')
  u.searchParams.set('scope', opts.scopes)
  u.searchParams.set('state', opts.state)
  u.searchParams.set('code_challenge', opts.challenge)
  u.searchParams.set('code_challenge_method', 'S256')
  u.searchParams.set('access_type', 'offline')
  u.searchParams.set('prompt', 'consent')
  return u.toString()
}

export async function exchangeCode(opts: {
  clientId: string
  clientSecret: string
  redirectUri: string
  code: string
  verifier: string
}): Promise<TokenResult> {
  const form = new URLSearchParams({
    grant_type: 'authorization_code',
    code: opts.code,
    redirect_uri: opts.redirectUri,
    client_id: opts.clientId,
    client_secret: opts.clientSecret,
    code_verifier: opts.verifier,
  })
  return tokenRequest(form)
}

export async function refreshToken(opts: {
  clientId: string
  clientSecret: string
  refreshToken: string
}): Promise<TokenResult> {
  const form = new URLSearchParams({
    grant_type: 'refresh_token',
    refresh_token: opts.refreshToken,
    client_id: opts.clientId,
    client_secret: opts.clientSecret,
  })
  return tokenRequest(form)
}

export async function googleProfile(accessToken: string): Promise<Profile> {
  const res = await fetch(USERINFO_URL, {
    headers: { Authorization: `Bearer ${accessToken}` },
  })
  if (!res.ok) {
    throw new Error(`google userinfo HTTP ${res.status}`)
  }
  const body = (await res.json()) as { sub?: string; email?: string; name?: string }
  const subject = (body.sub || '').trim()
  if (!subject) throw new Error('google profile missing sub')
  const email = (body.email || '').trim()
  return {
    subject,
    login: email,
    name: (body.name || '').trim(),
    email,
  }
}

async function tokenRequest(form: URLSearchParams): Promise<TokenResult> {
  const res = await fetch(TOKEN_URL, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded', Accept: 'application/json' },
    body: form,
  })
  const body = (await res.json().catch(() => ({}))) as {
    error?: string
    error_description?: string
    access_token?: string
    token_type?: string
    scope?: string
    refresh_token?: string
    expires_in?: number
  }
  if (!res.ok || !body.access_token) {
    throw new Error(body.error_description || body.error || `google token HTTP ${res.status}`)
  }
  return {
    access_token: body.access_token,
    token_type: body.token_type,
    scope: body.scope,
    refresh_token: body.refresh_token,
    expires_in: body.expires_in,
  }
}
