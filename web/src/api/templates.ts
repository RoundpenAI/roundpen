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

export function templateDisplayName(t: Template): string {
  return t.names[0] ?? t.aliases[0] ?? t.templateID.slice(0, 8)
}

export const templates = {
  list: () => api<Template[]>('/v1/templates'),
}
