import { api } from './client'

export type IssueStatus =
  | 'new'
  | 'spec_review'
  | 'needs_discussion'
  | 'triaged'
  | 'claimed'
  | 'running'
  | 'verified'
  | 'needs_review'
  | 'failed'
  | 'closed'
  | 'cancelled'

export type Issue = {
  id: string
  key: string
  userId: string
  assistantId?: string
  sessionId?: string
  projectId?: string
  title: string
  summary: string
  prUrl?: string
  status: IssueStatus
  origin: 'chat' | 'console'
  createdAt: string
  updatedAt: string
  closedAt?: string
}

export type IssueDoc = {
  id: string
  key: string
  issueId: string
  taskId?: string
  kind: 'spec' | 'plan'
  version: number
  status: 'draft' | 'current' | 'superseded'
  title: string
  /** Index responses (listDocs, issue detail) omit the body. */
  contentMd?: string
  authorType: 'user' | 'assistant'
  createdAt: string
}

/** Named IssueTask because the Go type name `Task` is too generic here. */
export type IssueTask = {
  id: string
  key: string
  issueId: string
  planDocId?: string
  position: number
  title: string
  detail: string
  status: 'todo' | 'in_progress' | 'done' | 'blocked' | 'cancelled'
  sessionId?: string
  createdAt: string
  updatedAt: string
  doneAt?: string
}

export type BoardColumn = {
  status: IssueStatus
  terminal: boolean
  issues: Issue[]
}

export const ISSUE_BOARD_ORDER: IssueStatus[] = [
  'new',
  'spec_review',
  'needs_discussion',
  'triaged',
  'claimed',
  'running',
  'verified',
  'needs_review',
  'failed',
  'closed',
  'cancelled',
]

/** Allowed Kanban edges — keep in sync with internal/issue allowedTransitions. */
export const ISSUE_TRANSITIONS: Record<IssueStatus, IssueStatus[]> = {
  new: ['spec_review', 'needs_discussion', 'triaged'],
  spec_review: ['needs_discussion', 'triaged'],
  needs_discussion: ['spec_review', 'triaged'],
  triaged: ['claimed'],
  claimed: ['running'],
  running: ['verified', 'needs_review', 'failed', 'triaged'],
  verified: ['needs_review', 'closed'],
  needs_review: ['closed', 'cancelled'],
  failed: ['triaged'],
  cancelled: ['triaged'],
  closed: ['running'],
}

export function canTransition(from: IssueStatus, to: IssueStatus): boolean {
  if (from === to) return true
  return (ISSUE_TRANSITIONS[from] ?? []).includes(to)
}

export function allowedTargets(from: IssueStatus): IssueStatus[] {
  return [...(ISSUE_TRANSITIONS[from] ?? [])]
}

export const issues = {
  list: (status?: string, projectId?: string) => {
    const q = new URLSearchParams()
    if (status) q.set('status', status)
    if (projectId) q.set('projectId', projectId)
    const qs = q.toString()
    return api<{ issues: Issue[] }>(`/v1/issues${qs ? `?${qs}` : ''}`)
  },
  board: (projectId?: string) => {
    const q = new URLSearchParams()
    if (projectId) q.set('projectId', projectId)
    const qs = q.toString()
    return api<{
      columns: BoardColumn[]
      order: IssueStatus[]
      terminal: IssueStatus[]
    }>(`/v1/issues/board${qs ? `?${qs}` : ''}`)
  },
  create: (body: { title: string; summary?: string; projectId?: string }) =>
    api<{ issue: Issue }>('/v1/issues', {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  get: (key: string) =>
    api<{ issue: Issue; docs: IssueDoc[]; tasks: IssueTask[] }>(
      `/v1/issues/${key}`,
    ),
  update: (
    key: string,
    body: { status?: string; title?: string; summary?: string; prUrl?: string },
  ) =>
    api<{ issue: Issue }>(`/v1/issues/${key}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),
  transition: (key: string, to: string, reason?: string) =>
    api<{ issue: Issue }>(`/v1/issues/${key}/transitions`, {
      method: 'POST',
      body: JSON.stringify({ to, reason }),
    }),
  listDocs: (key: string, kind?: string) =>
    api<{ docs: IssueDoc[] }>(
      `/v1/issues/${key}/docs${kind ? `?kind=${encodeURIComponent(kind)}` : ''}`,
    ),
  getDoc: (key: string, docKey: string) =>
    api<{ doc: IssueDoc }>(`/v1/issues/${key}/docs/${docKey}`),
  writeDoc: (
    key: string,
    body: {
      kind: 'spec' | 'plan'
      contentMd: string
      title?: string
      status?: string
      taskKey?: string
    },
  ) =>
    api<{ doc: IssueDoc }>(`/v1/issues/${key}/docs`, {
      method: 'POST',
      // The API defaults authorType to "assistant"; console edits are human.
      body: JSON.stringify({ ...body, authorType: 'user' }),
    }),
  listTasks: (key: string) =>
    api<{ tasks: IssueTask[] }>(`/v1/issues/${key}/tasks`),
  createTask: (
    key: string,
    body: { title: string; detail?: string; position?: number },
  ) =>
    api<{ task: IssueTask }>(`/v1/issues/${key}/tasks`, {
      method: 'POST',
      body: JSON.stringify(body),
    }),
  updateTask: (
    key: string,
    body: { status?: string; title?: string; detail?: string },
  ) =>
    api<{ task: IssueTask }>(`/v1/tasks/${key}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
    }),
}
