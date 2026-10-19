import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  Banner,
  Button,
  Input,
  Select,
  Spin,
  Table,
  Tag,
  TextArea,
  Toast,
  Typography,
} from '@douyinfe/semi-ui-19'
import { IconArrowLeft, IconRefresh } from '@douyinfe/semi-icons'
import type { ColumnProps } from '@douyinfe/semi-ui-19/lib/es/table'
import {
  projects,
  type Project,
  type ProjectWorker,
  type ProjectWorkerRole,
} from '../api'
import { useT, type MessageKey } from '../i18n'

const WORKER_ROLES: ProjectWorkerRole[] = [
  'implementer',
  'reviewer',
  'tester',
]

function statusKey(status: Project['status']): MessageKey {
  return `projects.status.${status}`
}

function workerStatusKey(status: ProjectWorker['status']): MessageKey {
  return `projects.worker.status.${status}`
}

function workerRoleKey(role: ProjectWorker['role']): MessageKey {
  return `projects.worker.role.${role}`
}

function workerTagColor(
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

export function ProjectDetailPage() {
  const t = useT()
  const navigate = useNavigate()
  const { key = '' } = useParams()
  const [project, setProject] = useState<Project | null>(null)
  const [workers, setWorkers] = useState<ProjectWorker[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [opening, setOpening] = useState(false)
  const [archiving, setArchiving] = useState(false)
  const [workersLoading, setWorkersLoading] = useState(false)
  const [delegateTitle, setDelegateTitle] = useState('')
  const [delegateBrief, setDelegateBrief] = useState('')
  const [delegateRole, setDelegateRole] =
    useState<ProjectWorkerRole>('implementer')
  const [delegating, setDelegating] = useState(false)

  const loadWorkers = useCallback(async (projectKey: string) => {
    setWorkersLoading(true)
    try {
      const res = await projects.listWorkers(projectKey)
      setWorkers(res.workers ?? [])
    } catch {
      /* list is best-effort on the detail page */
    } finally {
      setWorkersLoading(false)
    }
  }, [])

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    projects
      .get(key)
      .then(async (res) => {
        if (cancelled) return
        setProject(res.project)
        setError(null)
        await loadWorkers(res.project.key)
      })
      .catch((e: unknown) => {
        if (cancelled) return
        setProject(null)
        setError(e instanceof Error ? e.message : t('projects.loadFailed'))
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [key, t, loadWorkers])

  async function openCoordinator() {
    if (!project || opening) return
    setOpening(true)
    try {
      const res = await projects.ensureSession(project.key)
      const assistantId =
        res.project.coordinatorAssistantId || project.coordinatorAssistantId
      if (!assistantId) {
        throw new Error(t('projects.noCoordinator'))
      }
      navigate(`/a/${assistantId}/s/${res.sessionId}`)
    } catch (e) {
      const message =
        e instanceof Error ? e.message : t('projects.openChatFailed')
      Toast.error(message)
    } finally {
      setOpening(false)
    }
  }

  async function openWorker(w: ProjectWorker) {
    if (!project?.coordinatorAssistantId || !w.sessionId) return
    navigate(`/a/${project.coordinatorAssistantId}/s/${w.sessionId}`)
  }

  async function delegateManual() {
    if (!project || delegating) return
    const title = delegateTitle.trim()
    if (!title) {
      Toast.error(t('projects.delegate.taskTitle'))
      return
    }
    setDelegating(true)
    try {
      await projects.delegate(project.key, {
        title,
        brief: delegateBrief.trim() || undefined,
        role: delegateRole,
      })
      Toast.success(t('projects.delegate.success'))
      setDelegateTitle('')
      setDelegateBrief('')
      await loadWorkers(project.key)
    } catch (e) {
      Toast.error(
        e instanceof Error ? e.message : t('projects.delegate.failed'),
      )
    } finally {
      setDelegating(false)
    }
  }

  async function toggleArchive() {
    if (!project || archiving) return
    setArchiving(true)
    const next = project.status === 'active' ? 'archived' : 'active'
    try {
      const res = await projects.update(project.key, { status: next })
      setProject(res.project)
      Toast.success(
        next === 'archived'
          ? t('projects.archived')
          : t('projects.restored'),
      )
    } catch (e) {
      Toast.error(e instanceof Error ? e.message : t('projects.updateFailed'))
    } finally {
      setArchiving(false)
    }
  }

  const workerColumns: ColumnProps<ProjectWorker>[] = [
    {
      title: t('projects.worker.column.key'),
      dataIndex: 'key',
      width: 100,
      render: (_: unknown, row: ProjectWorker) => (
        <Typography.Text
          size="small"
          style={{ fontFamily: 'var(--semi-font-family-code)' }}
        >
          {row.key}
        </Typography.Text>
      ),
    },
    {
      title: t('projects.worker.column.role'),
      dataIndex: 'role',
      width: 110,
      render: (_: unknown, row: ProjectWorker) => (
        <Tag size="small" color="blue">
          {t(workerRoleKey(row.role || 'implementer'))}
        </Tag>
      ),
    },
    {
      title: t('projects.worker.column.title'),
      dataIndex: 'title',
    },
    {
      title: t('projects.worker.column.status'),
      dataIndex: 'status',
      width: 120,
      render: (_: unknown, row: ProjectWorker) => (
        <Tag color={workerTagColor(row.status)} size="small">
          {t(workerStatusKey(row.status))}
        </Tag>
      ),
    },
  ]

  if (loading) {
    return (
      <div style={{ padding: 48, textAlign: 'center' }}>
        <Spin />
      </div>
    )
  }

  if (error || !project) {
    return (
      <div style={{ padding: 24, maxWidth: 900, margin: '0 auto' }}>
        <Link to="/projects">{t('projects.back')}</Link>
        <Banner
          type="danger"
          description={error ?? t('projects.loadFailed')}
          closeIcon={null}
          style={{ marginTop: 16 }}
        />
      </div>
    )
  }

  return (
    <div style={{ padding: 24, maxWidth: 900, margin: '0 auto', width: '100%' }}>
      <Button
        theme="borderless"
        icon={<IconArrowLeft />}
        onClick={() => navigate('/projects')}
        style={{ marginBottom: 12, paddingLeft: 0 }}
      >
        {t('projects.back')}
      </Button>

      <div
        style={{
          display: 'flex',
          alignItems: 'flex-start',
          gap: 12,
          flexWrap: 'wrap',
          marginBottom: 8,
        }}
      >
        <Typography.Title heading={3} style={{ margin: 0, flex: 1 }}>
          {project.icon ? `${project.icon} ` : ''}
          {project.name}
        </Typography.Title>
        <Tag color={project.status === 'active' ? 'green' : 'grey'}>
          {t(statusKey(project.status))}
        </Tag>
      </div>

      <Typography.Text
        type="tertiary"
        size="small"
        style={{ fontFamily: 'var(--semi-font-family-code)' }}
      >
        {project.key}
      </Typography.Text>

      <Typography.Paragraph type="tertiary" style={{ marginTop: 16 }}>
        {t('projects.detail.guide')}
      </Typography.Paragraph>

      <div
        style={{
          display: 'grid',
          gap: 12,
          marginTop: 20,
          marginBottom: 24,
        }}
      >
        <MetaRow label={t('projects.field.slug')} value={project.slug} code />
        <MetaRow
          label={t('projects.field.path')}
          value={project.repoPath}
          code
        />
        <MetaRow
          label={t('projects.field.repoUrl')}
          value={project.repoUrl || t('projects.field.repoUrlEmpty')}
        />
        <MetaRow
          label={t('projects.field.context')}
          value={`${project.repoPath}/.roundpen/context`}
          code
        />
      </div>

      <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', marginBottom: 28 }}>
        <Button
          theme="solid"
          type="primary"
          loading={opening}
          disabled={project.status !== 'active'}
          onClick={() => void openCoordinator()}
        >
          {t('projects.openChat')}
        </Button>
        <Link
          to={`/workspace?path=${encodeURIComponent(project.repoPath)}`}
          style={{ textDecoration: 'none' }}
        >
          <Button>{t('projects.openFiles')}</Button>
        </Link>
        <Button loading={archiving} onClick={() => void toggleArchive()}>
          {project.status === 'active'
            ? t('projects.archive')
            : t('projects.restore')}
        </Button>
      </div>

      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          marginBottom: 12,
        }}
      >
        <Typography.Title heading={5} style={{ margin: 0, flex: 1 }}>
          {t('projects.workers.title')}
        </Typography.Title>
        <Button
          icon={<IconRefresh />}
          loading={workersLoading}
          onClick={() => void loadWorkers(project.key)}
          aria-label={t('projects.refresh')}
        />
      </div>
      <Typography.Paragraph type="tertiary" style={{ marginBottom: 12 }}>
        {t('projects.workers.guide')}
      </Typography.Paragraph>
      <Table
        rowKey="id"
        columns={workerColumns}
        dataSource={workers}
        pagination={false}
        loading={workersLoading}
        empty={t('projects.workers.empty')}
        onRow={(record) => ({
          style: { cursor: record?.sessionId ? 'pointer' : 'default' },
          onClick: () => {
            if (record?.sessionId) void openWorker(record)
          },
        })}
      />

      {project.status === 'active' ? (
        <div style={{ marginTop: 28 }}>
          <Typography.Title heading={5} style={{ marginBottom: 8 }}>
            {t('projects.delegate.title')}
          </Typography.Title>
          <Typography.Paragraph type="tertiary" style={{ marginBottom: 16 }}>
            {t('projects.delegate.guide')}
          </Typography.Paragraph>
          <div style={{ display: 'grid', gap: 12, maxWidth: 560 }}>
            <div>
              <Typography.Text size="small" type="tertiary">
                {t('projects.delegate.taskTitle')}
              </Typography.Text>
              <Input
                value={delegateTitle}
                onChange={setDelegateTitle}
                placeholder={t('projects.delegate.taskTitlePlaceholder')}
                disabled={delegating}
              />
            </div>
            <div>
              <Typography.Text size="small" type="tertiary">
                {t('projects.delegate.role')}
              </Typography.Text>
              <Select
                value={delegateRole}
                onChange={(v) => setDelegateRole(v as ProjectWorkerRole)}
                style={{ width: '100%' }}
                disabled={delegating}
                optionList={WORKER_ROLES.map((role) => ({
                  value: role,
                  label: t(workerRoleKey(role)),
                }))}
              />
            </div>
            <div>
              <Typography.Text size="small" type="tertiary">
                {t('projects.delegate.brief')}
              </Typography.Text>
              <TextArea
                value={delegateBrief}
                onChange={setDelegateBrief}
                placeholder={t('projects.delegate.briefPlaceholder')}
                rows={4}
                disabled={delegating}
              />
            </div>
            <div>
              <Button
                theme="solid"
                type="secondary"
                loading={delegating}
                onClick={() => void delegateManual()}
              >
                {t('projects.delegate.submit')}
              </Button>
            </div>
          </div>
        </div>
      ) : null}
    </div>
  )
}

function MetaRow({
  label,
  value,
  code,
}: {
  label: string
  value: string
  code?: boolean
}) {
  return (
    <div>
      <Typography.Text size="small" type="tertiary">
        {label}
      </Typography.Text>
      <div>
        <Typography.Text
          style={
            code
              ? { fontFamily: 'var(--semi-font-family-code)' }
              : undefined
          }
        >
          {value}
        </Typography.Text>
      </div>
    </div>
  )
}
