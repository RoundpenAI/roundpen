import { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, Spin, Tree, Typography } from '@douyinfe/semi-ui-19'
import type { TreeNodeData } from '@douyinfe/semi-ui-19/lib/es/tree'
import { IconFile, IconFolder, IconRefresh } from '@douyinfe/semi-icons'
import { files, type DirEntry } from '../api'

type Props = {
  sandboxId: string
  path: string
  onPathChange: (path: string) => void
  onOpenFile: (path: string) => void
}

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

type EntryMeta = {
  kind: 'parent' | 'dir' | 'file'
  path: string
}

export function FileTree({ sandboxId, path, onPathChange, onOpenFile }: Props) {
  const [entries, setEntries] = useState<DirEntry[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await files.list(sandboxId, path)
      const sorted = [...(res.entries ?? [])].sort((a, b) => {
        if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1
        return a.name.localeCompare(b.name)
      })
      setEntries(sorted)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to list')
      setEntries([])
    } finally {
      setLoading(false)
    }
  }, [sandboxId, path])

  useEffect(() => {
    void load()
  }, [load])

  const treeData = useMemo((): TreeNodeData[] => {
    const nodes: TreeNodeData[] = []
    if (path !== '.') {
      nodes.push({
        key: '__parent__',
        label: '..',
        icon: <IconFolder style={{ color: 'var(--semi-color-text-2)' }} />,
        isLeaf: true,
        meta: { kind: 'parent', path: parentPath(path) } satisfies EntryMeta,
      })
    }
    for (const e of entries) {
      const next = joinPath(path, e.name)
      nodes.push({
        key: next,
        label: e.name,
        icon: e.is_dir ? (
          <IconFolder style={{ color: 'var(--semi-color-text-2)' }} />
        ) : (
          <IconFile style={{ color: 'var(--semi-color-text-2)' }} />
        ),
        isLeaf: true,
        meta: {
          kind: e.is_dir ? 'dir' : 'file',
          path: next,
        } satisfies EntryMeta,
      })
    }
    return nodes
  }, [entries, path])

  const displayPath = `/workspace${path === '.' ? '' : `/${path}`}`

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        fontSize: 13,
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          padding: '8px 12px',
          borderBottom: '1px solid var(--semi-color-border)',
        }}
      >
        <Typography.Text
          size="small"
          type="tertiary"
          style={{
            fontSize: 11,
            letterSpacing: '0.06em',
            textTransform: 'uppercase',
          }}
        >
          Files
        </Typography.Text>
        <Button
          theme="borderless"
          type="tertiary"
          size="small"
          icon={<IconRefresh />}
          onClick={() => void load()}
          aria-label="Refresh"
          title="Refresh"
        />
      </div>
      <div
        style={{
          padding: '6px 12px',
          borderBottom: '1px solid var(--semi-color-border)',
          fontFamily: 'var(--semi-font-family-code)',
          fontSize: 12,
          color: 'var(--semi-color-text-2)',
          overflow: 'hidden',
          textOverflow: 'ellipsis',
          whiteSpace: 'nowrap',
        }}
        title={displayPath}
      >
        {displayPath}
      </div>
      <div style={{ flex: 1, minHeight: 0, overflow: 'auto', padding: '4px 0' }}>
        {loading && (
          <div style={{ display: 'flex', justifyContent: 'center', padding: 16 }}>
            <Spin size="small" />
          </div>
        )}
        {error && (
          <Typography.Text
            type="danger"
            size="small"
            style={{ display: 'block', padding: '8px 12px' }}
          >
            {error}
          </Typography.Text>
        )}
        {!loading && !error && (
          <Tree
            treeData={treeData}
            directory
            defaultExpandAll
            onSelect={(_key, _selected, node) => {
              const meta = (node as TreeNodeData & { meta?: EntryMeta }).meta
              if (!meta) return
              if (meta.kind === 'parent' || meta.kind === 'dir') {
                onPathChange(meta.path)
              } else {
                onOpenFile(meta.path)
              }
            }}
            emptyContent={
              <Typography.Text type="tertiary" size="small">
                Empty
              </Typography.Text>
            }
            style={{ background: 'transparent' }}
          />
        )}
      </div>
    </div>
  )
}
