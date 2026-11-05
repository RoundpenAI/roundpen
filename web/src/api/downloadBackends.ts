import { api } from './client'

export type DownloadBackend = {
  id: string
  userId: string
  name: string
  type: 'qbittorrent' | 'aria2' | 'webhook' | string
  baseUrl: string
  username?: string
  hasPassword?: boolean
  defaultSavePath?: string
  createdAt: string
  updatedAt: string
}

export const downloadBackends = {
  list: () => api<{ backends: DownloadBackend[] }>('/v1/me/download-backends'),
  create: (body: {
    name: string
    type: string
    baseUrl: string
    username?: string
    password?: string
    defaultSavePath?: string
  }) =>
    api<{ backend: DownloadBackend }>('/v1/me/download-backends', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  update: (
    id: string,
    body: {
      name: string
      type: string
      baseUrl: string
      username?: string
      password?: string
      defaultSavePath?: string
    },
  ) =>
    api<{ backend: DownloadBackend }>(`/v1/me/download-backends/${id}`, {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  remove: (id: string) =>
    api<void>(`/v1/me/download-backends/${id}`, { method: 'DELETE' }),
}
