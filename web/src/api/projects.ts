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

export type ProjectWorkerRole = 'implementer' | 'reviewer' | 'tester'

export type ReviewFinding = {
  severity: string
  path?: string
  line?: number
  note: string
}

export type ReviewResult = {
  verdict: 'approve' | 'request_changes' | string
  summary?: string
  findings?: ReviewFinding[]
}

export type ProjectWorker = {
  id: string
  key: string
  projectId: string
  userId: string
  sessionId?: string
  issueId?: string
  title: string
  brief: string
  role: ProjectWorkerRole
  status: 'queued' | 'running' | 'succeeded' | 'failed' | 'cancelled'
  error?: string
  review?: ReviewResult
  userDecision?: '' | 'passed' | 'changes_requested'
  createdAt: string
  updatedAt: string
  startedAt?: string
  finishedAt?: string
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
  listWorkers: (key: string) =>
    api<{ workers: ProjectWorker[] }>(`/v1/projects/${key}/workers`),
  delegate: (
    key: string,
    body: {
      title: string
      brief?: string
      role?: ProjectWorkerRole
      issueKey?: string
    },
  ) =>
    api<{ worker: ProjectWorker }>(`/v1/projects/${key}/workers`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  getWorker: (key: string, workerKey: string) =>
    api<{ worker: ProjectWorker }>(
      `/v1/projects/${key}/workers/${encodeURIComponent(workerKey)}`,
    ),
  decideWorker: (
    key: string,
    workerKey: string,
    decision: 'passed' | 'changes_requested',
  ) =>
    api<{ worker: ProjectWorker; followUp?: ProjectWorker }>(
      `/v1/projects/${key}/workers/${encodeURIComponent(workerKey)}/decision`,
      {
        method: 'POST',
        body: JSON.stringify({ decision }),
      },
    ),
}
