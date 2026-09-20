import { api } from './client'

export type Issue = {
  id: string
  key: string
  userId: string
  assistantId?: string
  sessionId?: string
  title: string
  summary: string
  status:
    | 'drafting'
    | 'specced'
    | 'planned'
    | 'in_progress'
    | 'done'
    | 'cancelled'
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

export const issues = {
  list: (status?: string) =>
    api<{ issues: Issue[] }>(
      `/v1/issues${status ? `?status=${encodeURIComponent(status)}` : ''}`,
    ),
  create: (body: { title: string; summary?: string }) =>
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
    body: { status?: string; title?: string; summary?: string },
  ) =>
    api<{ issue: Issue }>(`/v1/issues/${key}`, {
      method: 'PATCH',
      body: JSON.stringify(body),
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
