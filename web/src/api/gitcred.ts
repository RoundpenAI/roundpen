import { api } from './client'

export type GitCredential = {
  id: string
  userId: string
  provider: string
  host: string
  username: string
  label?: string
  token?: string
  hasToken: boolean
}

export type GitRemoteRepo = {
  fullName: string
  name: string
  owner: string
  cloneUrl: string
  htmlUrl?: string
  private: boolean
  description?: string
  updatedAt?: string
}

export const gitCredentials = {
  list: () => api<{ credentials: GitCredential[] }>('/v1/me/git-credentials'),
  upsert: (body: {
    id?: string
    provider: string
    host: string
    username?: string
    label?: string
    token?: string
  }) =>
    api<GitCredential>('/v1/me/git-credentials', {
      method: 'PUT',
      body: JSON.stringify(body),
    }),
  remove: (id: string) =>
    api<void>(`/v1/me/git-credentials/${id}`, { method: 'DELETE' }),
  listRepos: (host?: string) =>
    api<{ connected: boolean; host?: string; repos: GitRemoteRepo[] }>(
      `/v1/me/git-repos${host ? `?host=${encodeURIComponent(host)}` : ''}`,
    ),
}
