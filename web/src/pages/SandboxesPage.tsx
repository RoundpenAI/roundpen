import { useCallback, useEffect, useMemo, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import {
  Banner,
  Button,
  Popconfirm,
  Select,
  Space,
  Spin,
  Table,
  Tag,
  Typography,
} from '@douyinfe/semi-ui-19'
import type { ColumnProps } from '@douyinfe/semi-ui-19/lib/es/table'
import { sandboxes, SUGGESTED_CATEGORIES, type Sandbox } from '../api'
import {
  SandboxCreateDialog,
  type SandboxCreateValues,
} from '../components/SandboxCreateDialog'
import {
  SandboxEditDialog,
  type SandboxEditValues,
} from '../components/SandboxEditDialog'
import { PageShell } from '../components/PageShell'

function stateTagColor(state: string | undefined): 'green' | 'orange' | 'grey' {
  if (state === 'running') return 'green'
  if (state === 'stopped') return 'orange'
  return 'grey'
}

export function SandboxesPage() {
  const navigate = useNavigate()
  const [list, setList] = useState<Sandbox[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [filterCategory, setFilterCategory] = useState('')

  const [createOpen, setCreateOpen] = useState(false)
  const [createBusy, setCreateBusy] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  const [editing, setEditing] = useState<Sandbox | null>(null)
  const [editBusy, setEditBusy] = useState(false)
  const [editError, setEditError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      setList(await sandboxes.list(filterCategory.trim() || undefined))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to list')
    } finally {
      setLoading(false)
    }
  }, [filterCategory])

  useEffect(() => {
    void load()
  }, [load])

  const categoryOptions = useMemo(() => {
    const fromList = list.map((s) => s.category).filter(Boolean) as string[]
    return Array.from(new Set([...SUGGESTED_CATEGORIES, ...fromList])).sort()
  }, [list])

  async function onCreate(values: SandboxCreateValues) {
    setCreateBusy(true)
    setCreateError(null)
    try {
      const sb = await sandboxes.create({
        templateID: values.templateID,
        timeout: values.timeoutSec,
        name: values.name || undefined,
        category: values.category || undefined,
        isDefault: values.isDefault,
      })
      setCreateOpen(false)
      navigate(`/s/${sb.sandboxID}`)
    } catch (err) {
      setCreateError(err instanceof Error ? err.message : 'create failed')
    } finally {
      setCreateBusy(false)
    }
  }

  async function onSaveEdit(values: SandboxEditValues) {
    if (!editing) return
    setEditBusy(true)
    setEditError(null)
    try {
      await sandboxes.patch(editing.sandboxID, values)
      setEditing(null)
      await load()
    } catch (err) {
      setEditError(err instanceof Error ? err.message : 'update failed')
    } finally {
      setEditBusy(false)
    }
  }

  async function onDelete(sb: Sandbox) {
    try {
      await sandboxes.remove(sb.sandboxID)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'delete failed')
    }
  }

  const columns: ColumnProps<Sandbox>[] = [
    {
      title: 'Name',
      dataIndex: 'name',
      render: (_: unknown, sb: Sandbox) => (
        <Typography.Text
          link
          onClick={() => navigate(`/s/${sb.sandboxID}`)}
          strong
        >
          {sb.name || sb.sandboxID.slice(0, 8)}
        </Typography.Text>
      ),
    },
    {
      title: 'Category',
      dataIndex: 'category',
      render: (_: unknown, sb: Sandbox) =>
        sb.category ? (
          <Space spacing={4}>
            <Tag size="small">{sb.category}</Tag>
            {sb.isDefault ? (
              <Tag size="small" color="blue">
                default
              </Tag>
            ) : null}
          </Space>
        ) : (
          <Typography.Text type="tertiary" size="small">
            uncategorized
          </Typography.Text>
        ),
    },
    {
      title: 'ID',
      dataIndex: 'sandboxID',
      render: (id: string) => (
        <Typography.Text
          type="tertiary"
          size="small"
          style={{ fontFamily: 'var(--semi-font-family-regular), monospace' }}
        >
          {id.slice(0, 8)}…
        </Typography.Text>
      ),
    },
    {
      title: 'State',
      dataIndex: 'state',
      render: (state: string | undefined) => (
        <Tag size="small" color={stateTagColor(state)}>
          {state || 'unknown'}
        </Tag>
      ),
    },
    {
      title: 'Actions',
      dataIndex: 'sandboxID',
      align: 'right',
      render: (_: unknown, sb: Sandbox) => {
        const label = sb.name || sb.sandboxID.slice(0, 8)
        return (
          <Space>
            <Button size="small" type="tertiary" onClick={() => navigate(`/s/${sb.sandboxID}`)}>
              Open
            </Button>
            <Button
              size="small"
              type="tertiary"
              onClick={() => {
                setEditError(null)
                setEditing(sb)
              }}
            >
              Edit
            </Button>
            <Popconfirm
              title={`Delete sandbox “${label}”?`}
              onConfirm={() => void onDelete(sb)}
              okType="danger"
            >
              <Button size="small" type="danger" theme="borderless">
                Delete
              </Button>
            </Popconfirm>
          </Space>
        )
      },
    },
  ]

  return (
    <PageShell subtitle="Your sandboxes" current="sandboxes">
      <div
        style={{
          marginBottom: 24,
          paddingBottom: 24,
          borderBottom: '1px solid var(--semi-color-border)',
          display: 'flex',
          flexWrap: 'wrap',
          gap: 12,
          alignItems: 'center',
        }}
      >
        <Space>
          <Button
            theme="solid"
            type="primary"
            onClick={() => {
              setCreateError(null)
              setCreateOpen(true)
            }}
          >
            New sandbox
          </Button>
          <Button type="tertiary" onClick={() => void load()}>
            Refresh
          </Button>
        </Space>
        <div
          style={{
            marginLeft: 'auto',
            display: 'flex',
            alignItems: 'center',
            gap: 8,
            minWidth: 0,
          }}
        >
          <Typography.Text type="tertiary" size="small">
            Filter
          </Typography.Text>
          <Select
            filter
            allowCreate
            showClear
            style={{ width: 160 }}
            value={filterCategory || undefined}
            onChange={(v) => setFilterCategory(typeof v === 'string' ? v : '')}
            optionList={categoryOptions.map((c) => ({ label: c, value: c }))}
            placeholder="all categories"
          />
        </div>
      </div>

      {error && (
        <div role="alert" style={{ marginBottom: 16 }}>
          <Banner fullMode={false} type="danger" description={error} closeIcon={null} />
        </div>
      )}

      {loading ? (
        <div style={{ padding: 24, textAlign: 'center' }}>
          <Spin />
        </div>
      ) : list.length === 0 ? (
        <div style={{ padding: '24px 0', display: 'flex', flexDirection: 'column', gap: 12, alignItems: 'flex-start' }}>
          <Typography.Text type="tertiary">
            No sandboxes yet. Create one to enter the pen.
          </Typography.Text>
          <Button
            theme="solid"
            type="primary"
            onClick={() => {
              setCreateError(null)
              setCreateOpen(true)
            }}
          >
            New sandbox
          </Button>
        </div>
      ) : (
        <Table
          rowKey="sandboxID"
          columns={columns}
          dataSource={list}
          pagination={false}
        />
      )}

      <SandboxCreateDialog
        open={createOpen}
        categoryOptions={categoryOptions}
        busy={createBusy}
        error={createError}
        onClose={() => {
          if (!createBusy) setCreateOpen(false)
        }}
        onCreate={onCreate}
      />

      <SandboxEditDialog
        open={editing != null}
        sandbox={editing}
        categoryOptions={categoryOptions}
        busy={editBusy}
        error={editError}
        onClose={() => {
          if (!editBusy) setEditing(null)
        }}
        onSave={onSaveEdit}
      />
    </PageShell>
  )
}
