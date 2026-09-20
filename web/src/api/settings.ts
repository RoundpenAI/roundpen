import { api } from './client'

export type ProxyProfile = {
  id: string
  name: string
  url: string
  description?: string
}

/** Auto-mode policy: prose rules read by the permission classifier. */
export type AutoModeSettings = {
  environment: string[]
  allow: string[]
  softDeny: string[]
  hardDeny: string[]
  /** Classifier model; empty uses the gateway default. */
  model?: string
}

export type AutoModeDefaults = {
  environment: string[]
  allow: string[]
  softDeny: string[]
  hardDeny: string[]
}

export type AppSettings = {
  allowPublicRegistration: boolean
  defaultImage: string
  defaultTtlSeconds: number
  previewPublicUrl: string
  previewTokenTtlSeconds: number
  templateBuilder: string
  llmgwEnabled: boolean
  llmgwPublicUrl: string
  llmgwLogBodyMaxBytes: number
  llmgwEmbeddingModel: string
  llmgwDefaultModel: string
  llmgwOpenaiBaseUrl: string
  llmgwOpenaiApiKey: string
  llmgwOpenaiProxy: string
  llmgwAnthropicBaseUrl: string
  llmgwAnthropicApiKey: string
  llmgwAnthropicProxy: string
  llmgwVirtualKeys: string
  webSearchEndpoint: string
  webSearchApiKey: string
  webSearchProxy: string
  proxies: ProxyProfile[]
  autoMode: AutoModeSettings
  cdpProvider: string
  cdpEndpoint: string
  cdpToken: string
  cdpPort: number
}

export type SystemInfo = {
  backend: string
  dockerHost: string
  dataRoot: string
  httpAddr: string
  templateBuilderActive: string
  templateBuilderHint?: string
  llmgwActive: boolean
  llmgwMounted: boolean
  cdpProviderActive: string
  cdpHostChromeFound: boolean
  cdpHint?: string
}

export type SettingsResponse = {
  settings: AppSettings
  system: SystemInfo
}

export type BrowserTestResult = {
  provider: string
  endpoint?: string
  path?: string
  version?: string
  playwright?: string[]
  chromePath?: string
  chromeOk?: boolean
}

export const adminSettings = {
  get: () => api<SettingsResponse>('/v1/admin/settings'),
  update: (settings: AppSettings) =>
    api<SettingsResponse>('/v1/admin/settings', {
      method: 'PUT',
      body: JSON.stringify(settings),
    }),
  browserTest: () =>
    api<{ ok: boolean; error?: string; result?: BrowserTestResult }>(
      '/v1/admin/settings/browser/test',
      { method: 'POST' },
    ),
  automodeDefaults: () =>
    api<AutoModeDefaults>('/v1/admin/settings/automode/defaults'),
}
