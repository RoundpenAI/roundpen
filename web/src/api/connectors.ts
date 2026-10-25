import { api, consoleReturnOrigin } from './client'

export type ConnectorStatus = {
  kind: string
  connected: boolean
  hasClient: boolean
  official?: boolean
  email?: string
  subject?: string
  scopes?: string
}

export type GoogleClientConfig = {
  clientId: string
  clientSecret: string
  callbackUrl: string
  updatedAt?: string
}

export const connectors = {
  list: () => api<{ connectors: ConnectorStatus[] }>('/v1/me/connectors'),
  getGoogleClient: () =>
    api<GoogleClientConfig>('/v1/me/connectors/google/client'),
  saveGoogleClient: (body: { clientId: string; clientSecret: string }) =>
    api<GoogleClientConfig>('/v1/me/connectors/google/client', {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  deleteGoogleClient: () =>
    api<void>('/v1/me/connectors/google/client', { method: 'DELETE' }),
  connectGoogle: (returnOrigin = consoleReturnOrigin()) =>
    api<{ authorizeUrl: string }>('/v1/me/connectors/google/connect', {
      method: 'POST',
      body: JSON.stringify({ return: returnOrigin || consoleReturnOrigin() }),
    }),
  disconnectGoogle: () =>
    api<void>('/v1/me/connectors/google', { method: 'DELETE' }),
}
