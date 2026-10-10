import { api } from './client'

export type McpAuthMode = 'oauth' | 'api_key' | 'both'

export type McpCatalogItem = {
  id: string
  name: string
  endpoint: string
  auth: McpAuthMode
  group: string
  description: string
  docsUrl: string
  connected: boolean
  status?: string
  accountLabel?: string
}

export type McpConnection = {
  serverId: string
  serverName: string
  authKind: 'oauth' | 'api_key'
  status: string
  accountLabel?: string
  scopes?: string
  toolCount: number
  toolsSyncedAt?: string
  lastError?: string
}

export type McpToolSummary = {
  name: string
  description?: string
}

export const mcp = {
  catalog: () => api<{ servers: McpCatalogItem[] }>('/v1/me/mcp/catalog'),
  listConnections: () =>
    api<{ connections: McpConnection[] }>('/v1/me/mcp/connections'),
  connectApiKey: (serverId: string, apiKey: string) =>
    api<{ connection: McpConnection }>('/v1/me/mcp/connections', {
      method: 'POST',
      body: JSON.stringify({ serverId, apiKey }),
    }),
  startOAuth: (serverId: string, redirectTo?: string) =>
    api<{ authorizeUrl: string }>(
      `/v1/me/mcp/connections/${encodeURIComponent(serverId)}/oauth/start`,
      { method: 'POST', body: JSON.stringify({ redirectTo }) },
    ),
  refreshTools: (serverId: string) =>
    api<{ connection: McpConnection; tools: McpToolSummary[] }>(
      `/v1/me/mcp/connections/${encodeURIComponent(serverId)}/refresh-tools`,
      { method: 'POST' },
    ),
  disconnect: (serverId: string) =>
    api<void>(`/v1/me/mcp/connections/${encodeURIComponent(serverId)}`, {
      method: 'DELETE',
    }),
}
