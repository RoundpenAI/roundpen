import { api } from './client'

export type Project = {
  id: string
  key: string
  userId: string
  name: string
  icon?: string
  slug: string
  repoPath: string
  repoUrl?: string
  coordinatorAssistantId?: string
  coordinatorSessionId?: string
  status: 'active' | 'archived'
  createdAt: string
  updatedAt: string
}

export const projects = {
  list: (status?: string) =>
    api<{ projects: Project[] }>(
      `/v1/projects${status ? `?status=${encodeURIComponent(status)}` : ''}`,
    ),
  create: (body: {
    name: string
    slug?: string
    icon?: string
    repoUrl?: string
  }) =>
    api<{ project: Project }>('/v1/projects', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  get: (key: string) =>
    api<{ project: Project }>(`/v1/projects/${key}`),
  update: (
    key: string,
    body: {
      name?: string
      icon?: string
      repoUrl?: string
      status?: 'active' | 'archived'
    },
  ) =>
    api<{ project: Project }>(`/v1/projects/${key}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),
  ensureSession: (key: string) =>
    api<{ sessionId: string; project: Project }>(
      `/v1/projects/${key}/ensure-session`,
      { method: 'POST' },
    ),
}
