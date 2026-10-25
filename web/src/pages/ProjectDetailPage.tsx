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
  type ProjectCIStatus,
  type ProjectSubscription,
  type ProjectSubscriptionKind,
  type ProjectWorker,
  type ProjectWorkerRole,
} from '../api'
import { workspacePreviewHref } from '../lib/changeSet'
import { useT, type MessageKey } from '../i18n'
import { useMatchMedia } from '../lib/useMatchMedia'

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
  const narrow = useMatchMedia('(max-width: 767px)')
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
  const [selectedReviewer, setSelectedReviewer] =
    useState<ProjectWorker | null>(null)
  const [deciding, setDeciding] = useState(false)
  const [subscriptions, setSubscriptions] = useState<ProjectSubscription[]>(
    [],
  )
  const [subsLoading, setSubsLoading] = useState(false)
  const [subKind, setSubKind] = useState<ProjectSubscriptionKind>('github_pr')
  const [subRepo, setSubRepo] = useState('')
  const [creatingSub, setCreatingSub] = useState(false)
  const [newSecret, setNewSecret] = useState<string | null>(null)
  const [ci, setCi] = useState<ProjectCIStatus | null>(null)
  const [ciLoading, setCiLoading] = useState(false)
  const [cloning, setCloning] = useState(false)
  const [cloneError, setCloneError] = useState<string | null>(null)

  const loadWorkers = useCallback(async (projectKey: string) => {
    setWorkersLoading(true)
    try {
      const res = await projects.listWorkers(projectKey)
      const list = res.workers ?? []
      setWorkers(list)
      setSelectedReviewer((prev) => {
        if (prev) {
          const fresh = list.find((w) => w.id === prev.id)
          if (fresh) return fresh
        }
        return (
          list.find(
            (w) =>
              w.role === 'reviewer' &&
              (w.status === 'succeeded' || w.status === 'failed'),
          ) ?? null
        )
      })
    } catch {
      /* list is best-effort on the detail page */
    } finally {
      setWorkersLoading(false)
    }
  }, [])

  const loadSubscriptions = useCallback(async (projectKey: string) => {
    setSubsLoading(true)
    try {
      const res = await projects.listSubscriptions(projectKey)
      setSubscriptions(res.subscriptions ?? [])
    } catch {
      setSubscriptions([])
    } finally {
      setSubsLoading(false)
    }
  }, [])

  const loadCI = useCallback(async (projectKey: string, refresh = false) => {
    setCiLoading(true)
    try {
      setCi(await projects.ciStatus(projectKey, refresh))
    } catch {
      setCi(null)
    } finally {
      setCiLoading(false)
    }
  }, [])

  const decideReview = async (decision: 'passed' | 'changes_requested') => {
    if (!project || !selectedReviewer) return
    setDeciding(true)
    try {
      const res = await projects.decideWorker(
        project.key,
        selectedReviewer.key,
        decision,
      )
      Toast.success(
        decision === 'passed'
          ? t('projects.review.passed')
          : t('projects.review.changesRequested'),
      )
      setSelectedReviewer(res.worker)
      await loadWorkers(project.key)
    } catch (e: unknown) {
      Toast.error(
        e instanceof Error ? e.message : t('projects.review.decideFailed'),
      )
    } finally {
      setDeciding(false)
    }
  }

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    setCloneError(null)
    projects
      .get(key)
      .then(async (res) => {
        if (cancelled) return
        setProject(res.project)
        setError(null)
        setSubRepo(res.project.repoUrl || '')
        const sideLoads = Promise.all([
          loadWorkers(res.project.key),
          loadSubscriptions(res.project.key),
          loadCI(res.project.key),
        ])
        if (
          res.project.status === 'active' &&
          res.project.repoUrl?.trim()
        ) {
          setCloning(true)
          try {
            await projects.ensureRepo(res.project.key)
            if (!cancelled) setCloneError(null)
          } catch (e: unknown) {
            if (!cancelled) {
              const message =
                e instanceof Error ? e.message : t('projects.repo.cloneFailed')
              setCloneError(message)
              Toast.error(message)
            }
          } finally {
            if (!cancelled) setCloning(false)
          }
        } else {
          setCloning(false)
        }
        await sideLoads
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
  }, [key, t, loadWorkers, loadSubscriptions, loadCI])

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

  async function createSubscription() {
    if (!project || creatingSub) return
    setCreatingSub(true)
    setNewSecret(null)
    try {
      const res = await projects.createSubscription(project.key, {
        kind: subKind,
        repo: subRepo.trim() || undefined,
      })
      if (res.subscription.secret) {
        setNewSecret(res.subscription.secret)
      }
      Toast.success(t('projects.subs.created'))
      await loadSubscriptions(project.key)
      if (subKind === 'github_ci') {
        await loadCI(project.key)
      }
    } catch (e) {
      Toast.error(
        e instanceof Error ? e.message : t('projects.subs.createFailed'),
      )
    } finally {
      setCreatingSub(false)
    }
  }

  async function toggleSub(sub: ProjectSubscription) {
    if (!project) return
    try {
      await projects.updateSubscription(project.key, sub.key, {
        enabled: !sub.enabled,
      })
      await loadSubscriptions(project.key)
    } catch (e) {
      Toast.error(
        e instanceof Error ? e.message : t('projects.subs.updateFailed'),
      )
    }
  }

  async function removeSub(sub: ProjectSubscription) {
    if (!project) return
    try {
      await projects.deleteSubscription(project.key, sub.key)
      Toast.success(t('projects.subs.deleted'))
      await loadSubscriptions(project.key)
      await loadCI(project.key)
    } catch (e) {
      Toast.error(
        e instanceof Error ? e.message : t('projects.subs.deleteFailed'),
      )
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
    ...(narrow
      ? [
          {
            title: t('projects.worker.column.title'),
            dataIndex: 'title',
            render: (_: unknown, row: ProjectWorker) => (
              <div className="rp-projects-cell">
                <Typography.Text strong>{row.title}</Typography.Text>
                <div className="rp-projects-cell-tags">
                  <Tag size="small" color="blue">
                    {t(workerRoleKey(row.role || 'implementer'))}
                  </Tag>
                  <Tag color={workerTagColor(row.status)} size="small">
                    {t(workerStatusKey(row.status))}
                  </Tag>
                  <span className="rp-projects-cell-meta">{row.key}</span>
                </div>
              </div>
            ),
          } satisfies ColumnProps<ProjectWorker>,
        ]
      : [
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
          } satisfies ColumnProps<ProjectWorker>,
          {
            title: t('projects.worker.column.role'),
            dataIndex: 'role',
            width: 110,
            render: (_: unknown, row: ProjectWorker) => (
              <Tag size="small" color="blue">
                {t(workerRoleKey(row.role || 'implementer'))}
              </Tag>
            ),
          } satisfies ColumnProps<ProjectWorker>,
          {
            title: t('projects.worker.column.title'),
            dataIndex: 'title',
          } satisfies ColumnProps<ProjectWorker>,
          {
            title: t('projects.worker.column.status'),
            dataIndex: 'status',
            width: 120,
            render: (_: unknown, row: ProjectWorker) => (
              <Tag color={workerTagColor(row.status)} size="small">
                {t(workerStatusKey(row.status))}
              </Tag>
            ),
          } satisfies ColumnProps<ProjectWorker>,
        ]),
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
      <div className="rp-projects rp-projects-detail">
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
    <div className="rp-projects rp-projects-detail">
      <Button
        theme="borderless"
        icon={<IconArrowLeft />}
        onClick={() => navigate('/projects')}
        style={{ marginBottom: 12, paddingLeft: 0 }}
      >
        {t('projects.back')}
      </Button>

      <div className="rp-projects-head" style={{ marginBottom: 8 }}>
        <Typography.Title heading={3} className="rp-projects-title">
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

      {cloning ? (
        <Banner
          type="info"
          description={
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}>
              <Spin size="small" />
              {t('projects.repo.cloning')}
            </span>
          }
          closeIcon={null}
          style={{ marginBottom: 16 }}
        />
      ) : null}
      {cloneError ? (
        <Banner
          type="danger"
          description={cloneError}
          closeIcon={null}
          style={{ marginBottom: 16 }}
        />
      ) : null}

      <div className="rp-projects-meta">
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

      <div className="rp-projects-actions">
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
        <Tag color={ciTagColor(ci?.status)} size="large">
          {t('projects.ci.label')}: {t(ciStatusKey(ci?.status))}
        </Tag>
        <Button
          icon={<IconRefresh />}
          loading={ciLoading}
          onClick={() => void loadCI(project.key, true)}
          aria-label={t('projects.ci.refresh')}
        />
      </div>

      <div style={{ marginBottom: 28 }}>
        <Typography.Title heading={5} style={{ marginBottom: 8 }}>
          {t('projects.subs.title')}
        </Typography.Title>
        <Typography.Paragraph type="tertiary" style={{ marginBottom: 12 }}>
          {t('projects.subs.guide')}
        </Typography.Paragraph>
        {newSecret ? (
          <Banner
            type="warning"
            description={`${t('projects.subs.secretOnce')}: ${newSecret}`}
            closeIcon={null}
            style={{ marginBottom: 12 }}
          />
        ) : null}
        {subscriptions.length === 0 && !subsLoading ? (
          <Typography.Paragraph type="tertiary" size="small">
            {t('projects.subs.empty')}
          </Typography.Paragraph>
        ) : (
          <div style={{ display: 'grid', gap: 10, marginBottom: 16 }}>
            {subscriptions.map((sub) => (
              <div
                key={sub.id}
                style={{
                  display: 'flex',
                  flexWrap: 'wrap',
                  gap: 8,
                  alignItems: 'center',
                }}
              >
                <Tag color={sub.enabled ? 'blue' : 'grey'} size="small">
                  {sub.kind === 'github_pr'
                    ? t('projects.subs.kind.pr')
                    : t('projects.subs.kind.ci')}
                </Tag>
                <Typography.Text
                  size="small"
                  style={{ fontFamily: 'var(--semi-font-family-code)' }}
                >
                  {sub.repo || '—'}
                </Typography.Text>
                <Typography.Text
                  size="small"
                  type="tertiary"
                  style={{ fontFamily: 'var(--semi-font-family-code)' }}
                >
                  {sub.hookPath}
                </Typography.Text>
                <Button size="small" onClick={() => void toggleSub(sub)}>
                  {sub.enabled
                    ? t('projects.subs.disable')
                    : t('projects.subs.enable')}
                </Button>
                <Button
                  size="small"
                  type="danger"
                  theme="borderless"
                  onClick={() => void removeSub(sub)}
                >
                  {t('projects.subs.delete')}
                </Button>
              </div>
            ))}
          </div>
        )}
        {project.status === 'active' ? (
          <div
            style={{
              display: 'flex',
              flexWrap: 'wrap',
              gap: 8,
              alignItems: 'flex-end',
              maxWidth: 640,
            }}
          >
            <div style={{ minWidth: 140 }}>
              <Typography.Text size="small" type="tertiary">
                {t('projects.subs.kind')}
              </Typography.Text>
              <Select
                value={subKind}
                onChange={(v) => setSubKind(v as ProjectSubscriptionKind)}
                style={{ width: '100%' }}
                disabled={creatingSub}
                optionList={[
                  { value: 'github_pr', label: t('projects.subs.kind.pr') },
                  { value: 'github_ci', label: t('projects.subs.kind.ci') },
                ]}
              />
            </div>
            <div style={{ flex: 1, minWidth: 200 }}>
              <Typography.Text size="small" type="tertiary">
                {t('projects.subs.repo')}
              </Typography.Text>
              <Input
                value={subRepo}
                onChange={setSubRepo}
                placeholder={t('projects.subs.repoPlaceholder')}
                disabled={creatingSub}
              />
            </div>
            <Button
              theme="solid"
              type="secondary"
              loading={creatingSub}
              onClick={() => void createSubscription()}
            >
              {t('projects.subs.add')}
            </Button>
          </div>
        ) : null}
      </div>

      <div className="rp-projects-head">
        <Typography.Title heading={5} className="rp-projects-title">
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
        className={
          narrow
            ? 'rp-projects-table rp-projects-table-narrow'
            : 'rp-projects-table'
        }
        rowKey="id"
        columns={workerColumns}
        dataSource={workers}
        pagination={false}
        loading={workersLoading}
        empty={t('projects.workers.empty')}
        onRow={(record) => ({
          style: {
            cursor:
              record?.role === 'reviewer' || record?.sessionId
                ? 'pointer'
                : 'default',
          },
          onClick: () => {
            if (record?.role === 'reviewer') {
              setSelectedReviewer(record)
              return
            }
            if (record?.sessionId) void openWorker(record)
          },
        })}
      />

      {selectedReviewer?.role === 'reviewer' ? (
        <div className="rp-review-panel">
          <Typography.Title heading={5} style={{ margin: '0 0 4px' }}>
            {t('projects.review.title')} · {selectedReviewer.key}
          </Typography.Title>
          <Typography.Paragraph type="tertiary" size="small">
            {t('projects.review.guide')}
          </Typography.Paragraph>
          {selectedReviewer.review ? (
            <>
              <div style={{ marginTop: 12, display: 'flex', gap: 8, alignItems: 'center' }}>
                <Tag
                  color={
                    selectedReviewer.review.verdict === 'approve'
                      ? 'green'
                      : 'orange'
                  }
                >
                  {selectedReviewer.review.verdict === 'approve'
                    ? t('projects.review.verdict.approve')
                    : t('projects.review.verdict.request_changes')}
                </Tag>
                {selectedReviewer.userDecision === 'passed' ? (
                  <Typography.Text type="success" size="small">
                    {t('projects.review.passed')}
                  </Typography.Text>
                ) : null}
                {selectedReviewer.userDecision === 'changes_requested' ? (
                  <Typography.Text type="warning" size="small">
                    {t('projects.review.changesRequested')}
                  </Typography.Text>
                ) : null}
              </div>
              {selectedReviewer.review.summary ? (
                <Typography.Paragraph style={{ marginTop: 10 }}>
                  <strong>{t('projects.review.summary')}: </strong>
                  {selectedReviewer.review.summary}
                </Typography.Paragraph>
              ) : null}
              {(selectedReviewer.review.findings?.length ?? 0) > 0 ? (
                <div style={{ marginTop: 8 }}>
                  <Typography.Text type="tertiary" size="small">
                    {t('projects.review.findings')}
                  </Typography.Text>
                  {selectedReviewer.review.findings!.map((f, i) => (
                    <div
                      key={i}
                      className={`rp-review-finding is-${f.severity || 'info'}`}
                    >
                      <Typography.Text size="small">
                        <Tag size="small" style={{ marginRight: 6 }}>
                          {f.severity || 'info'}
                        </Tag>
                        {f.path ? (
                          <>
                            <code>{f.path}{f.line ? `:${f.line}` : ''}</code>
                            {' — '}
                          </>
                        ) : null}
                        {f.note}
                      </Typography.Text>
                      {f.path ? (
                        <div style={{ marginTop: 4 }}>
                          <Link
                            to={workspacePreviewHref(
                              f.path.startsWith(project.repoPath)
                                ? f.path
                                : `${project.repoPath}/${f.path}`.replace(
                                    /\/+/g,
                                    '/',
                                  ),
                            )}
                          >
                            {t('projects.review.openFile')}
                          </Link>
                        </div>
                      ) : null}
                    </div>
                  ))}
                </div>
              ) : null}
            </>
          ) : (
            <Typography.Paragraph type="tertiary" style={{ marginTop: 8 }}>
              {t('projects.review.empty')}
            </Typography.Paragraph>
          )}
          {!selectedReviewer.userDecision &&
          (selectedReviewer.status === 'succeeded' ||
            selectedReviewer.status === 'failed') ? (
            <div style={{ display: 'flex', gap: 8, marginTop: 14 }}>
              <Button
                theme="solid"
                type="primary"
                loading={deciding}
                onClick={() => void decideReview('passed')}
              >
                {t('projects.review.pass')}
              </Button>
              <Button
                loading={deciding}
                onClick={() => void decideReview('changes_requested')}
              >
                {t('projects.review.requestChanges')}
              </Button>
              {selectedReviewer.sessionId ? (
                <Button
                  theme="borderless"
                  onClick={() => void openWorker(selectedReviewer)}
                >
                  {t('projects.openChat')}
                </Button>
              ) : null}
            </div>
          ) : null}
        </div>
      ) : null}

      {project.status === 'active' ? (
        <div style={{ marginTop: 28 }}>
          <Typography.Title heading={5} style={{ marginBottom: 8 }}>
            {t('projects.delegate.title')}
          </Typography.Title>
          <Typography.Paragraph type="tertiary" style={{ marginBottom: 16 }}>
            {t('projects.delegate.guide')}
          </Typography.Paragraph>
          <div className="rp-projects-delegate">
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

function ciStatusKey(status?: string): MessageKey {
  switch (status) {
    case 'success':
      return 'projects.ci.status.success'
    case 'failure':
      return 'projects.ci.status.failure'
    case 'pending':
      return 'projects.ci.status.pending'
    case 'cancelled':
      return 'projects.ci.status.cancelled'
    default:
      return 'projects.ci.status.unknown'
  }
}

function ciTagColor(
  status?: string,
): 'grey' | 'blue' | 'orange' | 'green' | 'red' {
  switch (status) {
    case 'success':
      return 'green'
    case 'failure':
      return 'red'
    case 'pending':
      return 'orange'
    default:
      return 'grey'
  }
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
      <div
        className="rp-projects-meta-value"
        style={
          code ? { fontFamily: 'var(--semi-font-family-code)' } : undefined
        }
      >
        <Typography.Text
          style={
            code ? { fontFamily: 'var(--semi-font-family-code)' } : undefined
          }
        >
          {value}
        </Typography.Text>
      </div>
    </div>
  )
}
