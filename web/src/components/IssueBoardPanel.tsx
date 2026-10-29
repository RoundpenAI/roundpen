import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Banner,
  Button,
  Input,
  Modal,
  Select,
  Spin,
  Tag,
  TextArea,
  Toast,
  Typography,
} from '@douyinfe/semi-ui-19'
import { IconRefresh } from '@douyinfe/semi-icons'
import {
  assistantsApi,
  allowedTargets,
  canTransition,
  ISSUE_BOARD_ORDER,
  issues,
  type Issue,
  type IssueStatus,
  type ProjectWorker,
} from '../api'
import { useT, type MessageKey } from '../i18n'

function statusKey(status: IssueStatus): MessageKey {
  return `issues.status.${status}`
}

function workerStatusColor(
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

type Snapshot = {
  issues: Issue[]
  error: string | null
}

export type IssueBoardPanelProps = {
  /** Board is scoped to this project; creates attach projectId. */
  projectId: string
  /** Workers on this project — show active WRK tags on matching issue cards. */
  workers?: ProjectWorker[]
  /** Compact chrome for embedding under a project detail page. */
  embedded?: boolean
}

export function IssueBoardPanel({
  projectId,
  workers,
  embedded = false,
}: IssueBoardPanelProps) {
  const t = useT()
  const navigate = useNavigate()

  const [showTerminal, setShowTerminal] = useState(false)
  const [reloadToken, setReloadToken] = useState(0)
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
  const [assistantNames, setAssistantNames] = useState<Record<string, string>>(
    {},
  )
  const [dragKey, setDragKey] = useState<string | null>(null)
  const [dragFrom, setDragFrom] = useState<IssueStatus | null>(null)
  const [dropTarget, setDropTarget] = useState<IssueStatus | null>(null)
  const [moving, setMoving] = useState(false)

  const [createOpen, setCreateOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [title, setTitle] = useState('')
  const [summary, setSummary] = useState('')

  const loading = snapshot === null
  const list = snapshot?.issues ?? []
  const error = snapshot?.error ?? null

  useEffect(() => {
    let cancelled = false
    setSnapshot(null)
    issues
      .board(projectId)
      .then((res) => {
        if (cancelled) return
        const flat: Issue[] = []
        for (const col of res.columns ?? []) {
          for (const it of col.issues ?? []) flat.push(it)
        }
        setSnapshot({ issues: flat, error: null })
      })
      .catch((e: unknown) => {
        if (cancelled) return
        setSnapshot({
          issues: [],
          error: e instanceof Error ? e.message : t('issues.loadFailed'),
        })
      })
    return () => {
      cancelled = true
    }
  }, [t, reloadToken, projectId])

  useEffect(() => {
    let cancelled = false
    assistantsApi
      .list()
      .then((res) => {
        if (cancelled) return
        const map: Record<string, string> = {}
        for (const a of res.assistants ?? []) map[a.id] = a.name
        setAssistantNames(map)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [])

  // Embedded project boards poll so claim_owner / WRK tags catch work-pulse advances.
  useEffect(() => {
    if (!embedded) return
    const id = window.setInterval(() => {
      setReloadToken((n) => n + 1)
    }, 60_000)
    return () => window.clearInterval(id)
  }, [embedded])

  const workersByIssueId = useMemo(() => {
    const map = new Map<string, ProjectWorker[]>()
    for (const w of workers ?? []) {
      if (!w.issueId) continue
      const bucket = map.get(w.issueId) ?? []
      bucket.push(w)
      map.set(w.issueId, bucket)
    }
    return map
  }, [workers])

  const columnsByStatus = useMemo(() => {
    const map = new Map<IssueStatus, Issue[]>()
    for (const s of ISSUE_BOARD_ORDER) map.set(s, [])
    for (const it of list) {
      const bucket = map.get(it.status)
      if (bucket) bucket.push(it)
      else map.set(it.status, [it])
    }
    return map
  }, [list])

  const visibleColumns = useMemo(() => {
    return ISSUE_BOARD_ORDER.filter((s) => {
      if (s === 'closed' || s === 'cancelled') return showTerminal
      return true
    })
  }, [showTerminal])

  function ownerLabel(owner: string | undefined): string | null {
    if (!owner) return null
    if (owner === 'human') return t('issues.owner.human')
    if (owner === 'sysadmin') return t('issues.owner.sysadmin')
    return assistantNames[owner] ?? owner.slice(0, 8)
  }

  async function submitCreate() {
    const trimmed = title.trim()
    if (!trimmed || creating) return
    setCreating(true)
    setCreateError(null)
    try {
      const { issue } = await issues.create({
        title: trimmed,
        summary: summary.trim() || undefined,
        projectId,
      })
      setCreateOpen(false)
      navigate(`/issues/${issue.key}`)
    } catch (e) {
      const message = e instanceof Error ? e.message : t('issues.createFailed')
      setCreateError(message)
      Toast.error(message)
    } finally {
      setCreating(false)
    }
  }

  async function moveIssue(key: string, to: IssueStatus) {
    if (moving) return
    setMoving(true)
    try {
      const { issue } = await issues.transition(key, to)
      setSnapshot((prev) => {
        if (!prev) return prev
        return {
          ...prev,
          issues: prev.issues.map((it) => (it.key === key ? issue : it)),
        }
      })
    } catch (e) {
      const message =
        e instanceof Error ? e.message : t('issues.transitionFailed')
      Toast.error(message)
    } finally {
      setMoving(false)
      setDragKey(null)
      setDragFrom(null)
      setDropTarget(null)
    }
  }

  return (
    <div style={{ width: '100%' }}>
      <div
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          gap: 8,
          alignItems: 'center',
          marginBottom: 12,
        }}
      >
        {embedded ? (
          <Typography.Title heading={5} style={{ margin: 0, marginRight: 8 }}>
            {t('projects.issues.title')}
          </Typography.Title>
        ) : null}
        <Button type="tertiary" onClick={() => setShowTerminal((v) => !v)}>
          {showTerminal
            ? t('issues.board.hideTerminal')
            : t('issues.board.showTerminal')}
        </Button>
        <div style={{ flex: 1 }} />
        <Button
          icon={<IconRefresh />}
          type="tertiary"
          onClick={() => {
            setSnapshot(null)
            setReloadToken((n) => n + 1)
          }}
        >
          {t('issues.refresh')}
        </Button>
        <Button
          theme="solid"
          type="primary"
          onClick={() => {
            setTitle('')
            setSummary('')
            setCreateError(null)
            setCreateOpen(true)
          }}
        >
          {t('issues.new')}
        </Button>
      </div>

      {embedded ? (
        <Typography.Paragraph type="tertiary" style={{ marginBottom: 12 }}>
          {t('projects.issues.guide')}
        </Typography.Paragraph>
      ) : null}

      {error ? (
        <div role="alert" style={{ marginBottom: 16 }}>
          <Banner
            fullMode={false}
            type="danger"
            description={error}
            closeIcon={null}
          />
        </div>
      ) : null}

      {loading ? (
        <div style={{ padding: '24px 0', textAlign: 'center' }}>
          <Spin />
        </div>
      ) : (
        <div
          style={{
            display: 'flex',
            gap: 10,
            overflowX: 'auto',
            paddingBottom: 12,
            alignItems: 'flex-start',
          }}
        >
          {visibleColumns.map((status) => {
            const cards = columnsByStatus.get(status) ?? []
            const accepts =
              dragFrom != null &&
              canTransition(dragFrom, status) &&
              dragFrom !== status
            const highlight = dropTarget === status && accepts
            return (
              <div
                key={status}
                onDragOver={(e) => {
                  if (!dragFrom || !canTransition(dragFrom, status)) return
                  e.preventDefault()
                  setDropTarget(status)
                }}
                onDragLeave={() => {
                  if (dropTarget === status) setDropTarget(null)
                }}
                onDrop={(e) => {
                  e.preventDefault()
                  if (!dragKey || !dragFrom) return
                  if (
                    !canTransition(dragFrom, status) ||
                    dragFrom === status
                  ) {
                    setDragKey(null)
                    setDragFrom(null)
                    setDropTarget(null)
                    return
                  }
                  void moveIssue(dragKey, status)
                }}
                style={{
                  minWidth: 220,
                  maxWidth: 260,
                  flex: '0 0 220px',
                  background: highlight
                    ? 'var(--semi-color-primary-light-default)'
                    : 'var(--semi-color-fill-0)',
                  borderRadius: 8,
                  padding: 8,
                  border: highlight
                    ? '1px solid var(--semi-color-primary)'
                    : '1px solid transparent',
                  minHeight: 120,
                }}
              >
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 6,
                    marginBottom: 8,
                    padding: '0 4px',
                  }}
                >
                  <Typography.Text strong size="small">
                    {t(statusKey(status))}
                  </Typography.Text>
                  <Typography.Text type="tertiary" size="small">
                    {cards.length}
                  </Typography.Text>
                </div>
                {cards.length === 0 ? (
                  <Typography.Text
                    type="tertiary"
                    size="small"
                    style={{ display: 'block', padding: '12px 4px' }}
                  >
                    {accepts
                      ? t('issues.board.dropHint')
                      : list.length === 0 && status === 'new'
                        ? t('projects.issues.empty')
                        : t('issues.board.emptyColumn')}
                  </Typography.Text>
                ) : (
                  cards.map((it) => {
                    const owner = ownerLabel(it.claimOwner)
                    const linked = workersByIssueId.get(it.id) ?? []
                    const activeWorkers = linked.filter(
                      (w) =>
                        w.status === 'queued' || w.status === 'running',
                    )
                    return (
                      <div
                        key={it.key}
                        draggable={!moving}
                        onDragStart={() => {
                          setDragKey(it.key)
                          setDragFrom(it.status)
                        }}
                        onDragEnd={() => {
                          setDragKey(null)
                          setDragFrom(null)
                          setDropTarget(null)
                        }}
                        onClick={() => navigate(`/issues/${it.key}`)}
                        style={{
                          background: 'var(--semi-color-bg-1)',
                          borderRadius: 6,
                          padding: '10px 10px 8px',
                          marginBottom: 8,
                          cursor: 'grab',
                          opacity: dragKey === it.key ? 0.6 : 1,
                          boxShadow: '0 1px 2px rgba(0,0,0,0.06)',
                        }}
                      >
                        <Typography.Text
                          size="small"
                          style={{
                            fontFamily: 'var(--semi-font-family-code)',
                            display: 'block',
                          }}
                          type="tertiary"
                        >
                          {it.key}
                        </Typography.Text>
                        <Typography.Text
                          strong
                          size="small"
                          style={{ display: 'block', marginTop: 2 }}
                        >
                          {it.title}
                        </Typography.Text>
                        {it.summary ? (
                          <Typography.Text
                            type="tertiary"
                            size="small"
                            ellipsis={{ rows: 2 }}
                            style={{ display: 'block', marginTop: 4 }}
                          >
                            {it.summary}
                          </Typography.Text>
                        ) : null}
                        {it.updatedAt ? (
                          <Typography.Text
                            type="tertiary"
                            size="small"
                            style={{ display: 'block', marginTop: 4 }}
                          >
                            {t('issues.column.updated')}:{' '}
                            {new Date(it.updatedAt).toLocaleString()}
                          </Typography.Text>
                        ) : null}
                        <div
                          style={{
                            display: 'flex',
                            flexWrap: 'wrap',
                            gap: 4,
                            marginTop: 8,
                          }}
                        >
                          {owner ? (
                            <Tag size="small" color="blue">
                              {t('issues.owner.label')}: {owner}
                            </Tag>
                          ) : null}
                          {activeWorkers.map((w) => {
                            const empty =
                              w.status === 'running' &&
                              (w.messageCount ?? 0) === 0
                            return (
                              <Tag
                                key={w.id}
                                size="small"
                                color={
                                  empty ? 'red' : workerStatusColor(w.status)
                                }
                              >
                                {w.key}
                                {w.status === 'running' && empty
                                  ? ` · ${t('projects.worker.progress.noMsgs')}`
                                  : ''}
                              </Tag>
                            )
                          })}
                        </div>
                        <div
                          style={{ marginTop: 8 }}
                          onClick={(e) => e.stopPropagation()}
                        >
                          <Select
                            size="small"
                            placeholder={t('issues.board.moveTo')}
                            style={{ width: '100%' }}
                            disabled={moving}
                            optionList={allowedTargets(it.status).map((s) => ({
                              label: t(statusKey(s)),
                              value: s,
                            }))}
                            onChange={(v) => {
                              if (typeof v === 'string') {
                                void moveIssue(it.key, v as IssueStatus)
                              }
                            }}
                          />
                        </div>
                      </div>
                    )
                  })
                )}
              </div>
            )
          })}
        </div>
      )}

      <Modal
        title={t('issues.new')}
        visible={createOpen}
        onCancel={() => {
          if (!creating) setCreateOpen(false)
        }}
        footer={null}
        maskClosable={!creating}
        closeOnEsc={!creating}
        width={480}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void submitCreate()
          }}
          style={{ display: 'flex', flexDirection: 'column', gap: 16 }}
        >
          <div>
            <Typography.Text
              size="small"
              type="tertiary"
              style={{ display: 'block', marginBottom: 4 }}
            >
              {t('issues.field.title')}
            </Typography.Text>
            <Input
              value={title}
              onChange={setTitle}
              aria-label={t('issues.field.title')}
              autoFocus
            />
          </div>
          <div>
            <Typography.Text
              size="small"
              type="tertiary"
              style={{ display: 'block', marginBottom: 4 }}
            >
              {t('issues.field.summary')}
            </Typography.Text>
            <TextArea
              value={summary}
              onChange={setSummary}
              autosize={{ minRows: 3, maxRows: 6 }}
              placeholder={t('issues.field.summaryHint')}
            />
          </div>
          <Typography.Text type="tertiary" size="small">
            {t('projects.issues.createScoped')}
          </Typography.Text>
          {createError ? (
            <Banner
              fullMode={false}
              type="danger"
              description={createError}
              closeIcon={null}
            />
          ) : null}
          <div
            style={{
              display: 'flex',
              justifyContent: 'flex-end',
              gap: 8,
            }}
          >
            <Button
              type="tertiary"
              onClick={() => setCreateOpen(false)}
              disabled={creating}
            >
              {t('issues.cancel')}
            </Button>
            <Button
              htmlType="submit"
              theme="solid"
              type="primary"
              loading={creating}
              disabled={!title.trim()}
            >
              {t('issues.create')}
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  )
}
