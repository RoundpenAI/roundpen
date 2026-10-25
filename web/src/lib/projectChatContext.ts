import {
  assistantsApi,
  projects,
  type Assistant,
  type Project,
  type ProjectWorker,
} from '../api'
import { roleLabel } from './projectChatLabels'

export type ProjectChatContext = {
  project: Project
  worker?: ProjectWorker
  /** Label for the top assistant slot (project · role). */
  label: string
  /** Longer title for tooltip. */
  title: string
}

export { roleLabel }

/**
 * Resolve project / worker chrome for a chat opened under a coordinator
 * assistant (coordinators are omitted from the assistants list API).
 */
export async function loadProjectChatContext(
  assistantId: string,
  sessionId: string,
): Promise<ProjectChatContext | null> {
  if (!assistantId) return null

  let assistant: Assistant | null = null
  try {
    assistant = await assistantsApi.get(assistantId)
  } catch {
    assistant = null
  }
  if (assistant && assistant.kind !== 'coordinator') {
    return null
  }

  let project: Project | null = null
  try {
    const res = await projects.list('active')
    project =
      (res.projects ?? []).find(
        (p) => p.coordinatorAssistantId === assistantId,
      ) ?? null
  } catch {
    return null
  }
  if (!project) return null

  let worker: ProjectWorker | undefined
  if (sessionId) {
    try {
      const res = await projects.listWorkers(project.key)
      worker = (res.workers ?? []).find((w) => w.sessionId === sessionId)
    } catch {
      worker = undefined
    }
  }

  if (worker) {
    const role = roleLabel(worker.role)
    return {
      project,
      worker,
      label: `${project.name} · ${role}`,
      title: worker.title
        ? `${project.name} · ${role} · ${worker.title}`
        : `${project.name} · ${role}`,
    }
  }

  const name =
    assistant?.name?.trim() ||
    (project.name ? `${project.name} · 协调者` : '协调者')
  return {
    project,
    label: name,
    title: `${project.name} · 协调者`,
  }
}
