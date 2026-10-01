import { api } from './client'

export type RoutineStatus = 'active' | 'paused' | 'archived'
export type RoutineAutonomy = 'read' | 'browse'
export type RoutineRunStatus =
  | 'queued'
  | 'running'
  | 'waiting_user'
  | 'succeeded'
  | 'failed'
  | 'skipped_overlap'
  | 'skipped_stale'
  | 'cancelled'

export type Routine = {
  id: string
  key: string
  userId: string
  assigneeAssistantId?: string
  createdByAssistantId?: string
  createdBySessionId?: string
  issueId?: string
  title: string
  brief: string
  autonomy: RoutineAutonomy
  hosts: string[] | null
  cron: string
  timezone: string
  deliverIm: boolean
  maxDurationSec: number
  state: unknown
  status: RoutineStatus
  nextRunAt?: string
  createdAt: string
  updatedAt: string
  lastSummary?: string
}

export type RoutineRun = {
  id: string
  key: string
  routineId: string
  userId: string
  assistantId?: string
  sessionId?: string
  status: RoutineRunStatus
  scheduledAt: string
  startedAt?: string
  finishedAt?: string
  waitStartedAt?: string
  summary: string
  error?: string
  deliverError?: string
  assistTicketId?: string
  budgetLeftSec?: number
  createdAt: string
}

export type RoutineWrite = {
  title?: string
  brief?: string
  cron?: string
  timezone?: string
  autonomy?: RoutineAutonomy
  hosts?: string[]
  deliverIm?: boolean
  maxDurationSec?: number
  assigneeAssistantId?: string
  assigneeConfirmed?: boolean
  status?: RoutineStatus
}

export const routines = {
  list: (assistantId?: string) => {
    const q = assistantId
      ? `?assistantId=${encodeURIComponent(assistantId)}`
      : ''
    return api<{ routines: Routine[] }>(`/v1/routines${q}`)
  },
  create: (body: RoutineWrite) =>
    api<{ routine: Routine }>('/v1/routines', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  get: (key: string) =>
    api<{ routine: Routine; runs: RoutineRun[] }>(`/v1/routines/${key}`),
  update: (key: string, body: RoutineWrite) =>
    api<{ routine: Routine }>(`/v1/routines/${key}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),
  cancelRun: (key: string, runKey: string) =>
    api<{ run: RoutineRun }>(`/v1/routines/${key}/runs/${runKey}/cancel`, {
      method: 'POST',
    }),
}
