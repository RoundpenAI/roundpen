import { api } from './client'

/** Auto-mode policy: prose rules read by the permission classifier. */
export type AutoModeSettings = {
  environment: string[]
  allow: string[]
  softDeny: string[]
  hardDeny: string[]
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
  llmgwEnabled: boolean
  llmgwPublicUrl: string
  llmgwLogBodyMaxBytes: number
  llmgwVirtualKeys: string
  autoMode: AutoModeSettings
}

export type SystemInfo = {
  backend: string
  dockerHost: string
  dataRoot: string
  httpAddr: string
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
  browserTest: (itemId = '') =>
    api<{ ok: boolean; error?: string; result?: BrowserTestResult }>(
      '/v1/admin/settings/browser/test',
      { method: 'POST', body: JSON.stringify({ itemId }) },
    ),
  automodeDefaults: () =>
    api<AutoModeDefaults>('/v1/admin/settings/automode/defaults'),
}
