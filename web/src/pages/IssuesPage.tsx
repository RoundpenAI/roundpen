import { useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Banner,
  Button,
  Input,
  Modal,
  Select,
  Spin,
  Table,
  Tag,
  TextArea,
  Toast,
  Typography,
} from '@douyinfe/semi-ui-19'
import { IconRefresh } from '@douyinfe/semi-icons'
import type { ColumnProps } from '@douyinfe/semi-ui-19/lib/es/table'
import {
  assistantsApi,
  allowedTargets,
  canTransition,
  ISSUE_BOARD_ORDER,
  issues,
  type Issue,
  type IssueStatus,
} from '../api'
import { useT, type MessageKey } from '../i18n'

const ALL = 'all'

function statusKey(status: IssueStatus): MessageKey {
  return `issues.status.${status}`
}

function statusTagColor(
  status: IssueStatus,
): 'grey' | 'blue' | 'orange' | 'green' | 'red' | 'amber' {
  switch (status) {
    case 'spec_review':
    case 'needs_discussion':
    case 'triaged':
      return 'blue'
    case 'claimed':
    case 'running':
      return 'orange'
    case 'verified':
    case 'closed':
      return 'green'
    case 'failed':
    case 'needs_review':
      return 'red'
    case 'cancelled':
      return 'grey'
    default:
      return 'grey'
  }
}

