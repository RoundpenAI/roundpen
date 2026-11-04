import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Badge,
  Button,
  List,
  SideSheet,
  Tag,
  Typography,
} from '@douyinfe/semi-ui-19'
import { IconPulse, IconStop } from '@douyinfe/semi-icons'
import { projects, type Project, type ProjectWorker } from '../api'
import { WorkerProgressHint } from './WorkerProgressHint'
import { useT } from '../i18n'
import type { MessageKey } from '../i18n/translate'
import { roleLabel } from '../lib/projectChatLabels'

export function pickActiveWorker(
  workers: ProjectWorker[],
): ProjectWorker | undefined {
  return (
    workers.find((w) => w.status === 'running') ??
    workers.find((w) => w.status === 'queued')
  )
}

function statusKey(status: ProjectWorker['status']): MessageKey {
  return `projects.worker.status.${status}`
}

function statusColor(
  status: ProjectWorker['status'],
): 'grey' | 'blue' | 'orange' | 'green' | 'red' {
  switch (status) {
    case 'queued':
      return 'grey'
    case 'running':
      return 'orange'
    case 'succeeded':
      return 'green'
    case 'failed':
      return 'red'
    default:
      return 'blue'
  }
}

type Props = {
  project: Project
  /** Current chat session id (coordinator or a worker). */
  sessionId: string
  /** True when this chat is a worker session. */
  isWorkerSession: boolean
  /** Notifies parent when the active (running/queued) worker changes. */
  onActiveChange?: (worker: ProjectWorker | undefined) => void
}

/** Header control + sheet: list project workers and open their live chat. */
export function CoordinatorWorkersSheet({
  project,
  sessionId,
  isWorkerSession,
  onActiveChange,
}: Props) {
  const t = useT()
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [loading, setLoading] = useState(false)
  const [stoppingKey, setStoppingKey] = useState<string | null>(null)
  const [workers, setWorkers] = useState<ProjectWorker[]>([])

  const load = useCallback(async () => {
    setLoading(true)
    try {
      const res = await projects.listWorkers(project.key)
      const list = res.workers ?? []
      setWorkers(list)
      onActiveChange?.(pickActiveWorker(list))
    } catch {
      setWorkers([])
      onActiveChange?.(undefined)
    } finally {
      setLoading(false)
    }
  }, [project.key, onActiveChange])

  useEffect(() => {
    void load()
  }, [load, sessionId])

  useEffect(() => {
    // Keep the composer stop button in sync while a worker is live.
    const timer = window.setInterval(() => void load(), open ? 4000 : 8000)
    return () => window.clearInterval(timer)
  }, [open, load])

  const liveCount = workers.filter(
    (w) => w.status === 'running' || w.status === 'queued',
  ).length
  const assistantId = project.coordinatorAssistantId

  const openWorker = (w: ProjectWorker) => {
    if (!assistantId || !w.sessionId) return
    setOpen(false)
    navigate(`/a/${assistantId}/s/${w.sessionId}`)
  }

  const stopWorker = async (w: ProjectWorker) => {
    if (w.status !== 'running' && w.status !== 'queued') return
    setStoppingKey(w.key)
    try {
      await projects.cancelWorker(project.key, w.key)
      await load()
    } finally {
      setStoppingKey(null)
    }
  }

  const backToCoordinator = () => {
    if (!assistantId || !project.coordinatorSessionId) return
    navigate(`/a/${assistantId}/s/${project.coordinatorSessionId}`)
  }

  return (
    <>
      {isWorkerSession && project.coordinatorSessionId ? (
        <Button
          theme="borderless"
          type="tertiary"
          size="small"
          onClick={backToCoordinator}
        >
          {t('projects.workers.backCoordinator')}
        </Button>
      ) : null}
      <Badge count={liveCount} overflowCount={9} type="danger" dot={false}>
        <Button
          theme="borderless"
          type="tertiary"
          size="small"
          icon={<IconPulse />}
          aria-label={t('projects.workers.liveEntry')}
          onClick={() => setOpen(true)}
        >
          {t('projects.workers.liveEntry')}
        </Button>
      </Badge>
      <SideSheet
        title={t('projects.workers.title')}
        visible={open}
        onCancel={() => setOpen(false)}
        width={Math.min(420, typeof window !== 'undefined' ? window.innerWidth : 420)}
      >
        <Typography.Paragraph type="tertiary" size="small">
          {t('projects.workers.liveGuide')}
        </Typography.Paragraph>
        <div style={{ margin: '8px 0 12px', display: 'flex', gap: 8 }}>
          <Button size="small" loading={loading} onClick={() => void load()}>
            {t('projects.refresh')}
          </Button>
          <Button
            size="small"
            theme="borderless"
            onClick={() => {
              setOpen(false)
              navigate(`/projects/${project.key}`)
            }}
          >
            {t('projects.openFromChat')}
          </Button>
        </div>
        <List
          loading={loading}
          emptyContent={t('projects.workers.empty')}
          dataSource={workers}
          renderItem={(w) => {
            const current = w.sessionId === sessionId
            const canOpen = Boolean(assistantId && w.sessionId)
            return (
              <List.Item
                className={
                  current
                    ? 'rp-coord-worker-item is-current'
                    : 'rp-coord-worker-item'
                }
                main={
                  <div className="rp-coord-worker-main">
                    <div className="rp-coord-worker-title">
                      <Typography.Text strong>
                        {w.key} · {roleLabel(w.role)}
                      </Typography.Text>
                      <Tag size="small" color={statusColor(w.status)}>
                        {t(statusKey(w.status))}
                      </Tag>
                    </div>
                    <Typography.Text
                      type="tertiary"
                      size="small"
                      ellipsis={{ showTooltip: true }}
                    >
                      {w.title}
                    </Typography.Text>
                    <div style={{ marginTop: 4 }}>
                      <WorkerProgressHint worker={w} compact />
                    </div>
                  </div>
                }
                extra={
                  <div className="rp-coord-worker-actions">
                    {w.status === 'running' || w.status === 'queued' ? (
                      <Button
                        size="small"
                        type="danger"
                        theme="borderless"
                        icon={<IconStop />}
                        loading={stoppingKey === w.key}
                        aria-label={t('projects.workers.stop')}
                        onClick={() => void stopWorker(w)}
                      >
                        {t('projects.workers.stop')}
                      </Button>
                    ) : null}
                    {canOpen ? (
                      <Button
                        size="small"
                        theme={current ? 'light' : 'solid'}
                        type="secondary"
                        disabled={current}
                        onClick={() => openWorker(w)}
                      >
                        {current
                          ? t('projects.workers.viewing')
                          : t('projects.workers.openLive')}
                      </Button>
                    ) : (
                      <Typography.Text type="tertiary" size="small">
                        {t('projects.workers.noSession')}
                      </Typography.Text>
                    )}
                  </div>
                }
              />
            )
          }}
        />
      </SideSheet>
    </>
  )
}
