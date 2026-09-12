import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  Banner,
  Button,
  Spin,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui-19'
import type { ColumnProps } from '@douyinfe/semi-ui-19/lib/es/table'
import {
  templates,
  templateDisplayName,
  type CreateTemplateResult,
  type Template,
} from '../api'
import { PageShell } from '../components/PageShell'
import { TemplateBuildDialog } from '../components/TemplateBuildDialog'
import { TemplateDetailDialog } from '../components/TemplateDetailDialog'
import {
  TemplateCreateDialog,
  type TemplateCreateValues,
} from '../components/TemplateCreateDialog'

function statusTagColor(
  status: string,
): 'green' | 'blue' | 'red' | 'orange' | 'grey' {
  switch (status) {
    case 'ready':
      return 'green'
    case 'building':
      return 'blue'
    case 'error':
      return 'red'
    case 'waiting':
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

function buildActionLabel(tpl: Template): string {
  if (tpl.buildStatus === 'building') return 'Logs'
  if (tpl.buildStatus === 'ready') return 'Rebuild'
  if (canBuild(tpl)) return 'Build'
  return 'Logs'
}

export function TemplatesPage() {
  const [list, setList] = useState<Template[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const [createOpen, setCreateOpen] = useState(false)
  const [createBusy, setCreateBusy] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  const [building, setBuilding] = useState<Template | null>(null)
  const [viewingId, setViewingId] = useState<string | null>(null)

  const load = useCallback(async (opts?: { silent?: boolean }) => {
    if (!opts?.silent) setLoading(true)
    setError(null)
    try {
      setList(await templates.list())
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to list')
    } finally {
      if (!opts?.silent) setLoading(false)
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const readyBases = useMemo(
    () => list.filter((t) => t.buildStatus === 'ready'),
    [list],
  )

  async function onCreate(values: TemplateCreateValues) {
    setCreateBusy(true)
    setCreateError(null)
    try {
      const created: CreateTemplateResult = await templates.create(values)
      setCreateOpen(false)
      await load()
      const row: Template = {
        templateID: created.templateID,
        buildID: created.buildID,
        cpuCount: values.cpuCount,
        memoryMB: values.memoryMB,
        diskSizeMB: 5120,
        public: created.public,
        names: created.names,
        aliases: created.aliases,
        buildStatus: 'waiting',
        envdVersion: '0.0.0-roundpen',
      }
      setBuilding(row)
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : 'create failed')
    } finally {
      setCreateBusy(false)
    }
  }

  const handleBuildDone = useCallback(() => {
    void load({ silent: true })
  }, [load])

  const columns: ColumnProps<Template>[] = useMemo(
    () => [
      {
        title: 'Name',
        dataIndex: 'templateID',
        render: (_: unknown, tpl: Template) => (
          <div>
            <Typography.Text strong>{templateDisplayName(tpl)}</Typography.Text>
            <Typography.Text
              type="tertiary"
              size="small"
              style={{ display: 'block', fontFamily: 'var(--semi-font-family-code)' }}
            >
              {tpl.templateID.slice(0, 8)}…
            </Typography.Text>
          </div>
        ),
      },
      {
        title: 'Status',
        dataIndex: 'buildStatus',
        width: 110,
        render: (_: unknown, tpl: Template) => {
          const status = tpl.buildStatus || 'waiting'
          return (
            <Tag color={statusTagColor(status)} size="small">
              {status}
            </Tag>
          )
        },
      },
      {
        title: 'Resources',
        dataIndex: 'cpuCount',
        width: 160,
        render: (_: unknown, tpl: Template) => (
          <Typography.Text size="small">
            {tpl.cpuCount}c · {tpl.memoryMB}MiB · {tpl.diskSizeMB}MiB
          </Typography.Text>
        ),
      },
      {
        title: 'Usage',
        dataIndex: 'spawnCount',
        width: 140,
        render: (_: unknown, tpl: Template) => (
          <Typography.Text type="tertiary" size="small">
            {tpl.spawnCount ?? 0} spawns
            {tpl.buildCount != null ? ` · ${tpl.buildCount} builds` : ''}
          </Typography.Text>
        ),
      },
      {
        title: 'Updated',
        dataIndex: 'updatedAt',
        width: 160,
        render: (_: unknown, tpl: Template) => (
          <Typography.Text type="tertiary" size="small">
            {formatWhen(tpl.updatedAt)}
          </Typography.Text>
        ),
      },
      {
        title: '',
        dataIndex: 'actions',
        width: 160,
        align: 'right',
        render: (_: unknown, tpl: Template) => (
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 4 }}>
            <Button
              type="tertiary"
              size="small"
              onClick={() => setViewingId(tpl.templateID)}
            >
              View
            </Button>
            <Button
              type="tertiary"
              size="small"
              onClick={() => setBuilding(tpl)}
            >
              {buildActionLabel(tpl)}
            </Button>
          </div>
        ),
      },
    ],
    [],
  )

  return (
    <PageShell
      subtitle="Environment images"
      current="templates"
      maxWidthClass="max-w-4xl"
    >
      <div
        style={{
          display: 'flex',
          gap: 8,
          marginBottom: 24,
          paddingBottom: 24,
          borderBottom: '1px solid var(--semi-color-border)',
        }}
      >
        <Button
          theme="solid"
          type="primary"
          onClick={() => {
            setCreateError(null)
            setCreateOpen(true)
          }}
        >
          New image
        </Button>
        <Button type="tertiary" onClick={() => void load()}>
          Refresh
        </Button>
      </div>

      <Typography.Text type="tertiary" size="small" style={{ display: 'block', marginBottom: 16 }}>
        Customize Agent (OCI) and Browser (qcow2) slot images. Built-ins are seeded
        on startup.
      </Typography.Text>

      {error && (
        <div role="alert" style={{ marginBottom: 16 }}>
          <Banner fullMode={false} type="danger" description={error} closeIcon={null} />
        </div>
      )}

      <Typography.Text
        type="tertiary"
        size="small"
        style={{ display: 'block', marginBottom: 16, lineHeight: 1.6 }}
      >
        Rebuild reuses the build ID when the spec is unchanged; changing base image / RUN /
        start / ready allocates a new build. Optional tags resolve as{' '}
        <Typography.Text
          size="small"
          style={{ fontFamily: 'var(--semi-font-family-code)' }}
        >
          name:tag
        </Typography.Text>
        .
      </Typography.Text>

      {loading ? (
        <div style={{ padding: '24px 0', textAlign: 'center' }}>
          <Spin />
        </div>
      ) : list.length === 0 ? (
        <Typography.Text type="tertiary">No templates registered.</Typography.Text>
      ) : (
        <Table
          columns={columns}
          dataSource={list}
          rowKey="templateID"
          pagination={false}
          size="small"
        />
      )}

      <TemplateCreateDialog
        open={createOpen}
        busy={createBusy}
        error={createError}
        onClose={() => {
          if (!createBusy) setCreateOpen(false)
        }}
        onCreate={onCreate}
      />

      <TemplateDetailDialog
        open={viewingId != null}
        templateId={viewingId}
        onClose={() => setViewingId(null)}
        onSaved={(tpl) => {
          setList((prev) =>
            prev.map((row) => (row.templateID === tpl.templateID ? { ...row, ...tpl } : row)),
          )
          void load({ silent: true })
        }}
        onDeleted={() => {
          setViewingId(null)
          void load()
        }}
        onBuild={(tpl) => {
          setViewingId(null)
          setBuilding(tpl)
        }}
      />

      <TemplateBuildDialog
        open={building != null}
        template={building}
        baseOptions={readyBases}
        onClose={() => setBuilding(null)}
        onDone={handleBuildDone}
      />
    </PageShell>
  )
}

function canBuild(t: Template): boolean {
  return t.buildStatus === 'waiting' || t.buildStatus === 'error'
}
