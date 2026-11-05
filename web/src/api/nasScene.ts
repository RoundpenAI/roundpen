import { api } from './client'

export type NasSceneMember = {
  key: string
  name: string
  assistantId?: string
  status: 'missing' | 'active' | 'disabled' | string
}

export type NasSceneStatus = {
  enabled: boolean
  defaultOnFnOS: boolean
  authProvider: string
  members: NasSceneMember[]
}

export const nasScene = {
  get: () => api<NasSceneStatus>('/v1/me/nas-scene'),
  setEnabled: (enabled: boolean) =>
    api<NasSceneStatus>('/v1/me/nas-scene', {
      method: 'PUT',
      body: JSON.stringify({ enabled }),
    }),
  restore: () =>
    api<NasSceneStatus>('/v1/me/nas-scene/restore', { method: 'POST' }),
}
