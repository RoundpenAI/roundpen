import { authorizeURL as githubAuthorize, exchangeCode as githubExchange, githubProfile, refreshToken as githubRefresh } from './github'
import { authorizeURL as googleAuthorize, exchangeCode as googleExchange, googleProfile, refreshToken as googleRefresh } from './google'
import { selectScopes } from './scopes'
import type { Env, Profile, TokenResult } from './types'

export { selectScopes }

export const GITHUB_NAS_CALLBACK = '/v1/auth/oauth/github/cloud/callback'
export const GOOGLE_NAS_CALLBACK = '/v1/connectors/google/cloud/callback'

const GITHUB_SCOPES = 'repo read:user user:email'
const GOOGLE_SCOPES =
  'openid email https://www.googleapis.com/auth/gmail.readonly https://www.googleapis.com/auth/calendar.readonly'

export type Provider = {
  id: string
  label: string
  /** Registered on the upstream OAuth app. */
  workerCallbackPath: string
  nasCallbackPath: string
  defaultScopes: string
  allowedScopes: string[]
  clientId: string
  clientSecret: string
  authorize: (opts: {
    clientId: string
    redirectUri: string
    state: string
    scopes: string
    challenge: string
  }) => string
  exchange: (opts: {
    clientId: string
    clientSecret: string
    redirectUri: string
    code: string
    verifier: string
  }) => Promise<TokenResult>
  refresh: (opts: { clientId: string; clientSecret: string; refreshToken: string }) => Promise<TokenResult>
  profile: (accessToken: string) => Promise<Profile>
}

export function resolveProvider(id: string, env: Env): Provider | null {
  if (id === 'github') return githubProvider(env)
  if (id === 'google') return googleProvider(env)
  return null
}

/** Paths GitHub already has registered, plus the generic /oauth/github/callback alias. */
export function providerFromCallback(pathname: string): string | null {
  if (pathname === '/github/callback' || pathname === '/oauth/github/callback') return 'github'
  if (pathname === '/oauth/google/callback') return 'google'
  return null
}

export function providerFromStart(pathname: string): string | null {
  if (pathname === '/github/start' || pathname === '/oauth/github/start') return 'github'
  if (pathname === '/oauth/google/start') return 'google'
  return null
}

export function providerFromRefresh(pathname: string): string | null {
  const m = /^\/oauth\/([a-z0-9-]+)\/refresh$/.exec(pathname)
  return m ? m[1] : null
}

function githubProvider(env: Env): Provider {
  const scopes = scopeConfig(env.GITHUB_SCOPES, GITHUB_SCOPES)
  return {
    id: 'github',
    label: 'GitHub',
    workerCallbackPath: '/github/callback',
    nasCallbackPath: GITHUB_NAS_CALLBACK,
    ...scopes,
    clientId: env.GITHUB_CLIENT_ID,
    clientSecret: env.GITHUB_CLIENT_SECRET,
    authorize: githubAuthorize,
    exchange: githubExchange,
    refresh: githubRefresh,
    profile: githubProfile,
  }
}

function googleProvider(env: Env): Provider {
  const scopes = scopeConfig(env.GOOGLE_SCOPES, GOOGLE_SCOPES)
  return {
    id: 'google',
    label: 'Google',
    workerCallbackPath: '/oauth/google/callback',
    nasCallbackPath: GOOGLE_NAS_CALLBACK,
    ...scopes,
    clientId: env.GOOGLE_CLIENT_ID,
    clientSecret: env.GOOGLE_CLIENT_SECRET,
    authorize: googleAuthorize,
    exchange: googleExchange,
    refresh: googleRefresh,
    profile: googleProfile,
  }
}

function scopeConfig(raw: string | undefined, fallback: string): { defaultScopes: string; allowedScopes: string[] } {
  const defaultScopes = (raw || fallback).trim() || fallback
  return { defaultScopes, allowedScopes: defaultScopes.split(/\s+/).filter(Boolean) }
}
