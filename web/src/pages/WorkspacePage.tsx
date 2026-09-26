import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Banner, Button, Modal, Toast, Typography } from '@douyinfe/semi-ui-19'
import { IconChevronDown, IconRefresh, IconUpload } from '@douyinfe/semi-icons'
import { ApiError, meWorkspace, type DirEntry } from '../api'
import { Loading } from '../components/Loading'
import {
  ContextMenu,
  type ContextMenuItem,
  type ContextMenuState,
} from '../components/workspace/ContextMenu'
import {
  PreviewDialog,
  type PreviewTarget,
} from '../components/workspace/PreviewDialog'
import { WorkspaceTable } from '../components/workspace/WorkspaceTable'
import { useT } from '../i18n'
import {
  crumbSegments,
  fileKind,
  joinPath,
  rangeKeys,
  renameDest,
  sortEntries,
} from '../lib/workspaceFiles'

type Clipboard = { mode: 'cut' | 'copy'; paths: string[] } | null

export function WorkspacePage() {
  const t = useT()
  const [path, setPath] = useState('.')
  const [entries, setEntries] = useState<DirEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selected, setSelected] = useState<string[]>([])
  const [clipboard, setClipboard] = useState<Clipboard>(null)
  const [menu, setMenu] = useState<ContextMenuState>(null)
  const [renaming, setRenaming] = useState<string | null>(null)
  const [preview, setPreview] = useState<PreviewTarget | null>(null)
  const anchorRef = useRef<string | null>(null)
  const fileRef = useRef<HTMLInputElement>(null)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await meWorkspace.list(path)
      setEntries(sortEntries(res.entries ?? []))
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

  // Selection keys are names inside the current directory: leaving the
  // directory invalidates them (the clipboard stores full paths and survives).
  useEffect(() => {
    setSelected([])
    setRenaming(null)
    anchorRef.current = null
  }, [path])

  const names = useMemo(() => entries.map((e) => e.name), [entries])
  const cutPaths = useMemo(
    () => new Set(clipboard?.mode === 'cut' ? clipboard.paths : []),
    [clipboard],
  )
  const absOf = useCallback((name: string) => joinPath(path, name), [path])
  const crumbs = useMemo(() => crumbSegments(path), [path])

  const openPreview = useCallback(
    (entry: DirEntry) => {
      setPreview({
        name: entry.name,
        path: joinPath(path, entry.name),
        size: entry.size ?? 0,
        kind: fileKind(entry),
      })
    },
    [path],
  )

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

  // 409 carries a machine-ish conflict message from the API; show the
  // localized "already exists" instead.
  const failureText = (err: unknown) => {
    if (err instanceof ApiError && err.status === 409) {
      return t('workspace.existsConflict')
    }
    return err instanceof Error ? err.message : String(err)
  }

  const runBatch = async (
    targets: string[],
    op: (abs: string) => Promise<void>,
  ): Promise<string[]> => {
    const failed: string[] = []
    for (const abs of targets) {
      try {
        await op(abs)
      } catch (e) {
        failed.push(`${abs.split('/').pop()}: ${failureText(e)}`)
      }
    }
    return failed
  }

  const cut = (targets: string[]) => {
    setClipboard({ mode: 'cut', paths: targets.map(absOf) })
    Toast.info(t('workspace.cutDone', { count: targets.length }))
  }

  const copy = (targets: string[]) => {
    setClipboard({ mode: 'copy', paths: targets.map(absOf) })
    Toast.info(t('workspace.copied', { count: targets.length }))
  }

  const paste = async (destDir: string) => {
    if (!clipboard || clipboard.paths.length === 0) return
    const mode = clipboard.mode
    const failed: string[] = []
    const moved: string[] = []
    for (const src of clipboard.paths) {
      try {
        if (mode === 'cut') {
          await meWorkspace.move(src, destDir)
          moved.push(src)
        } else {
          await meWorkspace.copy(src, destDir)
        }
      } catch (e) {
        failed.push(`${src.split('/').pop()}: ${failureText(e)}`)
      }
    }
    // A cut only drops the entries that actually moved, so a retry after a
    // partial failure does not re-run the successful ones.
    if (mode === 'cut') {
      const remaining = clipboard.paths.filter((p) => !moved.includes(p))
      setClipboard(remaining.length ? { mode: 'cut', paths: remaining } : null)
    }
    await load()
    if (failed.length) {
      Toast.error(t('workspace.opFailed', { detail: failed.join('; ') }))
    }
  }

  const confirmDelete = (targets: string[]) => {
    if (targets.length === 0) return
    const title =
      targets.length === 1
        ? t('workspace.deleteConfirm', { name: targets[0] })
        : t('workspace.deleteConfirmMany', { count: targets.length })
    Modal.confirm({
      title,
      okButtonProps: { type: 'danger', theme: 'solid' },
      onOk: async () => {
        const failed = await runBatch(targets, (abs) => meWorkspace.remove(abs))
        setSelected([])
        await load()
        if (failed.length) {
          Toast.error(t('workspace.opFailed', { detail: failed.join('; ') }))
        }
      },
    })
  }

  const handleRowClick = (name: string, e: React.MouseEvent) => {
    if (renaming) return
    if (e.shiftKey && anchorRef.current) {
      setSelected(rangeKeys(names, anchorRef.current, name))
      return
    }
    if (e.ctrlKey || e.metaKey) {
      anchorRef.current = name
      setSelected((prev) =>
        prev.includes(name) ? prev.filter((k) => k !== name) : [...prev, name],
      )
      return
    }
    anchorRef.current = name
    setSelected([name])
  }

  const handleRowDoubleClick = (entry: DirEntry) => {
    if (renaming) return
    if (entry.is_dir) {
      setPath(joinPath(path, entry.name))
      return
    }
    openPreview(entry)
  }

  const download = (names: string[]) => {
    for (const name of names) {
      window.open(meWorkspace.downloadUrl(absOf(name)), '_blank', 'noopener')
    }
  }

  const handleRowContextMenu = (entry: DirEntry, e: React.MouseEvent) => {
    if (renaming) return
    const active = selected.includes(entry.name) ? selected : [entry.name]
    if (!selected.includes(entry.name)) setSelected([entry.name])
    const items: ContextMenuItem[] = []
    if (!entry.is_dir) {
      items.push({
        key: 'preview',
        label: t('workspace.preview'),
        onClick: () => openPreview(entry),
      })
      items.push({
        key: 'download',
        label: t('workspace.download'),
        onClick: () => download(active),
      })
    }
    items.push({
      key: 'cut',
      label: t('workspace.cut'),
      onClick: () => cut(active),
    })
    items.push({
      key: 'copy',
      label: t('workspace.copy'),
      onClick: () => copy(active),
    })
    if (active.length === 1) {
      items.push({
        key: 'rename',
        label: t('workspace.rename'),
        onClick: () => setRenaming(entry.name),
      })
    }
    items.push({
      key: 'delete',
      label: t('workspace.delete'),
      danger: true,
      onClick: () => confirmDelete(active),
    })
    setMenu({ x: e.clientX, y: e.clientY, items })
  }

  const handleEmptyContextMenu = (e: React.MouseEvent) => {
    if ((e.target as HTMLElement).closest('tr')) return
    e.preventDefault()
    if (!clipboard || clipboard.paths.length === 0) return
    setMenu({
      x: e.clientX,
      y: e.clientY,
      items: [
        {
          key: 'paste',
          label: t('workspace.paste'),
          onClick: () => void paste(path),
        },
      ],
    })
  }

  // The selection actions live in a dropdown anchored under the trigger
  // button, which sits in the toolbar instead of a bar of its own row.
  const openSelectionMenu = (e: React.MouseEvent<HTMLElement>) => {
    const rect = e.currentTarget.getBoundingClientRect()
    setMenu({
      x: rect.left,
      y: rect.bottom + 4,
      items: [
        { key: 'cut', label: t('workspace.cut'), onClick: () => cut(selected) },
        {
          key: 'copy',
          label: t('workspace.copy'),
          onClick: () => copy(selected),
        },
        {
          key: 'delete',
          label: t('workspace.delete'),
          danger: true,
          onClick: () => confirmDelete(selected),
        },
        {
          key: 'clear',
          label: t('workspace.clearSelection'),
          onClick: () => setSelected([]),
        },
      ],
    })
  }

  const handleRenameSubmit = async (name: string, next: string) => {
    setRenaming(null)
    const trimmed = next.trim()
    if (!trimmed || trimmed === name) return
    if (trimmed.includes('/')) {
      Toast.error(t('workspace.renameInvalid'))
      return
    }
    if (entries.some((e) => e.name === trimmed)) {
      Toast.error(t('workspace.existsConflict'))
      return
    }
    try {
      await meWorkspace.move(absOf(name), renameDest(path, trimmed))
      await load()
    } catch (e) {
      Toast.error(failureText(e))
    }
  }

  return (
    <div
      className="rp-ws-page"
      style={{
        padding: '16px 20px',
        // Without an explicit width (and with the auto margins that center
        // it in the flex column shell) the box sizes to its content, so the
        // page visibly jumps between narrow (loading) and wide (table).
        width: '100%',
        maxWidth: 960,
        margin: '0 auto',
        boxSizing: 'border-box',
      }}
      onContextMenu={handleEmptyContextMenu}
    >
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
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            flexWrap: 'wrap',
            gap: 2,
            flex: '1 1 220px',
            minWidth: 0,
          }}
        >
          {crumbs.map((c, i) => (
            <span
              key={c.path}
              style={{ display: 'inline-flex', alignItems: 'center', gap: 2 }}
            >
              {i > 0 && (
                <Typography.Text type="tertiary" size="small">
                  /
                </Typography.Text>
              )}
              <Button
                size="small"
                theme={i === crumbs.length - 1 ? 'light' : 'borderless'}
                type={i === crumbs.length - 1 ? 'primary' : 'tertiary'}
                disabled={i === crumbs.length - 1}
                onClick={() => setPath(c.path)}
              >
                {c.path === '.' ? t('workspace.rootLabel') : c.label}
              </Button>
            </span>
          ))}
        </div>
        {selected.length > 0 && (
          <Button
            type="primary"
            theme="light"
            icon={<IconChevronDown />}
            iconPosition="right"
            onClick={openSelectionMenu}
          >
            {t('workspace.selected', { count: selected.length })}
          </Button>
        )}
        <Button
          icon={<IconRefresh />}
          type="tertiary"
          onClick={() => void load()}
        >
          {t('workspace.refresh')}
        </Button>
        {clipboard && clipboard.paths.length > 0 ? (
          <Button type="tertiary" onClick={() => void paste(path)}>
            {t('workspace.paste')}
          </Button>
        ) : null}
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
        <div style={{ marginBottom: 12 }}>
          <Banner
            fullMode={false}
            type="danger"
            description={error}
            closeIcon={null}
          />
        </div>
      ) : null}

      {loading ? (
        <div style={{ padding: 48, textAlign: 'center' }}>
          <Loading tip={t('workspace.preparing')} />
        </div>
      ) : (
        <div className="rp-ws-table">
          <WorkspaceTable
            path={path}
            entries={entries}
            selectedKeys={selected}
            cutPaths={cutPaths}
            renaming={renaming}
            onSelectionChange={setSelected}
            onRowClick={handleRowClick}
            onRowDoubleClick={handleRowDoubleClick}
            onRowContextMenu={handleRowContextMenu}
            onRenameSubmit={(name, next) => void handleRenameSubmit(name, next)}
            onRenameCancel={() => setRenaming(null)}
            t={t}
          />
        </div>
      )}

      <ContextMenu state={menu} onClose={() => setMenu(null)} />
      <PreviewDialog target={preview} onClose={() => setPreview(null)} t={t} />
    </div>
  )
}
