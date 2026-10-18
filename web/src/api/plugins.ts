import { api } from './client'

export type PluginStatus = {
  id: string
  name: string
  description: string
  group: string
  enabled: boolean
}

export const plugins = {
  list: () => api<{ plugins: PluginStatus[] }>('/v1/admin/plugins'),
  setEnabled: (id: string, enabled: boolean) =>
    api<{ plugin: PluginStatus }>(
      `/v1/admin/plugins/${encodeURIComponent(id)}`,
      { method: 'PUT', body: JSON.stringify({ enabled }) },
    ),
}
