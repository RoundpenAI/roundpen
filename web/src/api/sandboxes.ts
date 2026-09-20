import { api, parseError } from './client'
import type { FileList, Sandbox } from './client'

export type PatchSandboxInput = {
  name?: string
  category?: string
  isDefault?: boolean
}

export const sandboxes = {
  get: (id: string) => api<Sandbox>(`/v1/sandboxes/${id}`),
  patch: (id: string, input: PatchSandboxInput) =>
    api<Sandbox>(`/v1/sandboxes/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    }),
  stop: (id: string) =>
    api<void>(`/v1/sandboxes/${id}/stop`, { method: 'POST' }),
}

export const files = {
  list: (id: string, path = '.') =>
    api<FileList>(
      `/v1/sandboxes/${id}/files?path=${encodeURIComponent(path)}`,
    ),
  readText: async (id: string, path: string) => {
    const res = await fetch(
      `/v1/sandboxes/${id}/files/content?path=${encodeURIComponent(path)}`,
      { credentials: 'include' },
    )
    if (!res.ok) throw await parseError(res)
    return res.text()
  },
  writeText: (id: string, path: string, content: string) =>
    api<void>(`/v1/sandboxes/${id}/files?path=${encodeURIComponent(path)}`, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/octet-stream' },
      body: content,
    }),
}

export const meWorkspace = {
  list: (path = '.') =>
    api<FileList>(
      `/v1/me/workspace/files?path=${encodeURIComponent(path)}`,
    ),
  downloadUrl: (path: string) =>
    `/v1/me/workspace/files/content?path=${encodeURIComponent(path)}`,
  upload: async (path: string, body: Blob) => {
    const res = await fetch(
      `/v1/me/workspace/files?path=${encodeURIComponent(path)}`,
      {
        method: 'POST',
        credentials: 'include',
        headers: { 'Content-Type': 'application/octet-stream' },
        body,
      },
    )
    if (!res.ok) throw await parseError(res)
  },
  remove: (path: string) =>
    api<void>(`/v1/me/workspace/files?path=${encodeURIComponent(path)}`, {
      method: 'DELETE',
    }),
}

export type PreviewLink = {
  url: string
  port: number
  token: string
  expires_at: string
}

export const preview = {
  link: (id: string, port: number, path = '/') =>
    api<PreviewLink>(
      `/v1/sandboxes/${id}/preview-link?port=${port}&path=${encodeURIComponent(path)}`,
    ),
}

export type BrowserStatus = {
  sandboxID: string
  name: string
  category?: string
  profile?: string
  attached: boolean
  url?: string
  width?: number
  height?: number
  mcp: string
  tools: string[]
}

export type BrowserSnapshot = {
  url: string
  title: string
  text: string
  nodes: { ref: string; role: string; name: string; tag?: string; value?: string }[]
}

export const browser = {
  status: (id: string) => api<BrowserStatus>(`/v1/sandboxes/${id}/browser`),
}

export function terminalWsUrl(id: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${window.location.host}/v1/sandboxes/${id}/terminal`
}
