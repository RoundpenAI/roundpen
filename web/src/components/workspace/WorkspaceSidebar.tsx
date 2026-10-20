import { useCallback, useEffect, useRef, useState } from 'react'
import { IconChevronRight, IconFile, IconFolder } from '@douyinfe/semi-icons'
import { meWorkspace, type DirEntry } from '../../api'
import type { MessageKey } from '../../i18n'
import { joinPath, parentPath, sortEntries } from '../../lib/workspaceFiles'

type Props = {
  /** Absolute workspace path of the file shown in the preview pane. */
  activePath: string | null
  cutPaths: ReadonlySet<string>
  /** Bump to re-fetch every currently expanded directory. */
  refreshKey: number
  onOpenFile: (absPath: string, entry: DirEntry) => void
  onContextMenu: (
    entry: DirEntry,
    absPath: string,
    e: React.MouseEvent,
  ) => void
  t: (key: MessageKey, vars?: Record<string, string | number>) => string
}

/** Ancestors of `path` (exclusive of `.` and of `path` itself). */
function ancestorDirs(path: string): string[] {
  const out: string[] = []
  let cur = parentPath(path)
  while (cur !== '.') {
    out.unshift(cur)
    cur = parentPath(cur)
  }
  return out
}

export function WorkspaceSidebar({
  activePath,
  cutPaths,
  refreshKey,
  onOpenFile,
  onContextMenu,
  t,
}: Props) {
  const [childrenByPath, setChildrenByPath] = useState<
    Record<string, DirEntry[]>
  >({})
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set())
  const [loadingPaths, setLoadingPaths] = useState<Set<string>>(() => new Set())
  const [rootError, setRootError] = useState<string | null>(null)
  // Tracks in-flight loads so expand + reveal do not double-fetch the same dir.
  const inflight = useRef(new Map<string, Promise<DirEntry[]>>())
  const expandedRef = useRef(expanded)
  expandedRef.current = expanded

  const loadDir = useCallback(async (dir: string): Promise<DirEntry[]> => {
    const existing = inflight.current.get(dir)
    if (existing) return existing
    setLoadingPaths((prev) => {
      const next = new Set(prev)
      next.add(dir)
      return next
    })
    const promise = meWorkspace
      .list(dir)
      .then((res) => sortEntries(res.entries ?? []))
      .then((entries) => {
        setChildrenByPath((prev) => ({ ...prev, [dir]: entries }))
        if (dir === '.') setRootError(null)
        return entries
      })
      .catch((e) => {
        if (dir === '.') {
          setRootError(e instanceof Error ? e.message : 'failed')
        }
        setChildrenByPath((prev) => ({ ...prev, [dir]: [] }))
        throw e
      })
      .finally(() => {
        inflight.current.delete(dir)
        setLoadingPaths((prev) => {
          const next = new Set(prev)
          next.delete(dir)
          return next
        })
      })
    inflight.current.set(dir, promise)
    return promise
  }, [])

  // Initial root load + refresh of every expanded dir (and root).
  useEffect(() => {
    void loadDir('.')
    for (const dir of expandedRef.current) {
      void loadDir(dir)
    }
  }, [refreshKey, loadDir])

  // Reveal the active file: expand + load each ancestor so the row is visible.
  useEffect(() => {
    if (!activePath) return
    const dirs = ancestorDirs(activePath)
    if (dirs.length === 0) return
    let cancelled = false
    void (async () => {
      setExpanded((prev) => {
        const next = new Set(prev)
        for (const d of dirs) next.add(d)
        return next
      })
      for (const d of dirs) {
        if (cancelled) return
        try {
          await loadDir(d)
        } catch {
          /* leave empty; user can retry via refresh */
        }
      }
    })()
    return () => {
      cancelled = true
    }
  }, [activePath, loadDir])

  const toggleDir = async (abs: string) => {
    const isOpen = expanded.has(abs)
    if (isOpen) {
      setExpanded((prev) => {
        const next = new Set(prev)
        next.delete(abs)
        return next
      })
      return
    }
    setExpanded((prev) => new Set(prev).add(abs))
    if (!childrenByPath[abs]) {
      try {
        await loadDir(abs)
      } catch {
        /* loadingPaths cleared in finally */
      }
    }
  }

  const root = childrenByPath['.']
  const rootLoading = loadingPaths.has('.') && !root

  return (
    <nav className="rp-ws-sidebar" aria-label={t('workspace.title')}>
      {rootError ? (
        <div className="rp-ws-sidebar-empty">{rootError}</div>
      ) : null}
      {rootLoading ? (
        <div className="rp-ws-sidebar-empty">…</div>
      ) : null}
      {root && root.length === 0 && !rootError ? (
        <div className="rp-ws-sidebar-empty">{t('workspace.empty')}</div>
      ) : null}
      {root?.map((entry) => (
        <TreeNode
          key={entry.name}
          entry={entry}
          abs={joinPath('.', entry.name)}
          depth={0}
          expanded={expanded}
          childrenByPath={childrenByPath}
          loadingPaths={loadingPaths}
          activePath={activePath}
          cutPaths={cutPaths}
          onToggleDir={toggleDir}
          onOpenFile={onOpenFile}
          onContextMenu={onContextMenu}
        />
      ))}
    </nav>
  )
}

