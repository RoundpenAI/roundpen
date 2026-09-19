import { useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  Banner,
  Button,
  Card,
  Checkbox,
  Input,
  MarkdownRender,
  Modal,
  Select,
  Spin,
  TabPane,
  Tabs,
  Tag,
  TextArea,
  Toast,
  Typography,
} from '@douyinfe/semi-ui-19'
import { IconArrowLeft, IconPlus, IconRefresh } from '@douyinfe/semi-icons'
import {
  assistantsApi,
  issues,
  type Issue,
  type IssueDoc,
  type IssueTask,
} from '../api'
import { useT, type MessageKey } from '../i18n'

type DocKind = IssueDoc['kind']

const DOC_KINDS: DocKind[] = ['spec', 'plan']

const ISSUE_STATUSES: Issue['status'][] = [
  'drafting',
  'specced',
  'planned',
  'in_progress',
  'done',
  'cancelled',
]

function statusKey(status: Issue['status']): MessageKey {
  return `issues.status.${status}`
}

function taskStatusKey(status: IssueTask['status']): MessageKey {
  return `issues.task.status.${status}`
}

function docStatusKey(status: IssueDoc['status']): MessageKey {
  return `issues.doc.${status}`
}

/** Same mapping the list page uses, so the two pages agree on colors. */
function statusTagColor(
  status: Issue['status'],
): 'grey' | 'blue' | 'orange' | 'green' {
  switch (status) {
    case 'specced':
    case 'planned':
      return 'blue'
    case 'in_progress':
      return 'orange'
    case 'done':
      return 'green'
    default:
      return 'grey'
  }
}

function taskTagColor(
  status: IssueTask['status'],
): 'grey' | 'blue' | 'green' | 'red' {
  switch (status) {
    case 'in_progress':
      return 'blue'
    case 'done':
      return 'green'
    case 'blocked':
      return 'red'
    default:
      return 'grey'
  }
}

