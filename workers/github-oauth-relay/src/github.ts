import type { Profile, TokenResult } from './types'

export function authorizeURL(opts: {
  clientId: string
  redirectUri: string
  state: string
  scopes: string
  challenge: string
}): string {
  const u = new URL('https://github.com/login/oauth/authorize')
  u.searchParams.set('client_id', opts.clientId)
  u.searchParams.set('redirect_uri', opts.redirectUri)
  u.searchParams.set('state', opts.state)
  u.searchParams.set('response_type', 'code')
  if (opts.scopes.trim()) u.searchParams.set('scope', opts.scopes.trim())
  u.searchParams.set('code_challenge', opts.challenge)
  u.searchParams.set('code_challenge_method', 'S256')
  return u.toString()
}

export async function exchangeCode(opts: {
  clientId: string
  clientSecret: string
  redirectUri: string
  code: string
  verifier: string
}): Promise<TokenResult> {
  const res = await fetch('https://github.com/login/oauth/access_token', {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      client_id: opts.clientId,
      client_secret: opts.clientSecret,
      code: opts.code,
      redirect_uri: opts.redirectUri,
      code_verifier: opts.verifier,
    }),
  })
  const body = (await res.json()) as {
    error?: string
    error_description?: string
    access_token?: string
    token_type?: string
    scope?: string
    refresh_token?: string
    expires_in?: number
  }
  if (!res.ok || !body.access_token) {
    throw new Error(body.error_description || body.error || `github token exchange HTTP ${res.status}`)
  }
  return {
    access_token: body.access_token,
    token_type: body.token_type,
    scope: body.scope,
    refresh_token: body.refresh_token,
    expires_in: body.expires_in,
  }
}

export async function refreshToken(opts: {
  clientId: string
  clientSecret: string
  refreshToken: string
}): Promise<TokenResult> {
  const res = await fetch('https://github.com/login/oauth/access_token', {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      grant_type: 'refresh_token',
      client_id: opts.clientId,
      client_secret: opts.clientSecret,
      refresh_token: opts.refreshToken,
    }),
  })
  const body = (await res.json()) as {
    error?: string
    error_description?: string
    access_token?: string
    token_type?: string
    scope?: string
    refresh_token?: string
    expires_in?: number
  }
  if (!res.ok || !body.access_token) {
    throw new Error(body.error_description || body.error || `github token refresh HTTP ${res.status}`)
  }
  return {
    access_token: body.access_token,
    token_type: body.token_type,
    scope: body.scope,
    refresh_token: body.refresh_token,
    expires_in: body.expires_in,
  }
}

export async function githubProfile(accessToken: string): Promise<Profile> {
  const me = await ghJSON<{ login?: string; name?: string; email?: string }>(accessToken, 'https://api.github.com/user')
  const login = (me.login || '').trim()
  if (!login) throw new Error('github profile missing login')

  let email = (me.email || '').trim()
  if (!email) {
    // Missing user:email scope (or a private-email 403) must not fail auth —
    // same soft-fail as the Go oauth client.
    try {
      const emails = await ghJSON<Array<{ email?: string; primary?: boolean; verified?: boolean }>>(
        accessToken,
        'https://api.github.com/user/emails',
      )
      const verified = emails.filter((e) => e.verified && e.email)
      email = (verified.find((e) => e.primary)?.email || verified[0]?.email || '').trim()
    } catch {
      email = ''
    }
  }
  return { subject: login, login, name: (me.name || '').trim(), email }
}

async function ghJSON<T>(accessToken: string, url: string): Promise<T> {
  const res = await fetch(url, {
    headers: {
      Accept: 'application/vnd.github+json',
      Authorization: `Bearer ${accessToken}`,
      'User-Agent': 'roundpen-github-oauth-relay',
      'X-GitHub-Api-Version': '2022-11-28',
    },
  })
  if (!res.ok) {
    throw new Error(`github api ${url} HTTP ${res.status}`)
  }
  return (await res.json()) as T
}

export async function pkcePair(): Promise<{ verifier: string; challenge: string }> {
  const raw = new Uint8Array(32)
  crypto.getRandomValues(raw)
  const verifier = b64url(raw)
  const digest = await crypto.subtle.digest('SHA-256', new TextEncoder().encode(verifier))
  return { verifier, challenge: b64url(new Uint8Array(digest)) }
}

function b64url(bytes: Uint8Array): string {
  let bin = ''
  for (const b of bytes) bin += String.fromCharCode(b)
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/g, '')
}
