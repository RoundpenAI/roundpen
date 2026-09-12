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

export type SetupStep = {
  title: string
  detail?: string
  command?: string
}

export type FileList = {
  entries: DirEntry[]
  path?: string
  host_path?: string
}

export class ApiError extends Error {
  status: number
  code?: string
  engine?: string
  setup?: SetupStep[]
  constructor(status: number, message: string, extra?: Partial<ApiError>) {
    super(message)
    this.status = status
    this.code = extra?.code
    this.engine = extra?.engine
    this.setup = extra?.setup
  }
}

function apiErrorMessage(body: unknown, fallback: string): string {
  if (body && typeof body === 'object') {
    const o = body as { error?: unknown; message?: unknown }
    if (typeof o.error === 'string' && o.error.trim()) return o.error
    if (typeof o.message === 'string' && o.message.trim()) return o.message
  }
  return fallback
}

async function parseError(res: Response): Promise<ApiError> {
  let message = res.statusText
  let extra: Partial<ApiError> = {}
  try {
    const body = await res.json()
    message = apiErrorMessage(body, message)
    if (body && typeof body === 'object') {
      const o = body as { code?: unknown; engine?: unknown; setup?: unknown }
      extra = {
        code: typeof o.code === 'string' ? o.code : undefined,
        engine: typeof o.engine === 'string' ? o.engine : undefined,
        setup: Array.isArray(o.setup) ? (o.setup as SetupStep[]) : undefined,
      }
    }
  } catch {
    /* ignore */
  }
  return new ApiError(res.status, message, extra)
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
  changePassword: (currentPassword: string, newPassword: string) =>
    api<void>('/v1/auth/password', {
      method: 'POST',
      body: JSON.stringify({
        current_password: currentPassword,
        new_password: newPassword,
      }),
    }),
}

