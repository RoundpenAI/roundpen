import { useCallback, useEffect, useState } from 'react'
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

  return (
    <div className="flex h-full flex-col text-sm">
      <div className="rp-pane-header flex items-center justify-between px-3 py-2">
        <span>Files</span>
        <button
          type="button"
          className="btn btn-ghost btn-xs"
          onClick={() => void load()}
          title="Refresh"
        >
          ↻
        </button>
      </div>
      <div className="border-b border-base-300 px-3 py-1.5 font-mono text-xs opacity-70">
        /workspace{path === '.' ? '' : `/${path}`}
      </div>
      <div className="min-h-0 flex-1 overflow-auto py-1">
        {path !== '.' && (
            <button
              type="button"
              className="flex w-full items-center gap-2 px-3 py-2.5 text-left hover:bg-base-300/40 sm:py-1"
            onClick={() => onPathChange(parentPath(path))}
          >
            <span className="opacity-50">‥</span>
            <span>..</span>
          </button>
        )}
        {loading && (
          <div className="px-3 py-2 text-xs opacity-50">Loading…</div>
        )}
        {error && (
          <div className="px-3 py-2 text-xs text-error">{error}</div>
        )}
        {!loading &&
          !error &&
          entries.map((e) => (
            <button
              key={e.name}
              type="button"
              className="flex w-full items-center gap-2 px-3 py-2.5 text-left font-mono text-[13px] hover:bg-base-300/40 sm:py-1"
              onClick={() => {
                const next = joinPath(path, e.name)
                if (e.is_dir) onPathChange(next)
                else onOpenFile(next)
              }}
            >
              <span className="w-4 shrink-0 opacity-50">
                {e.is_dir ? '▸' : '·'}
              </span>
              <span className="truncate">{e.name}</span>
            </button>
          ))}
        {!loading && !error && entries.length === 0 && (
          <div className="px-3 py-2 text-xs opacity-40">Empty</div>
        )}
      </div>
    </div>
  )
}
