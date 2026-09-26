import { api } from './client'

export type AssistantCapabilities = {
  shell: boolean
  browser: boolean
  mobile: boolean
  desktop: boolean
}

export type AssistantDirectoryGrant = {
  path: string
  mode: 'read' | 'readwrite'
  createdAt?: string
}

export type Assistant = {
  id: string
  userId: string
  name: string
  bio: string
  identityMode: 'proxy_user' | 'independent'
  capabilities: AssistantCapabilities
  networkTier: 'none' | 'dev_sites' | 'all'
  networkAllowlist: string[]
  directoryGrants: AssistantDirectoryGrant[]
  status: 'active' | 'disabled'
  kind?: 'user' | 'system'
  imChannels?: ImChannels
  primarySessionId?: string
  createdAt: string
  updatedAt: string
}

export type ImChannel = {
  enabled: boolean
  allowFrom?: string
  token?: string
  proxy?: string
  groupReplyAll?: boolean
  appId?: string
  appSecret?: string
  clientId?: string
  clientSecret?: string
  botToken?: string
  appToken?: string
  botId?: string
  botSecret?: string
  baseUrl?: string
  accountId?: string
}

export type ImChannels = Record<string, ImChannel>

export const IM_CHANNEL_META: {
  type: string
  label: string
  family: 'botToken' | 'appPair' | 'dualToken' | 'wecomWS' | 'weixinQR'
}[] = [
  { type: 'telegram', label: 'Telegram', family: 'botToken' },
  { type: 'discord', label: 'Discord', family: 'botToken' },
  { type: 'slack', label: 'Slack', family: 'dualToken' },
  { type: 'feishu', label: '飞书', family: 'appPair' },
  { type: 'dingtalk', label: '钉钉', family: 'appPair' },
  { type: 'wecom', label: '企业微信', family: 'wecomWS' },
  { type: 'weixin', label: '微信', family: 'weixinQR' },
]

export const assistantsApi = {
  list: () => api<{ assistants: Assistant[] }>('/v1/assistants'),
  create: (body: {
    name: string
    bio?: string
    identityMode: 'proxy_user' | 'independent'
    preset?: string
    capabilities?: AssistantCapabilities
  }) =>
    api<Assistant>('/v1/assistants', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  get: (id: string) => api<Assistant>(`/v1/assistants/${id}`),
  update: (
    id: string,
    body: Partial<{
      name: string
      bio: string
      identityMode: 'proxy_user' | 'independent'
      confirmIdentityChange: boolean
      capabilities: AssistantCapabilities
      networkTier: 'none' | 'dev_sites' | 'all'
      networkAllowlist: string[]
      directoryGrants: AssistantDirectoryGrant[]
      status: 'active' | 'disabled'
      imChannels: ImChannels
    }>,
  ) =>
    api<Assistant>(`/v1/assistants/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),
  weixinBegin: (id: string, apiUrl?: string) =>
    api<{ qrKey: string; qrUrl: string }>(
      `/v1/assistants/${id}/im/weixin/begin`,
      {
        method: 'POST',
        body: JSON.stringify({ apiUrl: apiUrl || '' }),
      },
    ),
  weixinPoll: (id: string, qrKey: string, apiUrl?: string) =>
    api<{
      status: string
      connected?: boolean
      accountId?: string
      imChannels?: ImChannels
    }>(`/v1/assistants/${id}/im/weixin/poll`, {
      method: 'POST',
      body: JSON.stringify({ qrKey, apiUrl: apiUrl || '' }),
    }),
  ensureSession: (id: string) =>
    api<{ sessionId: string }>(`/v1/assistants/${id}/ensure-session`, {
      method: 'POST',
      body: '{}',
    }),
  activity: (id: string) =>
    api<{ activity: ActivityItem[]; busy: boolean }>(
      `/v1/assistants/${id}/activity`,
    ),
  policyCheck: (
    id: string,
    body: {
      dimension: 'network' | 'directory' | 'capability'
      target: string
      mode?: string
      sessionId?: string
      record?: boolean
    },
  ) =>
    api<PolicyDecision>(`/v1/assistants/${id}/policy/check`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  listTickets: (id: string, pendingOnly = false) =>
    api<{ tickets: AssistTicket[] }>(
      `/v1/assistants/${id}/assist-tickets${pendingOnly ? '?pending=1' : ''}`,
    ),
  createTicket: (
    id: string,
    body: {
      sessionId?: string
      kind?: string
      title: string
      reason?: string
      contextSummary?: string
      askHuman?: string
      payload?: unknown
    },
  ) =>
    api<AssistTicket>(`/v1/assistants/${id}/assist-tickets`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  resolveTicket: (
    ticketId: string,
    body: { resolution: 'allow_once' | 'permanent' | 'reject'; note?: string },
  ) =>
    api<AssistTicket>(`/v1/assist-tickets/${ticketId}/resolve`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  pendingTickets: () =>
    api<{ count: number; tickets: AssistTicket[] }>(
      '/v1/me/assist-tickets/pending',
    ),
}

export type ActivityItem = {
  id: string
  at: string
  kind: string
  title: string
  detail?: string
  status?: string
  source: string
}

export type PolicyDecision = {
  allowed: boolean
  dimension: string
  target: string
  reason: string
  appliable: boolean
}

export type AssistTicket = {
  id: string
  userId: string
  assistantId: string
  sessionId: string
  kind: string
  status: string
  title: string
  reason: string
  contextSummary: string
  askHuman: string
  payload?: unknown
  resolution?: string
  resolutionNote?: string
  createdAt: string
  updatedAt: string
  resolvedAt?: string
}