function formatWhen(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

type Snapshot = {
  issues: Issue[]
  error: string | null
}

export function IssuesPage() {
  const t = useT()
  const navigate = useNavigate()

  const [view, setView] = useState<'board' | 'list'>('board')
  const [filter, setFilter] = useState<string>(ALL)
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

  const loading = snapshot === null
  const list = snapshot?.issues ?? []
  const error = snapshot?.error ?? null

  const [createOpen, setCreateOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [title, setTitle] = useState('')
  const [summary, setSummary] = useState('')
  const [templateId, setTemplateId] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    issues
      .board()
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
  }, [t, reloadToken])

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

  const filterOptions = useMemo(
    () => [
      { label: t('issues.filter.all'), value: ALL },
      ...ISSUE_BOARD_ORDER.map((s) => ({ label: t(statusKey(s)), value: s })),
    ],
    [t],
  )

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

  const filteredList = useMemo(() => {
    if (filter === ALL) return list
    return list.filter((it) => it.status === filter)
  }, [list, filter])

  const templates: {
    id: string
    name: MessageKey
    hint: MessageKey
    title: MessageKey
    summary: MessageKey
  }[] = [
    {
      id: 'change',
      name: 'issues.template.change',
      hint: 'issues.template.changeHint',
      title: 'issues.template.changeTitle',
      summary: 'issues.template.changeSummary',
    },
    {
      id: 'investigate',
      name: 'issues.template.investigate',
      hint: 'issues.template.investigateHint',
      title: 'issues.template.investigateTitle',
      summary: 'issues.template.investigateSummary',
    },
    {
      id: 'process',
      name: 'issues.template.process',
      hint: 'issues.template.processHint',
      title: 'issues.template.processTitle',
      summary: 'issues.template.processSummary',
    },
  ]

  const openCreate = () => {
    setTitle('')
    setSummary('')
    setTemplateId(null)
    setCreateError(null)
    setCreateOpen(true)
  }

  const applyTemplate = (id: string) => {
    const tpl = templates.find((item) => item.id === id)
    if (!tpl) return
    setTemplateId(id)
    setTitle(t(tpl.title))
    setSummary(t(tpl.summary))
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

  const tableColumns: ColumnProps<Issue>[] = useMemo(
    () => [
      {
        title: t('issues.column.key'),
        dataIndex: 'key',
        width: 110,
        render: (_: unknown, it: Issue) => (
          <Typography.Text
            size="small"
            style={{ fontFamily: 'var(--semi-font-family-code)' }}
          >
            {it.key}
          </Typography.Text>
        ),
      },
      {
        title: t('issues.column.title'),
        dataIndex: 'title',
        render: (_: unknown, it: Issue) => (
          <div>
            <Typography.Text strong>{it.title}</Typography.Text>
            {it.summary ? (
              <Typography.Text
                type="tertiary"
                size="small"
                style={{ display: 'block' }}
              >
                {it.summary}
              </Typography.Text>
            ) : null}
          </div>
        ),
      },
      {
        title: t('issues.column.status'),
        dataIndex: 'status',
        width: 140,
        render: (status: IssueStatus) => (
          <Tag size="small" color={statusTagColor(status)}>
            {t(statusKey(status))}
          </Tag>
        ),
      },
      {
        title: t('issues.column.origin'),
        dataIndex: 'assistantId',
        width: 160,
        render: (_: unknown, it: Issue) => {
          const name = it.assistantId
            ? assistantNames[it.assistantId]
            : undefined
          return (
            <Typography.Text type="tertiary" size="small">
              {name ??
                (it.origin === 'chat'
                  ? t('issues.origin.chat')
                  : t('issues.origin.console'))}
            </Typography.Text>
          )
        },
      },
      {
        title: t('issues.column.updated'),
        dataIndex: 'updatedAt',
        width: 180,
        render: (_: unknown, it: Issue) => (
          <Typography.Text type="tertiary" size="small">
            {formatWhen(it.updatedAt)}
          </Typography.Text>
        ),
      },
    ],
    [t, assistantNames],
  )

  return (
    <div
      style={{
        padding: '16px 20px',
        width: '100%',
        maxWidth: view === 'board' ? 1400 : 960,
        margin: '0 auto',
        boxSizing: 'border-box',
      }}
    >
      <Typography.Title heading={3} style={{ margin: '0 0 4px' }}>
        {t('issues.title')}
      </Typography.Title>

      <div
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          gap: 8,
          alignItems: 'center',
          marginTop: 16,
          marginBottom: 12,
        }}
      >
        <Button
          theme={view === 'board' ? 'solid' : 'light'}
          type={view === 'board' ? 'primary' : 'tertiary'}
          onClick={() => setView('board')}
        >
          {t('issues.view.board')}
        </Button>
        <Button
          theme={view === 'list' ? 'solid' : 'light'}
          type={view === 'list' ? 'primary' : 'tertiary'}
          onClick={() => setView('list')}
        >
          {t('issues.view.list')}
        </Button>
        {view === 'list' ? (
          <Select
            style={{ width: 180 }}
            value={filter}
            optionList={filterOptions}
            onChange={(v) => setFilter(typeof v === 'string' ? v : ALL)}
          />
        ) : (
          <Button type="tertiary" onClick={() => setShowTerminal((v) => !v)}>
            {showTerminal
              ? t('issues.board.hideTerminal')
              : t('issues.board.showTerminal')}
          </Button>
        )}
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
        <Button theme="solid" type="primary" onClick={openCreate}>
          {t('issues.new')}
        </Button>
      </div>

      {error && (
        <div role="alert" style={{ marginBottom: 16 }}>
          <Banner
            fullMode={false}
            type="danger"
            description={error}
            closeIcon={null}
          />
        </div>
      )}

      {loading ? (
        <div style={{ padding: '24px 0', textAlign: 'center' }}>
          <Spin />
        </div>
      ) : view === 'board' ? (
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
              dragFrom != null && canTransition(dragFrom, status) && dragFrom !== status
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
                  if (!canTransition(dragFrom, status) || dragFrom === status) {
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
                      : t('issues.board.emptyColumn')}
                  </Typography.Text>
                ) : (
                  cards.map((it) => (
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
                  ))
                )}
              </div>
            )
          })}
        </div>
      ) : (
        <Table
          columns={tableColumns}
          dataSource={filteredList}
          rowKey="key"
          pagination={false}
          size="small"
          empty={
            <div style={{ padding: '32px 0', textAlign: 'center' }}>
              <Typography.Text type="tertiary">
                {filter === ALL ? t('issues.empty') : t('issues.filter.empty')}
              </Typography.Text>
            </div>
          }
          onRow={(record) =>
            record
              ? {
                  onClick: () => navigate(`/issues/${record.key}`),
                  style: { cursor: 'pointer' },
                }
              : {}
          }
        />
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
        width={520}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void submitCreate()
          }}
          style={{ display: 'flex', flexDirection: 'column', gap: 16 }}
        >
          <Typography.Text type="tertiary" size="small">
            {t('issues.create.guide')}
          </Typography.Text>
          <div>
            <Typography.Text
              size="small"
              type="tertiary"
              style={{ display: 'block', marginBottom: 8 }}
            >
              {t('issues.create.templates')}
            </Typography.Text>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
              {templates.map((tpl) => (
                <Button
                  key={tpl.id}
                  htmlType="button"
                  theme={templateId === tpl.id ? 'solid' : 'light'}
                  type={templateId === tpl.id ? 'primary' : 'tertiary'}
                  onClick={() => applyTemplate(tpl.id)}
                  style={{
                    height: 'auto',
                    justifyContent: 'flex-start',
                    textAlign: 'left',
                    whiteSpace: 'normal',
                    padding: '8px 12px',
                  }}
                >
                  <span style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
                    <Typography.Text strong>{t(tpl.name)}</Typography.Text>
                    <Typography.Text type="tertiary" size="small">
                      {t(tpl.hint)}
                    </Typography.Text>
                  </span>
                </Button>
              ))}
            </div>
          </div>
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

          {createError && (
            <div role="alert">
              <Banner
                fullMode={false}
                type="danger"
                description={createError}
                closeIcon={null}
              />
            </div>
          )}

          <div
            style={{
              display: 'flex',
              justifyContent: 'flex-end',
              gap: 8,
              marginTop: 8,
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
