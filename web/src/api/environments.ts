import { api } from './client'

export type EnvironmentView = {
  slot: string
  sandboxId?: string
  templateId?: string
  status: string
  name?: string
  provider?: string
  image?: string
}

export type LiveLink = {
  mode: 'managed' | 'remote' | 'cloud' | 'host'
  url: string
  hint?: string
}

export const environments = {
  list: () =>
    api<{ environments: EnvironmentView[] }>('/v1/me/environments'),
  ensureBrowser: () =>
    api<{
      slot: string
      provider?: string
      managed?: boolean
      sandboxId?: string
      status?: string
    }>('/v1/me/environments/browser/ensure', { method: 'POST' }),
  ensureAgent: () =>
    api<{ slot: string; sandboxId: string; status: string; name: string }>(
      '/v1/me/environments/agent/ensure',
      { method: 'POST', body: '{}' },
    ),
  browserLive: () =>
    api<LiveLink>('/v1/me/environments/browser/live-link'),
  upgradeAgent: (force = false) =>
    api<{ status: string; image: string; digest: string; environment: EnvironmentView }>(
      '/v1/me/environments/agent/upgrade',
      { method: 'POST', body: JSON.stringify({ force }) },
    ),
}

export type ModelSource = 'gateway' | 'own'

export const modelSource = {
  get: () => api<{ modelSource: ModelSource }>('/v1/me/model-source'),
  set: (source: ModelSource) =>
    api<{
      modelSource: ModelSource
      status?: string
      environment?: EnvironmentView
      rebuildError?: string
    }>('/v1/me/model-source', {
      method: 'PUT',
      body: JSON.stringify({ modelSource: source }),
    }),
}

export type SlotProxyView = {
  id: string
  name: string
  description?: string
}

export const slotProxies = {
  get: () =>
    api<{ proxies: SlotProxyView[]; agent: string; browser: string }>('/v1/me/proxies'),
  set: (slot: 'agent' | 'browser', profileId: string) =>
    api<{
      slot: string
      profileId: string
      status?: string
      environment?: EnvironmentView
      rebuildError?: string
    }>('/v1/me/proxy', {
      method: 'PUT',
      body: JSON.stringify({ slot, profileId }),
    }),
}

export type SetupPrivilege = 'auto' | 'manual'

export type SetupActionRun = {
  actionId: string
  title: string
  reason: string
  status: string
  privilege?: SetupPrivilege
  command: string
  error?: string
  log?: string
}

export type SetupPlan = {
  id: string
  summary: string
  context: {
    name: string
    bio: string
    identityMode: string
    preset: string
  }
  actions: SetupActionRun[] | null
  createdAt: string
}

export const setupApi = {
  llmReady: () =>
    api<{ ready: boolean; reason?: string }>('/v1/setup/llm-ready'),
  createPlan: (body: {
    name: string
    bio: string
    identityMode: string
    preset: string
  }) =>
    api<SetupPlan>('/v1/setup/plans', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  getPlan: (id: string) => api<SetupPlan>(`/v1/setup/plans/${id}`),
  confirm: (planId: string, actionId: string) =>
    api<SetupPlan>(
      `/v1/setup/plans/${planId}/actions/${actionId}/confirm`,
      { method: 'POST' },
    ),
  recheck: (planId: string, actionId: string) =>
    api<SetupPlan>(
      `/v1/setup/plans/${planId}/actions/${actionId}/recheck`,
      { method: 'POST' },
    ),
  retry: (planId: string, actionId: string) =>
    api<SetupPlan>(`/v1/setup/plans/${planId}/actions/${actionId}/retry`, {
      method: 'POST',
    }),
}

export type BrowserTask = {
  id: string
  userId: string
  kind: 'explore' | 'verify' | string
  url: string
  brief: string
  sessionId?: string
  status: string
  createdAt: string
  updatedAt: string
}

export const browserTasks = {
  list: () => api<{ tasks: BrowserTask[] }>('/v1/browser-tasks'),
  create: (body: { kind: 'explore' | 'verify'; url: string; brief?: string }) =>
    api<{
      task: BrowserTask
      sessionId: string
      prompt: string
    }>('/v1/browser-tasks', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
}

/** Suggested categories for agent resolve (e.g. open Browser → default). */
export const SUGGESTED_CATEGORIES = ['Browser', 'Code', 'Shell'] as const