/** Current is the live revision; draft is not in force; the rest is history. */
function docTagColor(status: IssueDoc['status']): 'grey' | 'green' | 'orange' {
  switch (status) {
    case 'current':
      return 'green'
    case 'draft':
      return 'orange'
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
  key: string
  issue: Issue | null
  docs: IssueDoc[]
  tasks: IssueTask[]
  error: string | null
}

export function IssueDetailPage() {
  const t = useT()
  const { key = '' } = useParams<{ key: string }>()

  const [reloadToken, setReloadToken] = useState(0)
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
  const [assistantNames, setAssistantNames] = useState<Record<string, string>>(
    {},
  )
  const [savingStatus, setSavingStatus] = useState(false)

  const reload = () => setReloadToken((n) => n + 1)
  // Keep the previous snapshot on screen while re-fetching the same issue:
  // writes advance the issue status server-side, so a re-fetch (not a local
  // patch) is what makes the header tell the truth.
  const loading = snapshot?.key !== key
  const issue = snapshot?.key === key ? snapshot.issue : null
  const docs = snapshot?.key === key ? snapshot.docs : []
  const tasks = snapshot?.key === key ? snapshot.tasks : []

  useEffect(() => {
    if (!key) return
    let cancelled = false
    issues
      .get(key)
      .then((res) => {
        if (cancelled) return
        setSnapshot({
          key,
          issue: res.issue,
          docs: res.docs ?? [],
          tasks: res.tasks ?? [],
          error: null,
        })
      })
      .catch((e: unknown) => {
        if (cancelled) return
        setSnapshot({
          key,
          issue: null,
          docs: [],
          tasks: [],
          error: e instanceof Error ? e.message : t('issues.loadFailed'),
        })
      })
    return () => {
      cancelled = true
    }
  }, [key, t, reloadToken])

  // Names are a display nicety: without them the source still links through.
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

  const statusOptions = useMemo(
    () => ISSUE_STATUSES.map((s) => ({ label: t(statusKey(s)), value: s })),
    [t],
  )

  async function changeStatus(next: string) {
    if (!issue || savingStatus) return
    setSavingStatus(true)
    try {
      await issues.update(issue.key, { status: next })
      reload()
    } catch (e) {
      Toast.error(
        e instanceof Error ? e.message : t('issues.updateFailed'),
      )
    } finally {
      setSavingStatus(false)
    }
  }

  if (loading && !snapshot) {
    return (
      <div style={{ padding: '24px 0', textAlign: 'center' }}>
        <Spin />
      </div>
    )
  }

  return (
    <div style={{ padding: '16px 20px', maxWidth: 1280, margin: '0 auto' }}>
      <Link
        to="/issues"
        style={{
          color: 'var(--semi-color-link)',
          fontSize: 12,
          display: 'inline-flex',
          alignItems: 'center',
          gap: 4,
        }}
      >
        <IconArrowLeft size="small" />
        {t('issues.back')}
      </Link>

      {snapshot?.error && (
        <div role="alert" style={{ marginTop: 12 }}>
          <Banner
            fullMode={false}
            type="danger"
            description={snapshot.error}
            closeIcon={null}
          />
        </div>
      )}

      {issue && (
        <>
          <div
            style={{
              display: 'flex',
              flexWrap: 'wrap',
              alignItems: 'center',
              gap: 8,
              marginTop: 12,
            }}
          >
            <Typography.Text
              size="small"
              style={{ fontFamily: 'var(--semi-font-family-code)' }}
            >
              {issue.key}
            </Typography.Text>
            <Tag size="small" color={statusTagColor(issue.status)}>
              {t(statusKey(issue.status))}
            </Tag>
            <div style={{ flex: 1 }} />
            <Button
              icon={<IconRefresh />}
              type="tertiary"
              onClick={reload}
            >
              {t('issues.refresh')}
            </Button>
          </div>

          <Typography.Title heading={3} style={{ margin: '4px 0' }}>
            {issue.title}
          </Typography.Title>
          {issue.summary ? (
            <Typography.Text type="secondary" style={{ display: 'block' }}>
              {issue.summary}
            </Typography.Text>
          ) : null}

          <div
            style={{
              display: 'flex',
              flexWrap: 'wrap',
              alignItems: 'center',
              gap: 16,
              marginTop: 12,
              marginBottom: 16,
            }}
          >
            <span
              style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}
            >
              <Typography.Text type="tertiary" size="small">
                {t('issues.column.status')}
              </Typography.Text>
              <Select
                size="small"
                style={{ width: 150 }}
                value={issue.status}
                disabled={savingStatus}
                aria-label={t('issues.column.status')}
                optionList={statusOptions}
                onChange={(v) => {
                  if (typeof v === 'string') void changeStatus(v)
                }}
              />
            </span>

            <Typography.Text type="tertiary" size="small">
              {t('issues.column.origin')}
              {': '}
              {issue.assistantId ? (
                <>
                  <Link
                    to={`/a/${issue.assistantId}`}
                    style={{ color: 'var(--semi-color-link)' }}
                  >
                    {assistantNames[issue.assistantId] ?? issue.assistantId}
                  </Link>
                  {issue.sessionId ? (
                    <>
                      {' · '}
                      <Link
                        to={`/a/${issue.assistantId}/s/${issue.sessionId}`}
                        style={{ color: 'var(--semi-color-link)' }}
                      >
                        {t('issues.link.session')}
                      </Link>
                    </>
                  ) : null}
                </>
              ) : issue.origin === 'chat' ? (
                t('issues.origin.chat')
              ) : (
                t('issues.origin.console')
              )}
            </Typography.Text>

            <Typography.Text type="tertiary" size="small">
              {t('issues.column.updated')}
              {': '}
              {formatWhen(issue.updatedAt)}
            </Typography.Text>
          </div>

          {/* Two columns that wrap into one stack on narrow screens. */}
          <div
            style={{
              display: 'flex',
              flexWrap: 'wrap',
              alignItems: 'flex-start',
              gap: 16,
            }}
          >
            <div style={{ flex: '3 1 460px', minWidth: 0 }}>
              <DocsPanel
                issueKey={issue.key}
                docs={docs}
                onChanged={reload}
              />
            </div>
            <div style={{ flex: '2 1 320px', minWidth: 0 }}>
              <TasksPanel
                issueKey={issue.key}
                tasks={tasks}
                onChanged={reload}
              />
            </div>
          </div>
        </>
      )}
    </div>
  )
}

