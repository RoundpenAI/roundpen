import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useSearchParams } from 'react-router-dom'
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
  FilePreview,
  type PreviewTarget,
} from '../components/workspace/FilePreview'
import { WorkspaceSidebar } from '../components/workspace/WorkspaceSidebar'
import { WorkspaceTable } from '../components/workspace/WorkspaceTable'
import { useT } from '../i18n'
import {
  crumbSegments,
  fileKind,
  joinPath,
  renameDest,
  sortEntries,
} from '../lib/workspaceFiles'

type Clipboard = { mode: 'cut' | 'copy'; paths: string[] } | null

export function WorkspacePage() {
  const t = useT()
  const [searchParams] = useSearchParams()
  const initialPath = (() => {
    const raw = (searchParams.get('path') ?? '').trim()
    if (!raw || raw === '/' || raw.includes('..')) return '.'
    return raw.replace(/^\/+/, '').replace(/\/+$/, '') || '.'
  })()
  const previewName = (() => {
    const raw = (searchParams.get('preview') ?? '').trim()
    if (!raw || raw.includes('..') || raw.includes('/')) return ''
    return raw
  })()
  const [path, setPath] = useState(initialPath)
  const [entries, setEntries] = useState<DirEntry[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [selected, setSelected] = useState<string[]>([])
  const [clipboard, setClipboard] = useState<Clipboard>(null)
  const [menu, setMenu] = useState<ContextMenuState>(null)
  const [renaming, setRenaming] = useState<string | null>(null)
  // Split layout (left tree + right pane). Stays on while browsing folders;
  // only "back to list" exits to the full table.
  const [treeMode, setTreeMode] = useState(false)
  const [preview, setPreview] = useState<PreviewTarget | null>(null)
  // Bumped after each successful list/mutation so the sidebar reloads caches.
  const [treeRefreshKey, setTreeRefreshKey] = useState(0)
  const fileRef = useRef<HTMLInputElement>(null)
  const previewOpenedRef = useRef('')

  useEffect(() => {
    setPath(initialPath)
  }, [initialPath])

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await meWorkspace.list(path)
      setEntries(sortEntries(res.entries ?? []))
      setTreeRefreshKey((k) => k + 1)
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
  }, [path])

  const cutPaths = useMemo(
    () => new Set(clipboard?.mode === 'cut' ? clipboard.paths : []),
    [clipboard],
  )
  const absOf = useCallback((name: string) => joinPath(path, name), [path])
  const crumbs = useMemo(() => crumbSegments(path), [path])

  const openPreview = useCallback((absPath: string, entry: DirEntry) => {
    setTreeMode(true)
    setPreview({
      name: entry.name,
      path: absPath,
      size: entry.size ?? 0,
      kind: fileKind(entry),
    })
  }, [])

  // Deep-link from chat ChangeSet: /workspace?path=dir&preview=file.ext
  useEffect(() => {
    if (!previewName || loading || error) return
    const key = `${path}\0${previewName}`
    if (previewOpenedRef.current === key) return
    const entry = entries.find((e) => e.name === previewName && !e.is_dir)
    if (!entry) return
    previewOpenedRef.current = key
    openPreview(joinPath(path, entry.name), entry)
  }, [previewName, loading, error, entries, path, openPreview])

  const exitTreeMode = useCallback(() => {
    setTreeMode(false)
    setPreview(null)
  }, [])

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

  const cut = (absPaths: string[]) => {
    setClipboard({ mode: 'cut', paths: absPaths })
    Toast.info(t('workspace.cutDone', { count: absPaths.length }))
  }

  const copy = (absPaths: string[]) => {
    setClipboard({ mode: 'copy', paths: absPaths })
    Toast.info(t('workspace.copied', { count: absPaths.length }))
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

  const confirmDelete = (names: string[]) => {
    confirmDeleteAbs(names.map(absOf), names[0] ?? '')
  }

  const confirmDeleteAbs = (absPaths: string[], labelName: string) => {
    if (absPaths.length === 0) return
    const title =
      absPaths.length === 1
        ? t('workspace.deleteConfirm', { name: labelName || absPaths[0] })
        : t('workspace.deleteConfirmMany', { count: absPaths.length })
    Modal.confirm({
      title,
      okButtonProps: { type: 'danger', theme: 'solid' },
      onOk: async () => {
        const failed = await runBatch(absPaths, (abs) => meWorkspace.remove(abs))
        setSelected([])
        await load()
        if (failed.length) {
          Toast.error(t('workspace.opFailed', { detail: failed.join('; ') }))
        }
      },
    })
  }

  const openDir = (next: string) => {
    setPath(next)
  }

  // Row click opens (dir → enter, file → preview). Selection is checkbox-only.
  const handleRowClick = (name: string) => {
    if (renaming) return
    const entry = entries.find((x) => x.name === name)
    if (!entry) return
    if (entry.is_dir) {
      openDir(joinPath(path, entry.name))
      return
    }
    openPreview(joinPath(path, entry.name), entry)
  }

  const handleRowDoubleClick = (entry: DirEntry) => {
    if (renaming) return
    if (entry.is_dir) {
      openDir(joinPath(path, entry.name))
      return
    }
    openPreview(joinPath(path, entry.name), entry)
  }

  const downloadAbs = (absPaths: string[]) => {
    for (const abs of absPaths) {
      window.open(meWorkspace.downloadUrl(abs), '_blank', 'noopener')
    }
  }

  const handleRowContextMenu = (entry: DirEntry, e: React.MouseEvent) => {
    if (renaming) return
    const active = selected.includes(entry.name) ? selected : [entry.name]
    if (!selected.includes(entry.name)) setSelected([entry.name])
    const absTargets = active.map(absOf)
    const items: ContextMenuItem[] = []
    if (!entry.is_dir) {
      items.push({
        key: 'preview',
        label: t('workspace.preview'),
        onClick: () => openPreview(joinPath(path, entry.name), entry),
      })
      items.push({
        key: 'download',
        label: t('workspace.download'),
        onClick: () => downloadAbs(absTargets),
      })
    }
    items.push({
      key: 'cut',
      label: t('workspace.cut'),
      onClick: () => cut(absTargets),
    })
    items.push({
      key: 'copy',
      label: t('workspace.copy'),
      onClick: () => copy(absTargets),
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

  // Sidebar tree nodes are addressed by absolute path (nested dirs).
  const handleTreeContextMenu = (
    entry: DirEntry,
    abs: string,
    e: React.MouseEvent,
  ) => {
    const items: ContextMenuItem[] = []
    if (!entry.is_dir) {
      items.push({
        key: 'preview',
        label: t('workspace.preview'),
        onClick: () => openPreview(abs, entry),
      })
      items.push({
        key: 'download',
        label: t('workspace.download'),
        onClick: () => downloadAbs([abs]),
      })
    }
    items.push({
      key: 'cut',
      label: t('workspace.cut'),
      onClick: () => cut([abs]),
    })
    items.push({
      key: 'copy',
      label: t('workspace.copy'),
      onClick: () => copy([abs]),
    })
    items.push({
      key: 'delete',
      label: t('workspace.delete'),
      danger: true,
      onClick: () =>
        confirmDeleteAbs([abs], entry.name),
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
        {
          key: 'cut',
          label: t('workspace.cut'),
          onClick: () => cut(selected.map(absOf)),
        },
        {
          key: 'copy',
          label: t('workspace.copy'),
          onClick: () => copy(selected.map(absOf)),
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

  // Inline rename lives on the table; leave split view so the input is visible.
  useEffect(() => {
    if (renaming) exitTreeMode()
  }, [renaming, exitTreeMode])

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

  const split = treeMode

  return (
    <div
      className={split ? 'rp-ws-page is-split' : 'rp-ws-page'}
      style={{
        padding: '16px 20px',
        paddingBottom: 'max(24px, env(safe-area-inset-bottom, 0px))',
        // Without an explicit width (and with the auto margins that center
        // it in the flex column shell) the box sizes to its content, so the
        // page visibly jumps between narrow (loading) and wide (table).
        width: '100%',
        maxWidth: split ? undefined : 960,
        margin: '0 auto',
        boxSizing: 'border-box',
        flex: split ? '1 1 auto' : '0 0 auto',
        minHeight: split ? 0 : undefined,
        display: split ? 'flex' : undefined,
        flexDirection: split ? 'column' : undefined,
      }}
      onContextMenu={handleEmptyContextMenu}
    >
      <Typography.Title heading={3} style={{ margin: '0 0 4px' }}>
        {t('workspace.title')}
      </Typography.Title>
      {!split ? (
        <Typography.Text type="tertiary" size="small">
          {t('workspace.subtitle')}
        </Typography.Text>
      ) : null}

      <div
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          gap: 8,
          alignItems: 'center',
          marginTop: split ? 8 : 16,
          marginBottom: 12,
          flex: '0 0 auto',
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
                onClick={() => openDir(c.path)}
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

      {loading && !split ? (
        <div style={{ padding: 48, textAlign: 'center' }}>
          <Loading tip={t('workspace.preparing')} />
        </div>
      ) : split ? (
        <div className="rp-ws-split">
          <WorkspaceSidebar
            activePath={preview?.path ?? null}
            cutPaths={cutPaths}
            refreshKey={treeRefreshKey}
            onOpenFile={openPreview}
            onContextMenu={handleTreeContextMenu}
            t={t}
          />
          {preview ? (
            <FilePreview
              target={preview}
              onClose={exitTreeMode}
              t={t}
            />
          ) : (
            <div className="rp-ws-preview rp-ws-preview-empty">
              <div className="rp-ws-preview-bar">
                <div className="rp-ws-preview-meta">
                  <Typography.Text type="tertiary">
                    {t('workspace.previewPickFile')}
                  </Typography.Text>
                </div>
                <div className="rp-ws-preview-actions">
                  <Button type="tertiary" onClick={exitTreeMode}>
                    {t('workspace.backToList')}
                  </Button>
                </div>
              </div>
            </div>
          )}
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
    </div>
  )
}
