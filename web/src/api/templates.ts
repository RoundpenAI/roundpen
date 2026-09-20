import { api } from './client'

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