function DocsPanel({
  issueKey,
  docs,
  onChanged,
}: {
  issueKey: string
  docs: IssueDoc[]
  onChanged: () => void
}) {
  const t = useT()
  const [kind, setKind] = useState<DocKind>('spec')

  return (
    <Card bodyStyle={{ padding: 16 }}>
      <Tabs
        type="line"
        activeKey={kind}
        onChange={(k) => setKind(k as DocKind)}
      >
        {DOC_KINDS.map((k) => (
          <TabPane
            key={k}
            itemKey={k}
            tab={t(k === 'spec' ? 'issues.tab.spec' : 'issues.tab.plan')}
          />
        ))}
      </Tabs>
      <DocVersion
        // Remount on issue/kind change so the selected version and the body
        // cache never leak across issues.
        key={`${issueKey}:${kind}`}
        issueKey={issueKey}
        kind={kind}
        docs={docs}
        onChanged={onChanged}
      />
    </Card>
  )
}

function DocVersion({
  issueKey,
  kind,
  docs,
  onChanged,
}: {
  issueKey: string
  kind: DocKind
  docs: IssueDoc[]
  onChanged: () => void
}) {
  const t = useT()
  const kindLabel = t(kind === 'spec' ? 'issues.tab.spec' : 'issues.tab.plan')

  // Newest first in the selector; every revision is kept as its own row.
  const versions = useMemo(
    () =>
      docs
        .filter((d) => d.kind === kind)
        .sort((a, b) => b.version - a.version),
    [docs, kind],
  )

  const [picked, setPicked] = useState<string | null>(null)
  const [bodies, setBodies] = useState<Record<string, string>>({})
  const [bodyErrors, setBodyErrors] = useState<Record<string, string>>({})

  const [composing, setComposing] = useState(false)
  const [draft, setDraft] = useState('')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState<string | null>(null)

  const preferred = versions.find((d) => d.status === 'current') ?? versions[0]
  const selectedKey =
    picked && versions.some((d) => d.key === picked) ? picked : preferred?.key
  const selectedDoc = versions.find((d) => d.key === selectedKey)

  // The index omits bodies, so the selected version's markdown is fetched
  // lazily; the cache check keeps this to one request per version.
  useEffect(() => {
    if (!selectedKey) return
    if (bodies[selectedKey] !== undefined) return
    let cancelled = false
    issues
      .getDoc(issueKey, selectedKey)
      .then((res) => {
        if (cancelled) return
        setBodies((prev) => ({
          ...prev,
          [selectedKey]: res.doc.contentMd ?? '',
        }))
      })
      .catch((e: unknown) => {
        if (cancelled) return
        setBodyErrors((prev) => ({
          ...prev,
          [selectedKey]:
            e instanceof Error ? e.message : t('issues.doc.bodyFailed'),
        }))
      })
    return () => {
      cancelled = true
    }
  }, [issueKey, selectedKey, bodies, t])

  async function submit() {
    const content = draft.trim()
    if (!content || saving) return
    setSaving(true)
    setSaveError(null)
    try {
      const { doc } = await issues.writeDoc(issueKey, {
        kind,
        contentMd: content,
        status: 'current',
      })
      // We already hold the body, and the new version is the one to show.
      setBodies((prev) => ({ ...prev, [doc.key]: content }))
      setPicked(doc.key)
      setDraft('')
      setComposing(false)
      onChanged()
    } catch (e) {
      const message =
        e instanceof Error ? e.message : t('issues.doc.writeFailed')
      setSaveError(message)
      Toast.error(message)
    } finally {
      setSaving(false)
    }
  }

  const body = selectedKey ? bodies[selectedKey] : undefined
  const bodyError = selectedKey ? bodyErrors[selectedKey] : undefined

  return (
    <div style={{ marginTop: 12 }}>
      <div
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'center',
          gap: 8,
        }}
      >
        {versions.length > 0 && (
          <>
            <Select
              size="small"
              style={{ width: 110 }}
              value={selectedKey}
              aria-label={t('issues.doc.version', {
                n: selectedDoc?.version ?? '',
              })}
              optionList={versions.map((d) => ({
                label: `v${d.version}`,
                value: d.key,
              }))}
              onChange={(v) => setPicked(typeof v === 'string' ? v : null)}
            />
            {selectedDoc && (
              <Tag size="small" color={docTagColor(selectedDoc.status)}>
                {t(docStatusKey(selectedDoc.status))}
              </Tag>
            )}
          </>
        )}
        <div style={{ flex: 1 }} />
        <Button
          size="small"
          theme="borderless"
          type="primary"
          icon={<IconPlus />}
          onClick={() => {
            setSaveError(null)
            setComposing(true)
          }}
        >
          {t('issues.doc.newVersion')}
        </Button>
      </div>

      {selectedDoc?.title ? (
        <Typography.Text
          type="secondary"
          style={{ display: 'block', marginTop: 8 }}
        >
          {selectedDoc.title}
        </Typography.Text>
      ) : null}

      <div style={{ marginTop: 12 }}>
        {versions.length === 0 ? (
          <Typography.Text type="tertiary">
            {t('issues.doc.empty')}
          </Typography.Text>
        ) : body !== undefined ? (
          body.trim() === '' ? (
            <Typography.Text type="tertiary">
              {t('issues.doc.empty')}
            </Typography.Text>
          ) : (
            <div style={{ overflowX: 'auto', overflowWrap: 'anywhere' }}>
              <MarkdownRender raw={body} format="md" />
            </div>
          )
        ) : bodyError ? (
          <div role="alert">
            <Banner
              fullMode={false}
              type="danger"
              description={bodyError}
              closeIcon={null}
            />
          </div>
        ) : (
          <div style={{ padding: '16px 0', textAlign: 'center' }}>
            <Spin />
          </div>
        )}
      </div>

      <Modal
        title={`${kindLabel} · ${t('issues.doc.newVersion')}`}
        visible={composing}
        onCancel={() => {
          if (!saving) setComposing(false)
        }}
        footer={null}
        maskClosable={!saving}
        closeOnEsc={!saving}
        width={720}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void submit()
          }}
          style={{ display: 'flex', flexDirection: 'column', gap: 16 }}
        >
          <Typography.Text type="tertiary" size="small">
            {t('issues.doc.newVersionHint')}
          </Typography.Text>

          <TextArea
            value={draft}
            onChange={setDraft}
            autosize={{ minRows: 10, maxRows: 20 }}
            placeholder={t('issues.doc.contentPlaceholder')}
            autoFocus
          />

          {saveError && (
            <div role="alert">
              <Banner
                fullMode={false}
                type="danger"
                description={saveError}
                closeIcon={null}
              />
            </div>
          )}

          <div
            style={{
              display: 'flex',
              justifyContent: 'flex-end',
              gap: 8,
            }}
          >
            <Button
              type="tertiary"
              onClick={() => setComposing(false)}
              disabled={saving}
            >
              {t('issues.cancel')}
            </Button>
            <Button
              htmlType="submit"
              theme="solid"
              type="primary"
              loading={saving}
              disabled={!draft.trim()}
            >
              {t('issues.create')}
            </Button>
          </div>
        </form>
      </Modal>
    </div>
  )
}

