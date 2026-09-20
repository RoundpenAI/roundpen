import { api } from './client'

export type OAuthProviderOption = {
  id: string
  kind: string
  host: string
  label: string
}

// Public surface: the login page needs the buttons before a session exists.
export const oauthProviders = {
  list: () =>
    api<{ providers: OAuthProviderOption[] }>('/v1/auth/oauth/providers'),
  startUrl: (id: string, redirect?: string) =>
    `/v1/auth/oauth/${encodeURIComponent(id)}/start${
      redirect ? `?redirect=${encodeURIComponent(redirect)}` : ''
    }`,
}

export type Identity = {
  id: string
  providerId: string
  providerKind: string
  providerLabel: string
  providerHost: string
  login: string
  name: string
  email: string
  scopes: string
  expiresAt?: string
  hasRefreshToken: boolean
  lastLoginAt?: string
  createdAt: string
}

export const identities = {
  list: () => api<{ identities: Identity[] }>('/v1/me/identities'),
  linkUrl: (providerId: string) =>
    api<{ authorizeUrl: string }>(
      `/v1/me/identities/link/${encodeURIComponent(providerId)}`,
      { method: 'POST' },
    ),
  remove: (id: string) =>
    api<void>(`/v1/me/identities/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
}

export type OAuthProvider = {
  id: string
  kind: string
  scheme: string
  host: string
  label: string
  clientId: string
  clientSecret: string
  scopes: string
  authUrl: string
  tokenUrl: string
  apiUrl: string
  enabled: boolean
  displayLabel: string
  callbackUrl: string
}

export const oauthAdmin = {
  list: () => api<{ providers: OAuthProvider[] }>('/v1/admin/oauth/providers'),
  save: (body: Partial<OAuthProvider> & { id: string; kind: string; host: string }) =>
    api<{ provider: OAuthProvider }>('/v1/admin/oauth/providers', {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  remove: (id: string) =>
    api<void>(`/v1/admin/oauth/providers/${encodeURIComponent(id)}`, {
      method: 'DELETE',
    }),
}