type NodeProps = {
  entry: DirEntry
  abs: string
  depth: number
  expanded: ReadonlySet<string>
  childrenByPath: Record<string, DirEntry[]>
  loadingPaths: ReadonlySet<string>
  activePath: string | null
  cutPaths: ReadonlySet<string>
  onToggleDir: (abs: string) => void
  onOpenFile: (absPath: string, entry: DirEntry) => void
  onContextMenu: (
    entry: DirEntry,
    absPath: string,
    e: React.MouseEvent,
  ) => void
}

function TreeNode({
  entry,
  abs,
  depth,
  expanded,
  childrenByPath,
  loadingPaths,
  activePath,
  cutPaths,
  onToggleDir,
  onOpenFile,
  onContextMenu,
}: NodeProps) {
  const isDir = entry.is_dir
  const open = isDir && expanded.has(abs)
  const active = !isDir && abs === activePath
  const kids = isDir ? childrenByPath[abs] : undefined
  const loading = isDir && loadingPaths.has(abs) && !kids

  return (
    <>
      <button
        type="button"
        className={[
          'rp-ws-sidebar-item',
          active ? 'is-active' : '',
          open ? 'is-open' : '',
        ]
          .filter(Boolean)
          .join(' ')}
        style={{
          paddingLeft: 8 + depth * 14,
          opacity: cutPaths.has(abs) ? 0.5 : undefined,
        }}
        onClick={() => {
          if (isDir) onToggleDir(abs)
          else onOpenFile(abs, entry)
        }}
        onContextMenu={(e) => {
          e.preventDefault()
          onContextMenu(entry, abs, e)
        }}
      >
        {isDir ? (
          <IconChevronRight
            className={[
              'rp-ws-sidebar-chevron',
              open ? 'is-open' : '',
            ]
              .filter(Boolean)
              .join(' ')}
          />
        ) : (
          <span className="rp-ws-sidebar-chevron-spacer" />
        )}
        {isDir ? <IconFolder /> : <IconFile />}
        <span className="rp-ws-sidebar-label">{entry.name}</span>
      </button>
      {open ? (
        loading ? (
          <div
            className="rp-ws-sidebar-empty"
            style={{ paddingLeft: 8 + (depth + 1) * 14 }}
          >
            …
          </div>
        ) : kids && kids.length === 0 ? (
          <div
            className="rp-ws-sidebar-empty"
            style={{ paddingLeft: 8 + (depth + 1) * 14 }}
          >
            —
          </div>
        ) : (
          kids?.map((child) => (
            <TreeNode
              key={child.name}
              entry={child}
              abs={joinPath(abs, child.name)}
              depth={depth + 1}
              expanded={expanded}
              childrenByPath={childrenByPath}
              loadingPaths={loadingPaths}
              activePath={activePath}
              cutPaths={cutPaths}
              onToggleDir={onToggleDir}
              onOpenFile={onOpenFile}
              onContextMenu={onContextMenu}
            />
          ))
        )
      ) : null}
    </>
  )
}