function TasksPanel({
  issueKey,
  tasks,
  onChanged,
}: {
  issueKey: string
  tasks: IssueTask[]
  onChanged: () => void
}) {
  const t = useT()
  const sorted = useMemo(
    () => [...tasks].sort((a, b) => a.position - b.position),
    [tasks],
  )
  const doneCount = sorted.filter((x) => x.status === 'done').length

  const [busy, setBusy] = useState<string | null>(null)
  const [adding, setAdding] = useState(false)
  const [title, setTitle] = useState('')
  const [detail, setDetail] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)

  // Every write can advance the issue status server-side, so the panel never
  // patches its own copy: it asks the page to re-fetch.
  async function setTaskStatus(task: IssueTask, status: IssueTask['status']) {
    if (busy) return
    setBusy(task.key)
    try {
      await issues.updateTask(task.key, { status })
      onChanged()
    } catch (e) {
      Toast.error(
        e instanceof Error ? e.message : t('issues.task.updateFailed'),
      )
    } finally {
      setBusy(null)
    }
  }

  async function submit() {
    const trimmed = title.trim()
    if (!trimmed || saving) return
    setSaving(true)
    setError(null)
    try {
      // No position: the server appends new tasks at the end of the list.
      await issues.createTask(issueKey, {
        title: trimmed,
        detail: detail.trim() || undefined,
      })
      setTitle('')
      setDetail('')
      setAdding(false)
      onChanged()
    } catch (e) {
      const message =
        e instanceof Error ? e.message : t('issues.task.createFailed')
      setError(message)
      Toast.error(message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Card bodyStyle={{ padding: 16 }}>
      <div
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'center',
          gap: 8,
        }}
      >
        <Typography.Text strong>{t('issues.tasks')}</Typography.Text>
        {sorted.length > 0 && (
          <Typography.Text type="tertiary" size="small">
            {doneCount}/{sorted.length}
          </Typography.Text>
        )}
        <div style={{ flex: 1 }} />
        <Button
          size="small"
          theme="borderless"
          type="primary"
          icon={<IconPlus />}
          onClick={() => {
            setError(null)
            setAdding(true)
          }}
        >
          {t('issues.task.new')}
        </Button>
      </div>

      {adding && (
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void submit()
          }}
          style={{
            display: 'flex',
            flexDirection: 'column',
            gap: 8,
            marginTop: 12,
          }}
        >
          <Input
            value={title}
            onChange={setTitle}
            placeholder={t('issues.field.title')}
            autoFocus
          />
          <TextArea
            value={detail}
            onChange={setDetail}
            autosize={{ minRows: 2, maxRows: 4 }}
            placeholder={t('issues.task.detailPlaceholder')}
          />
          {error && (
            <div role="alert">
              <Banner
                fullMode={false}
                type="danger"
                description={error}
                closeIcon={null}
              />
            </div>
          )}
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
            <Button
              type="tertiary"
              onClick={() => setAdding(false)}
              disabled={saving}
            >
              {t('issues.cancel')}
            </Button>
            <Button
              htmlType="submit"
              theme="solid"
              type="primary"
              loading={saving}
              disabled={!title.trim()}
            >
              {t('issues.create')}
            </Button>
          </div>
        </form>
      )}

      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 12,
          marginTop: 12,
        }}
      >
        {sorted.length === 0 && !adding && (
          <Typography.Text type="tertiary">
            {t('issues.task.empty')}
          </Typography.Text>
        )}

        {sorted.map((task) => {
          const finished =
            task.status === 'done' || task.status === 'cancelled'
          return (
            <div
              key={task.key}
              style={{
                padding: '8px 10px',
                border: '1px solid var(--semi-color-border)',
                borderRadius: 'var(--semi-border-radius-medium)',
                background:
                  task.status === 'blocked'
                    ? 'var(--semi-color-danger-light-default)'
                    : 'transparent',
              }}
            >
              <div
                style={{ display: 'flex', alignItems: 'flex-start', gap: 8 }}
              >
                <Checkbox
                  checked={task.status === 'done'}
                  disabled={busy === task.key || task.status === 'cancelled'}
                  aria-label={task.title}
                  onChange={(e) =>
                    void setTaskStatus(task, e.target.checked ? 'done' : 'todo')
                  }
                />
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div
                    style={{
                      display: 'flex',
                      flexWrap: 'wrap',
                      alignItems: 'center',
                      gap: 8,
                    }}
                  >
                    <Typography.Text
                      size="small"
                      style={{ fontFamily: 'var(--semi-font-family-code)' }}
                    >
                      {task.key}
                    </Typography.Text>
                    <Typography.Text
                      strong={!finished}
                      type={finished ? 'tertiary' : undefined}
                      style={
                        finished
                          ? { textDecoration: 'line-through' }
                          : undefined
                      }
                    >
                      {task.title}
                    </Typography.Text>
                    <Tag size="small" color={taskTagColor(task.status)}>
                      {t(taskStatusKey(task.status))}
                    </Tag>
                  </div>

                  {/* A blocked task must say why: its detail is surfaced even
                      when the rest of the list stays quiet. */}
                  {task.status === 'blocked' ? (
                    <div
                      style={{
                        marginTop: 6,
                        padding: '6px 8px',
                        borderLeft: '3px solid var(--semi-color-danger)',
                        borderRadius: 4,
                        background: 'var(--semi-color-bg-2)',
                      }}
                    >
                      <Typography.Text
                        size="small"
                        type={task.detail ? 'secondary' : 'danger'}
                        style={{ whiteSpace: 'pre-wrap' }}
                      >
                        {task.detail || t('issues.task.blockedHint')}
                      </Typography.Text>
                    </div>
                  ) : task.detail ? (
                    <Typography.Text
                      size="small"
                      type="tertiary"
                      style={{
                        display: 'block',
                        marginTop: 4,
                        whiteSpace: 'pre-wrap',
                      }}
                    >
                      {task.detail}
                    </Typography.Text>
                  ) : null}

                  {task.status === 'cancelled' ? (
                    <Button
                      size="small"
                      theme="borderless"
                      type="tertiary"
                      style={{ marginTop: 4 }}
                      disabled={busy === task.key}
                      onClick={() => void setTaskStatus(task, 'todo')}
                    >
                      {t('issues.task.reopen')}
                    </Button>
                  ) : task.status === 'done' ? null : (
                    <div
                      style={{
                        display: 'flex',
                        flexWrap: 'wrap',
                        gap: 4,
                        marginTop: 4,
                      }}
                    >
                      {task.status !== 'in_progress' && (
                        <Button
                          size="small"
                          theme="borderless"
                          type="primary"
                          disabled={busy === task.key}
                          onClick={() =>
                            void setTaskStatus(task, 'in_progress')
                          }
                        >
                          {t('issues.task.markInProgress')}
                        </Button>
                      )}
                      {task.status !== 'blocked' && (
                        <Button
                          size="small"
                          theme="borderless"
                          type="warning"
                          disabled={busy === task.key}
                          onClick={() => void setTaskStatus(task, 'blocked')}
                        >
                          {t('issues.task.markBlocked')}
                        </Button>
                      )}
                      <Button
                        size="small"
                        theme="borderless"
                        type="danger"
                        disabled={busy === task.key}
                        onClick={() => void setTaskStatus(task, 'cancelled')}
                      >
                        {t('issues.task.markCancelled')}
                      </Button>
                    </div>
                  )}
                </div>
              </div>
            </div>
          )
        })}
      </div>
    </Card>
  )
}
