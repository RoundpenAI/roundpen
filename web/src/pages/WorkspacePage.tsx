import { useCallback, useEffect, useRef, useState } from 'react'
import {
  Button,
  Spin,
  Table,
  Typography,
  Toast,
} from '@douyinfe/semi-ui-19'
import { IconFolder, IconFile, IconRefresh, IconUpload } from '@douyinfe/semi-icons'
import { meWorkspace, type DirEntry } from '../api'
import { useT } from '../i18n'

function joinPath(base: string, name: string): string {
  if (!base || base === '.') return name
  return `${base.replace(/\/$/, '')}/${name}`
}

function parentPath(path: string): string {
  if (!path || path === '.') return '.'
  const parts = path.split('/').filter(Boolean)
  parts.pop()
  return parts.length ? parts.join('/') : '.'
}

export function WorkspacePage() {
  const t = useT()
  const [path, setPath] = useState('.')
  const [entries, setEntries] = useState<DirEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await meWorkspace.list(path)
      const sorted = [...(res.entries ?? [])].sort((a, b) => {
        if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1
        return a.name.localeCompare(b.name)
      })
      setEntries(sorted)
    } catch (e) {
      setEntries([])
      setError(e instanceof Error ? e.message : 'failed')
    } finally {
      setLoading(false)
    }
  }, [path])

  useEffect(() => {
    void load()
  }, [load])

  const onUpload = async (file: File) => {
    const dest = joinPath(path, file.name)
    try {
      await meWorkspace.upload(dest, file)
      Toast.success(file.name)
      await load()
    } catch (e) {
      Toast.error(e instanceof Error ? e.message : 'upload failed')
    }
  }

  const onDelete = async (name: string) => {
    const target = joinPath(path, name)
    if (!window.confirm(t('workspace.deleteConfirm', { name }))) return
    try {
      await meWorkspace.remove(target)
      await load()
    } catch (e) {
      Toast.error(e instanceof Error ? e.message : 'delete failed')
    }
  }

  const crumb =
    path === '.' ? '/workspace' : `/workspace/${path.split('/').join('/')}`

  return (
      <div style={{ padding: '16px 20px', maxWidth: 960, margin: '0 auto' }}>
        <Typography.Title heading={3} style={{ margin: '0 0 4px' }}>
          {t('workspace.title')}
        </Typography.Title>
        <Typography.Text type="tertiary" size="small">
          {t('workspace.subtitle')}
        </Typography.Text>

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
          <Typography.Text
            code
            ellipsis={{ showTooltip: true }}
            style={{ flex: '1 1 160px', minWidth: 0 }}
          >
            {crumb}
          </Typography.Text>
          <Button
            icon={<IconRefresh />}
            type="tertiary"
            onClick={() => void load()}
          >
            {t('workspace.refresh')}
          </Button>
          <Button
            icon={<IconUpload />}
            theme="solid"
            onClick={() => fileRef.current?.click()}
          >
            {t('workspace.upload')}
          </Button>
          <input
            ref={fileRef}
            type="file"
            style={{ display: 'none' }}
            onChange={(e) => {
              const f = e.target.files?.[0]
              e.target.value = ''
              if (f) void onUpload(f)
            }}
          />
        </div>

        {error ? (
          <Typography.Text type="danger">{error}</Typography.Text>
        ) : null}

        {loading ? (
          <div style={{ padding: 48, textAlign: 'center' }}>
            <Spin tip={t('workspace.preparing')} />
          </div>
        ) : (
          <Table
            dataSource={[
              ...(path !== '.'
                ? [
                    {
                      name: t('workspace.parent'),
                      is_dir: true,
                      size: 0,
                      __parent: true,
                    } as DirEntry & { __parent?: boolean },
                  ]
                : []),
              ...entries,
            ]}
            rowKey={(record) => {
              const row = record as (DirEntry & { __parent?: boolean }) | undefined
              if (!row) return 'row'
              return row.__parent ? '__parent__' : String(row.name)
            }}
            empty={t('workspace.empty')}
            pagination={false}
            columns={[
              {
                title: 'Name',
                dataIndex: 'name',
                render: (
                  name: string,
                  row: DirEntry & { __parent?: boolean },
                ) => (
                  <Button
                    theme="borderless"
                    type="tertiary"
                    icon={row.is_dir ? <IconFolder /> : <IconFile />}
                    onClick={() => {
                      if (row.__parent) {
                        setPath(parentPath(path))
                        return
                      }
                      if (row.is_dir) setPath(joinPath(path, name))
                    }}
                  >
                    {name}
                  </Button>
                ),
              },
              {
                title: 'Size',
                dataIndex: 'size',
                width: 100,
                render: (size: number, row: DirEntry & { __parent?: boolean }) =>
                  row.is_dir || row.__parent ? '—' : String(size),
              },
              {
                title: '',
                width: 180,
                render: (_: unknown, row: DirEntry & { __parent?: boolean }) => {
                  if (row.__parent || row.is_dir) return null
                  const full = joinPath(path, row.name)
                  return (
                    <div style={{ display: 'flex', gap: 8 }}>
                      <Button
                        size="small"
                        type="tertiary"
                        onClick={() => {
                          window.open(
                            meWorkspace.downloadUrl(full),
                            '_blank',
                            'noopener',
                          )
                        }}
                      >
                        {t('workspace.download')}
                      </Button>
                      <Button
                        size="small"
                        type="danger"
                        onClick={() => void onDelete(row.name)}
                      >
                        {t('workspace.delete')}
                      </Button>
                    </div>
                  )
                },
              },
            ]}
          />
        )}
      </div>
  )
}
