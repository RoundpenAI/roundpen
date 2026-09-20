import { api, apiErrorMessage, ApiError } from './client'

export type AgentProvider = {
  id: string
  name: string
  description?: string
  enabled: boolean
  mode: string
  templateId?: string
}

export type AgentSession = {
  id: string
  userId: string
  title: string
  providerId: string
  sandboxId: string
  assistantId?: string
  status: string
  createdAt: string
  updatedAt: string
}

export type AgentMessageMeta = {
  type?: string
  toolId?: string
  title?: string
  status?: string
  kind?: string
  input?: unknown
  output?: unknown
  stopReason?: string
  durationMs?: number
  requestId?: string
  optionId?: string
  outcome?: string
  options?: { optionId: string; name: string; kind?: string }[]
  // Skill commands persist the expanded instruction text as content; display
  // keeps what the user actually typed ("/review 关注并发").
  display?: string
  command?: string
  commandArgs?: string
}

export type AgentMessage = {
  id: string
  sessionId: string
  role: string
  content: string
  meta?: AgentMessageMeta | null
  createdAt: string
}

export type AgentCommand = {
  name: string
  kind?: string
  source?: string
  description?: string
  args?: boolean
}

export type AgentBrowserStatus = {
  sessionId: string
  hubId: string
  attached: boolean
  url: string
  width: number
  height: number
  takeover: boolean
}

export const agents = {
  list: () => api<{ agents: AgentProvider[] }>('/v1/agents'),
  sessions: () => api<{ sessions: AgentSession[] }>('/v1/agent-sessions'),
  createSession: (body: {
    title?: string
    providerId?: string
    assistantId?: string
  }) =>
    api<AgentSession>('/v1/agent-sessions', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  getSession: (id: string) => api<AgentSession>(`/v1/agent-sessions/${id}`),
  renameSession: (id: string, title: string) =>
    api<AgentSession>(`/v1/agent-sessions/${id}`, {
      method: 'PATCH',
      body: JSON.stringify({ title }),
    }),
  deleteSession: (id: string) =>
    api<void>(`/v1/agent-sessions/${id}`, { method: 'DELETE' }),
  messages: (id: string) =>
    api<{ messages: AgentMessage[] }>(`/v1/agent-sessions/${id}/messages`),
  commands: (id: string) =>
    api<{ commands: AgentCommand[] }>(`/v1/agent-sessions/${id}/commands`),
  sessionWsUrl: (id: string) => {
    const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
    return `${proto}://${window.location.host}/v1/agent-sessions/${id}/ws`
  },
  browserStatus: (id: string) =>
    api<AgentBrowserStatus>(`/v1/agent-sessions/${id}/browser`),
  browserScreenshotUrl: (id: string, bust?: number) =>
    `/v1/agent-sessions/${id}/browser/screenshot${bust != null ? `?t=${bust}` : ''}`,
  setBrowserTakeover: (id: string, enabled: boolean) =>
    api<{
      ok: boolean
      attached?: boolean
      takeover: boolean
      url: string
      width: number
      height: number
    }>(`/v1/agent-sessions/${id}/browser/takeover`, {
      method: 'POST',
      body: JSON.stringify({ enabled }),
    }),
  browserScreenshotBlob: async (id: string): Promise<Blob> => {
    const res = await fetch(`/v1/agent-sessions/${id}/browser/screenshot?t=${Date.now()}`, {
      credentials: 'include',
    })
    if (!res.ok) {
      let message = res.statusText
      try {
        message = apiErrorMessage(await res.json(), message)
      } catch {
        /* ignore */
      }
      throw new ApiError(res.status, message)
    }
    return res.blob()
  },
  browserInput: (
    id: string,
    body:
      | { type: 'click' | 'move'; x: number; y: number }
      | { type: 'wheel'; x: number; y: number; deltaX: number; deltaY: number }
      | { type: 'type'; text: string }
      | { type: 'key'; key: string },
  ) =>
    api<{ ok: boolean; url?: string }>(`/v1/agent-sessions/${id}/browser/input`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
}
