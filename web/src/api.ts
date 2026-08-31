export type User = {
  username: string
  email: string
  fullname: string
  orgName: string
  apiKey: string
  role: string
}

export type Sandbox = {
  sandboxID: string
  name: string
  category?: string
  isDefault?: boolean
  templateID: string
  clientID: string
  state: string
  metadata?: Record<string, string>
}

export type DirEntry = {
  name: string
  is_dir: boolean
  size: number
  mod_time?: string
}

export type FileList = {
  entries: DirEntry[]
  path?: string
  host_path?: string
}

export class ApiError extends Error {
  status: number
  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

async function parseError(res: Response): Promise<ApiError> {
  let message = res.statusText
  try {
    const body = (await res.json()) as { message?: string }
    if (body.message) message = body.message
  } catch {
    /* ignore */
  }
  return new ApiError(res.status, message)
}

export async function api<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body && !(init.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const res = await fetch(path, {
    ...init,
    headers,
    credentials: 'include',
  })
  if (!res.ok) throw await parseError(res)
  if (res.status === 204) return undefined as T
  const ct = res.headers.get('Content-Type') ?? ''
  if (ct.includes('application/json')) return (await res.json()) as T
  return (await res.text()) as T
}

export const auth = {
  me: () => api<User>('/v1/auth/user'),
  login: (user: string, password: string) =>
    api<{ user: User }>('/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ user, password }),
    }),
  logout: () => api<void>('/v1/auth/logout', { method: 'POST' }),
}

export type Template = {
  templateID: string
  buildID: string
  cpuCount: number
  memoryMB: number
  diskSizeMB: number
  public: boolean
  names: string[]
  aliases: string[]
  buildStatus: string
  envdVersion: string
  createdAt?: string
  updatedAt?: string
  spawnCount?: number
  buildCount?: number
  lastSpawnedAt?: string | null
}

export type TemplateBuildSummary = {
  buildID: string
  status: string
  artifactRef?: string
  cpuCount: number
  memoryMB: number
  diskSizeMB: number
  errorMessage?: string
  createdAt: string
  updatedAt: string
}

export type TemplateDetail = Template & {
  builtin: boolean
  namespace: string
  name: string
  description: string
  profile: string
  builds: TemplateBuildSummary[]
}

export type TemplatePatch = {
  description?: string
  public?: boolean
  cpuCount?: number
  memoryMB?: number
  diskSizeMB?: number
}

export type BuildStep = {
  type: string
  args?: string[]
}

export type BuildSpec = {
  fromImage?: string
  fromTemplate?: string
  force?: boolean
  steps?: BuildStep[]
  startCmd?: string
  readyCmd?: string
  cpuCount?: number
  memoryMB?: number
}

export type CreateTemplateResult = {
  templateID: string
  buildID: string
  public: boolean
  names: string[]
  tags: string[]
  aliases: string[]
}

export type BuildLogEntry = {
  timestamp: string
  message: string
  level: string
  step?: string
}

export type BuildStatus = {
  templateID: string
  buildID: string
  status: string
  logs: string[]
  logEntries: BuildLogEntry[]
  reason?: { message: string }
}

export function templateDisplayName(t: Template): string {
  return t.names[0] ?? t.aliases[0] ?? t.templateID.slice(0, 8)
}

export const templates = {
  list: () => api<Template[]>('/templates'),
  get: (templateID: string) => api<TemplateDetail>(`/templates/${templateID}`),
  update: (templateID: string, patch: TemplatePatch) =>
    api<Template>(`/templates/${templateID}`, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    }),
  remove: (templateID: string) =>
    api<void>(`/templates/${templateID}`, { method: 'DELETE' }),
  create: (input: {
    name: string
    cpuCount?: number
    memoryMB?: number
    public?: boolean
  }) =>
    api<CreateTemplateResult>('/v3/templates', {
      method: 'POST',
      body: JSON.stringify({
        name: input.name,
        cpuCount: input.cpuCount,
        memoryMB: input.memoryMB,
        public: input.public ?? true,
      }),
    }),
  startBuild: (templateID: string, buildID: string, spec: BuildSpec) =>
    api<void>(`/v2/templates/${templateID}/builds/${buildID}`, {
      method: 'POST',
      body: JSON.stringify(spec),
    }),
  buildStatus: (templateID: string, buildID: string, logsOffset = 0) =>
    api<BuildStatus>(
      `/templates/${templateID}/builds/${buildID}/status?logsOffset=${logsOffset}&limit=200`,
    ),
}

export type CreateSandboxInput = {
  templateID: string
  timeout: number
  name?: string
  category?: string
  isDefault?: boolean
}

export type PatchSandboxInput = {
  name?: string
  category?: string
  isDefault?: boolean
}

export const sandboxes = {
  list: (category?: string) => {
    const q = category ? `?category=${encodeURIComponent(category)}` : ''
    return api<Sandbox[]>(`/sandboxes${q}`)
  },
  get: (id: string) => api<Sandbox>(`/sandboxes/${id}`),
  resolve: (opts: { name?: string; category?: string }) => {
    const params = new URLSearchParams()
    if (opts.name) params.set('name', opts.name)
    if (opts.category) params.set('category', opts.category)
    return api<Sandbox>(`/sandboxes/resolve?${params}`)
  },
  create: (input: CreateSandboxInput) =>
    api<Sandbox>('/sandboxes', {
      method: 'POST',
      body: JSON.stringify({
        templateID: input.templateID,
        timeout: input.timeout,
        name: input.name || undefined,
        category: input.category || undefined,
        isDefault: input.isDefault || undefined,
      }),
    }),
  patch: (id: string, input: PatchSandboxInput) =>
    api<Sandbox>(`/sandboxes/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    }),
  rename: (id: string, name: string) => sandboxes.patch(id, { name }),
  remove: (id: string) =>
    api<void>(`/sandboxes/${id}`, { method: 'DELETE' }),
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

export function terminalWsUrl(id: string): string {
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws'
  return `${proto}://${window.location.host}/v1/sandboxes/${id}/terminal`
}

/** Suggested categories for agent resolve (e.g. open Browser → default). */
export const SUGGESTED_CATEGORIES = ['Browser', 'Code', 'Shell'] as const

export type AppSettings = {
  allowPublicRegistration: boolean
  defaultImage: string
  defaultTtlSeconds: number
  previewPublicUrl: string
  previewTokenTtlSeconds: number
  templateBuilder: string
  kanikoDestination: string
  kanikoInsecure: boolean
  kanikoSkipTlsVerify: boolean
  kanikoExtraArgs: string
}

export type SystemInfo = {
  backend: string
  dockerHost: string
  dataRoot: string
  httpAddr: string
  templateBuilderActive: string
  templateBuilderHint?: string
}

export type SettingsResponse = {
  settings: AppSettings
  system: SystemInfo
}

export const adminSettings = {
  get: () => api<SettingsResponse>('/v1/admin/settings'),
  update: (settings: AppSettings) =>
    api<SettingsResponse>('/v1/admin/settings', {
      method: 'PUT',
      body: JSON.stringify(settings),
    }),
}
