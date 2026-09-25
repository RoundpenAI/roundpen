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
import { assistantsApi, issues, type Issue } from '../api'
import { useT, type MessageKey } from '../i18n'

const ALL = 'all'

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

/** Per the console spec: grey while drafting, blue once a doc exists,
 *  orange while work is underway, green when done, grey when cancelled. */
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

function formatWhen(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

type Snapshot = {
  filter: string
  issues: Issue[]
  error: string | null
}

export function IssuesPage() {
  const t = useT()
  const navigate = useNavigate()

  const [filter, setFilter] = useState<string>(ALL)
  const [reloadToken, setReloadToken] = useState(0)
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
  const [assistantNames, setAssistantNames] = useState<Record<string, string>>(
    {},
  )
  // The snapshot is keyed by filter: rows on screen for an older filter mean
  // the current one is still loading.
  const loading = snapshot?.filter !== filter
  const list = snapshot?.issues ?? []
  const error = snapshot?.error ?? null

  const [createOpen, setCreateOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [title, setTitle] = useState('')
  const [summary, setSummary] = useState('')

  useEffect(() => {
    let cancelled = false
    issues
      .list(filter === ALL ? undefined : filter)
      .then((res) => {
        if (cancelled) return
        setSnapshot({ filter, issues: res.issues ?? [], error: null })
      })
      .catch((e: unknown) => {
        if (cancelled) return
        setSnapshot({
          filter,
          issues: [],
          error: e instanceof Error ? e.message : t('issues.loadFailed'),
        })
      })
    return () => {
      cancelled = true
    }
  }, [filter, t, reloadToken])

  // Names are a display nicety: without them the source column still renders
  // the chat / console origin, so a failed lookup is not an error.
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
      ...ISSUE_STATUSES.map((s) => ({ label: t(statusKey(s)), value: s })),
    ],
    [t],
  )

  const openCreate = () => {
    setTitle('')
    setSummary('')
    setCreateError(null)
    setCreateOpen(true)
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

  const columns: ColumnProps<Issue>[] = useMemo(
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
        render: (status: Issue['status']) => (
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
        // Same shell as the settings sections: an explicit width keeps the
        // page from sizing to its content (and visibly resizing on load).
        width: '100%',
        maxWidth: 960,
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
        <Select
          style={{ width: 180 }}
          value={filter}
          optionList={filterOptions}
          onChange={(v) => setFilter(typeof v === 'string' ? v : ALL)}
        />
        <div style={{ flex: 1 }} />
        <Button
          icon={<IconRefresh />}
          type="tertiary"
          onClick={() => setReloadToken((n) => n + 1)}
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
      ) : (
        <Table
          columns={columns}
          dataSource={list}
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
        width={448}
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
