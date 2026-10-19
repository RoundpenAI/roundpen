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
  Toast,
  Typography,
} from '@douyinfe/semi-ui-19'
import { IconRefresh } from '@douyinfe/semi-icons'
import type { ColumnProps } from '@douyinfe/semi-ui-19/lib/es/table'
import { projects, type Project } from '../api'
import { useT, type MessageKey } from '../i18n'
import { useMatchMedia } from '../lib/useMatchMedia'

const ALL = 'all'

const PROJECT_STATUSES: Project['status'][] = ['active', 'archived']

function statusKey(status: Project['status']): MessageKey {
  return `projects.status.${status}`
}

function formatWhen(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

function slugify(name: string): string {
  return name
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .replace(/-{2,}/g, '-')
}

type Snapshot = {
  filter: string
  projects: Project[]
  error: string | null
}

export function ProjectsPage() {
  const t = useT()
  const navigate = useNavigate()
  const narrow = useMatchMedia('(max-width: 767px)')

  const [filter, setFilter] = useState<string>(ALL)
  const [reloadToken, setReloadToken] = useState(0)
  const [snapshot, setSnapshot] = useState<Snapshot | null>(null)
  const loading = snapshot?.filter !== filter
  const list = snapshot?.projects ?? []
  const error = snapshot?.error ?? null

  const [createOpen, setCreateOpen] = useState(false)
  const [creating, setCreating] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [slug, setSlug] = useState('')
  const [slugTouched, setSlugTouched] = useState(false)
  const [repoUrl, setRepoUrl] = useState('')

  useEffect(() => {
    let cancelled = false
    projects
      .list(filter === ALL ? undefined : filter)
      .then((res) => {
        if (cancelled) return
        setSnapshot({ filter, projects: res.projects ?? [], error: null })
      })
      .catch((e: unknown) => {
        if (cancelled) return
        setSnapshot({
          filter,
          projects: [],
          error: e instanceof Error ? e.message : t('projects.loadFailed'),
        })
      })
    return () => {
      cancelled = true
    }
  }, [filter, t, reloadToken])

  const filterOptions = useMemo(
    () => [
      { label: t('projects.filter.all'), value: ALL },
      ...PROJECT_STATUSES.map((s) => ({ label: t(statusKey(s)), value: s })),
    ],
    [t],
  )

  const openCreate = () => {
    setName('')
    setSlug('')
    setSlugTouched(false)
    setRepoUrl('')
    setCreateError(null)
    setCreateOpen(true)
  }

  async function submitCreate() {
    const trimmed = name.trim()
    if (!trimmed || creating) return
    setCreating(true)
    setCreateError(null)
    try {
      const body: {
        name: string
        slug?: string
        repoUrl?: string
      } = { name: trimmed }
      const s = slug.trim()
      if (s) body.slug = s
      const url = repoUrl.trim()
      if (url) body.repoUrl = url
      const { project } = await projects.create(body)
      setCreateOpen(false)
      navigate(`/projects/${project.key}`)
    } catch (e) {
      const message =
        e instanceof Error ? e.message : t('projects.createFailed')
      setCreateError(message)
      Toast.error(message)
    } finally {
      setCreating(false)
    }
  }

  const columns: ColumnProps<Project>[] = useMemo(() => {
    const nameCol: ColumnProps<Project> = {
      title: t('projects.column.name'),
      dataIndex: 'name',
      render: (_: unknown, row: Project) => {
        if (!narrow) {
          return <Typography.Text strong>{row.name}</Typography.Text>
        }
        return (
          <div className="rp-projects-cell">
            <div className="rp-projects-cell-title">
              <Typography.Text strong>{row.name}</Typography.Text>
              <Tag
                color={row.status === 'active' ? 'green' : 'grey'}
                size="small"
              >
                {t(statusKey(row.status))}
              </Tag>
            </div>
            <div className="rp-projects-cell-meta">
              {row.key}
              {' · '}
              {formatWhen(row.updatedAt)}
            </div>
            <div className="rp-projects-cell-path">{row.repoPath}</div>
          </div>
        )
      },
    }
    if (narrow) return [nameCol]
    return [
      {
        title: t('projects.column.key'),
        dataIndex: 'key',
        width: 110,
        render: (_: unknown, row: Project) => (
          <Typography.Text
            size="small"
            style={{ fontFamily: 'var(--semi-font-family-code)' }}
          >
            {row.key}
          </Typography.Text>
        ),
      },
      nameCol,
      {
        title: t('projects.column.path'),
        dataIndex: 'repoPath',
        render: (_: unknown, row: Project) => (
          <Typography.Text
            size="small"
            type="tertiary"
            style={{ fontFamily: 'var(--semi-font-family-code)' }}
          >
            {row.repoPath}
          </Typography.Text>
        ),
      },
      {
        title: t('projects.column.status'),
        dataIndex: 'status',
        width: 110,
        render: (_: unknown, row: Project) => (
          <Tag color={row.status === 'active' ? 'green' : 'grey'} size="small">
            {t(statusKey(row.status))}
          </Tag>
        ),
      },
      {
        title: t('projects.column.updated'),
        dataIndex: 'updatedAt',
        width: 180,
        render: (_: unknown, row: Project) => (
          <Typography.Text size="small" type="tertiary">
            {formatWhen(row.updatedAt)}
          </Typography.Text>
        ),
      },
    ]
  }, [narrow, t])

  return (
    <div className="rp-projects">
      <div className="rp-projects-head rp-projects-head-stack">
        <Typography.Title heading={3} className="rp-projects-title">
          {t('projects.title')}
        </Typography.Title>
        <div className="rp-projects-head-actions">
          <Select
            value={filter}
            onChange={(v) => setFilter(String(v))}
            optionList={filterOptions}
            style={{ width: 160 }}
          />
          <Button
            icon={<IconRefresh />}
            onClick={() => setReloadToken((n) => n + 1)}
            aria-label={t('projects.refresh')}
          />
          <Button theme="solid" type="primary" onClick={openCreate}>
            {t('projects.create')}
          </Button>
        </div>
      </div>

      <Typography.Paragraph type="tertiary" style={{ marginBottom: 16 }}>
        {t('projects.guide')}
      </Typography.Paragraph>

      {error ? (
        <Banner
          type="danger"
          description={error}
          closeIcon={null}
          style={{ marginBottom: 16 }}
        />
      ) : null}

      {loading && !snapshot ? (
        <div style={{ padding: 48, textAlign: 'center' }}>
          <Spin />
        </div>
      ) : (
        <Table
          className={
            narrow
              ? 'rp-projects-table rp-projects-table-narrow'
              : 'rp-projects-table'
          }
          rowKey="id"
          columns={columns}
          dataSource={list}
          pagination={false}
          loading={loading}
          empty={t('projects.empty')}
          onRow={(record) => ({
            style: { cursor: 'pointer' },
            onClick: () => {
              if (record) navigate(`/projects/${record.key}`)
            },
          })}
        />
      )}

      <Modal
        title={t('projects.createTitle')}
        visible={createOpen}
        onCancel={() => setCreateOpen(false)}
        onOk={() => void submitCreate()}
        okText={t('projects.create')}
        cancelText={t('projects.cancel')}
        confirmLoading={creating}
        okButtonProps={{ disabled: !name.trim() }}
      >
        <Typography.Paragraph type="tertiary" style={{ marginBottom: 12 }}>
          {t('projects.create.guide')}
        </Typography.Paragraph>
        {createError ? (
          <Banner
            type="danger"
            description={createError}
            closeIcon={null}
            style={{ marginBottom: 12 }}
          />
        ) : null}
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          <div>
            <Typography.Text size="small">{t('projects.field.name')}</Typography.Text>
            <Input
              value={name}
              onChange={(v) => {
                setName(v)
                if (!slugTouched) setSlug(slugify(v))
              }}
              placeholder={t('projects.field.namePlaceholder')}
              autoFocus
            />
          </div>
          <div>
            <Typography.Text size="small">{t('projects.field.slug')}</Typography.Text>
            <Input
              value={slug}
              onChange={(v) => {
                setSlugTouched(true)
                setSlug(v)
              }}
              placeholder={t('projects.field.slugPlaceholder')}
            />
            <Typography.Text size="small" type="tertiary">
              {t('projects.field.slugHint', {
                path: slug.trim()
                  ? `projects/${slug.trim()}`
                  : 'projects/<slug>',
              })}
            </Typography.Text>
          </div>
          <div>
            <Typography.Text size="small">
              {t('projects.field.repoUrl')}
            </Typography.Text>
            <Input
              value={repoUrl}
              onChange={setRepoUrl}
              placeholder={t('projects.field.repoUrlPlaceholder')}
            />
            <Typography.Text size="small" type="tertiary">
              {t('projects.field.repoUrlHint')}
            </Typography.Text>
          </div>
        </div>
      </Modal>
    </div>
  )
}