export type Template = {
  templateID: string
  buildID: string
  cpuCount: number
  memoryMB: number
  diskSizeMB: number
  public: boolean
  profile?: string
  slot?: string
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

export type TemplateTag = {
  tag: string
  buildID: string
}

export type TemplateDetail = Template & {
  builtin: boolean
  namespace: string
  name: string
  description: string
  profile: string
  slot: string
  builds: TemplateBuildSummary[]
  tags: TemplateTag[]
}

export type CreateBuildResult = {
  templateID: string
  buildID: string
  tags: string[]
}

export type TemplatePatch = {
  description?: string
  public?: boolean
  profile?: string
  slot?: string
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
  keepImageCmd?: boolean
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
  spec?: BuildSpec
}

export type StartBuildResult = {
  templateID: string
  buildID: string
  forked?: boolean
}

export function templateDisplayName(t: Template): string {
  return t.names[0] ?? t.aliases[0] ?? t.templateID.slice(0, 8)
}

export const templates = {
  list: () => api<Template[]>('/v1/templates'),
  get: (templateID: string) => api<TemplateDetail>(`/v1/templates/${templateID}`),
  update: (templateID: string, patch: TemplatePatch) =>
    api<Template>(`/v1/templates/${templateID}`, {
      method: 'PATCH',
      body: JSON.stringify(patch),
    }),
  remove: (templateID: string) =>
    api<void>(`/v1/templates/${templateID}`, { method: 'DELETE' }),
  create: (input: {
    name: string
    cpuCount?: number
    memoryMB?: number
    public?: boolean
    slot?: string
    profile?: string
  }) =>
    api<CreateTemplateResult>('/v1/templates', {
      method: 'POST',
      body: JSON.stringify({
        name: input.name,
        cpuCount: input.cpuCount,
        memoryMB: input.memoryMB,
        public: input.public ?? true,
        slot: input.slot,
        profile: input.profile ?? (input.slot === 'browser' ? 'browser' : 'dev'),
      }),
    }),
  createBuild: (
    templateID: string,
    input?: {
      tags?: string[]
      assignDefault?: boolean
      cpuCount?: number
      memoryMB?: number
      diskSizeMB?: number
    },
  ) =>
    api<CreateBuildResult>(`/v1/templates/${templateID}/builds`, {
      method: 'POST',
      body: JSON.stringify({
        tags: input?.tags,
        assignDefault: input?.assignDefault,
        cpuCount: input?.cpuCount,
        memoryMB: input?.memoryMB,
        diskSizeMB: input?.diskSizeMB,
      }),
    }),
  startBuild: (
    templateID: string,
    buildID: string,
    spec: BuildSpec & { tags?: string[]; assignDefault?: boolean },
  ) =>
    api<StartBuildResult>(`/v1/templates/${templateID}/builds/${buildID}`, {
      method: 'POST',
      body: JSON.stringify(spec),
    }),
  buildStatus: (templateID: string, buildID: string, logsOffset = 0) =>
    api<BuildStatus>(
      `/v1/templates/${templateID}/builds/${buildID}/status?logsOffset=${logsOffset}&limit=200`,
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
    return api<Sandbox[]>(`/v1/sandboxes${q}`)
  },
  get: (id: string) => api<Sandbox>(`/v1/sandboxes/${id}`),
  resolve: (opts: { name?: string; category?: string }) => {
    const params = new URLSearchParams()
    if (opts.name) params.set('name', opts.name)
    if (opts.category) params.set('category', opts.category)
    return api<Sandbox>(`/v1/sandboxes/resolve?${params}`)
  },
  create: (input: CreateSandboxInput) =>
    api<Sandbox>('/v1/sandboxes', {
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
    api<Sandbox>(`/v1/sandboxes/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(input),
    }),
  rename: (id: string, name: string) => sandboxes.patch(id, { name }),
  remove: (id: string) =>
    api<void>(`/v1/sandboxes/${id}`, { method: 'DELETE' }),
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

export type EnvironmentView = {
  slot: string
  sandboxId?: string
  templateId?: string
  status: string
  name?: string
}

export type DesktopLink = {
  sandboxId: string
  wsUrl: string
  token: string
  expiresAt: string
}

export const environments = {
  list: () =>
    api<{ environments: EnvironmentView[] }>('/v1/me/environments'),
  ensureBrowser: () =>
    api<{ slot: string; sandboxId: string; status: string; name: string }>(
      '/v1/me/environments/browser/ensure',
      { method: 'POST' },
    ),
  ensureAgent: (engine?: string) =>
    api<{ slot: string; sandboxId: string; status: string; name: string }>(
      '/v1/me/environments/agent/ensure',
      {
        method: 'POST',
        body: engine ? JSON.stringify({ engine }) : JSON.stringify({}),
      },
    ),
  browserDesktop: () =>
    api<DesktopLink>('/v1/me/environments/browser/desktop'),
}

export type EngineStatus = {
  id: string
  label: string
  summary: string
  ready: boolean
  agentReady: boolean
  browserReady?: boolean
  missing?: string[]
  setup?: SetupStep[]
}

export type RuntimeSnapshot = {
  defaultAgentEngine: string
  agentEngine: string
  engines: EngineStatus[]
}

export const runtime = {
  get: () => api<RuntimeSnapshot>('/v1/runtime'),
  setAgentEngine: (agentEngine: string) =>
    api<RuntimeSnapshot>('/v1/runtime', {
      method: 'PUT',
      body: JSON.stringify({ agentEngine }),
    }),
}

export type SetupPrivilege = 'auto' | 'manual'

export type SetupActionRun = {
  actionId: string
  title: string
  reason: string
  status: string
  privilege?: SetupPrivilege
  command: string
  error?: string
  log?: string
}

export type SetupPlan = {
  id: string
  summary: string
  context: {
    name: string
    bio: string
    identityMode: string
    preset: string
  }
  actions: SetupActionRun[] | null
  createdAt: string
}

export const setupApi = {
  llmReady: () =>
    api<{ ready: boolean; reason?: string }>('/v1/setup/llm-ready'),
  createPlan: (body: {
    name: string
    bio: string
    identityMode: string
    preset: string
  }) =>
    api<SetupPlan>('/v1/setup/plans', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  getPlan: (id: string) => api<SetupPlan>(`/v1/setup/plans/${id}`),
  confirm: (planId: string, actionId: string) =>
    api<SetupPlan>(
      `/v1/setup/plans/${planId}/actions/${actionId}/confirm`,
      { method: 'POST' },
    ),
  recheck: (planId: string, actionId: string) =>
    api<SetupPlan>(
      `/v1/setup/plans/${planId}/actions/${actionId}/recheck`,
      { method: 'POST' },
    ),
  retry: (planId: string, actionId: string) =>
    api<SetupPlan>(`/v1/setup/plans/${planId}/actions/${actionId}/retry`, {
      method: 'POST',
    }),
}

export type BrowserTask = {
  id: string
  userId: string
  kind: 'explore' | 'verify' | string
  url: string
  brief: string
  sessionId?: string
  status: string
  createdAt: string
  updatedAt: string
}

export const browserTasks = {
  list: () => api<{ tasks: BrowserTask[] }>('/v1/browser-tasks'),
  create: (body: { kind: 'explore' | 'verify'; url: string; brief?: string }) =>
    api<{
      task: BrowserTask
      sessionId: string
      prompt: string
    }>('/v1/browser-tasks', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
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
  kanikoExecutor: string
  kanikoRegistryMirrors: string
  kanikoInsecure: boolean
  kanikoSkipTlsVerify: boolean
  kanikoExtraArgs: string
  llmgwEnabled: boolean
  llmgwPublicUrl: string
  llmgwLogBodyMaxBytes: number
  llmgwEmbeddingModel: string
  llmgwDefaultModel: string
  llmgwOpenaiBaseUrl: string
  llmgwOpenaiApiKey: string
  llmgwAnthropicBaseUrl: string
  llmgwAnthropicApiKey: string
  llmgwVirtualKeys: string
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

export const adminSettings = {
  get: () => api<SettingsResponse>('/v1/admin/settings'),
  update: (settings: AppSettings) =>
    api<SettingsResponse>('/v1/admin/settings', {
      method: 'PUT',
      body: JSON.stringify(settings),
    }),
}

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

export type AssistantCapabilities = {
  shell: boolean
  browser: boolean
  mobile: boolean
  desktop: boolean
}

export type AssistantDirectoryGrant = {
  path: string
  mode: 'read' | 'readwrite'
  createdAt?: string
}

export type Assistant = {
  id: string
  userId: string
  name: string
  bio: string
  identityMode: 'proxy_user' | 'independent'
  capabilities: AssistantCapabilities
  networkTier: 'none' | 'dev_sites' | 'all'
  networkAllowlist: string[]
  directoryGrants: AssistantDirectoryGrant[]
  status: 'active' | 'disabled'
  kind?: 'user' | 'system'
  primarySessionId?: string
  createdAt: string
  updatedAt: string
}

export const assistantsApi = {
  list: () => api<{ assistants: Assistant[] }>('/v1/assistants'),
  create: (body: {
    name: string
    bio?: string
    identityMode: 'proxy_user' | 'independent'
    preset?: string
    capabilities?: AssistantCapabilities
  }) =>
    api<Assistant>('/v1/assistants', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  get: (id: string) => api<Assistant>(`/v1/assistants/${id}`),
  update: (
    id: string,
    body: Partial<{
      name: string
      bio: string
      identityMode: 'proxy_user' | 'independent'
      confirmIdentityChange: boolean
      capabilities: AssistantCapabilities
      networkTier: 'none' | 'dev_sites' | 'all'
      networkAllowlist: string[]
      directoryGrants: AssistantDirectoryGrant[]
      status: 'active' | 'disabled'
    }>,
  ) =>
    api<Assistant>(`/v1/assistants/${id}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),
  ensureSession: (id: string) =>
    api<{ sessionId: string }>(`/v1/assistants/${id}/ensure-session`, {
      method: 'POST',
      body: '{}',
    }),
  activity: (id: string) =>
    api<{ activity: ActivityItem[]; busy: boolean }>(
      `/v1/assistants/${id}/activity`,
    ),
  policyCheck: (
    id: string,
    body: {
      dimension: 'network' | 'directory' | 'capability'
      target: string
      mode?: string
      sessionId?: string
      record?: boolean
    },
  ) =>
    api<PolicyDecision>(`/v1/assistants/${id}/policy/check`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  listTickets: (id: string, pendingOnly = false) =>
    api<{ tickets: AssistTicket[] }>(
      `/v1/assistants/${id}/assist-tickets${pendingOnly ? '?pending=1' : ''}`,
    ),
  createTicket: (
    id: string,
    body: {
      sessionId?: string
      kind?: string
      title: string
      reason?: string
      contextSummary?: string
      askHuman?: string
      payload?: unknown
    },
  ) =>
    api<AssistTicket>(`/v1/assistants/${id}/assist-tickets`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  resolveTicket: (
    ticketId: string,
    body: { resolution: 'allow_once' | 'permanent' | 'reject'; note?: string },
  ) =>
    api<AssistTicket>(`/v1/assist-tickets/${ticketId}/resolve`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  pendingTickets: () =>
    api<{ count: number; tickets: AssistTicket[] }>(
      '/v1/me/assist-tickets/pending',
    ),
}

export type ActivityItem = {
  id: string
  at: string
  kind: string
  title: string
  detail?: string
  status?: string
  source: string
}

export type PolicyDecision = {
  allowed: boolean
  dimension: string
  target: string
  reason: string
  appliable: boolean
}

export type AssistTicket = {
  id: string
  userId: string
  assistantId: string
  sessionId: string
  kind: string
  status: string
  title: string
  reason: string
  contextSummary: string
  askHuman: string
  payload?: unknown
  resolution?: string
  resolutionNote?: string
  createdAt: string
  updatedAt: string
  resolvedAt?: string
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
}

export type AgentMessage = {
  id: string
  sessionId: string
  role: string
  content: string
  meta?: AgentMessageMeta | null
  createdAt: string
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
  createSession: (body: { title?: string; providerId?: string }) =>
    api<AgentSession>('/v1/agent-sessions', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  getSession: (id: string) => api<AgentSession>(`/v1/agent-sessions/${id}`),
  deleteSession: (id: string) =>
    api<void>(`/v1/agent-sessions/${id}`, { method: 'DELETE' }),
  messages: (id: string) =>
    api<{ messages: AgentMessage[] }>(`/v1/agent-sessions/${id}/messages`),
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
}

