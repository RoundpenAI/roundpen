import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
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
    const label = sb.name || sb.sandboxID.slice(0, 8)
    if (!confirm(`Delete sandbox “${label}”?`)) return
    try {
      await sandboxes.remove(sb.sandboxID)
      await load()
    } catch (err) {
      setError(err instanceof Error ? err.message : 'delete failed')
    }
  }

  return (
    <PageShell subtitle="Your sandboxes" current="sandboxes">
      <div className="mb-6 flex flex-col gap-3 border-b border-base-300 pb-6 sm:flex-row sm:flex-wrap sm:items-center">
        <div className="flex gap-2">
          <button
            type="button"
            className="btn btn-primary min-h-11 flex-1 sm:btn-sm sm:min-h-0 sm:flex-none"
            onClick={() => {
              setCreateError(null)
              setCreateOpen(true)
            }}
          >
            New sandbox
          </button>
          <button
            type="button"
            className="btn btn-ghost min-h-11 flex-1 sm:btn-sm sm:min-h-0 sm:flex-none"
            onClick={() => void load()}
          >
            Refresh
          </button>
        </div>
        <div className="flex min-w-0 items-center gap-2 sm:ml-auto">
          <span className="shrink-0 text-xs opacity-55">Filter</span>
          <input
            className="input input-bordered min-h-11 min-w-0 flex-1 text-base sm:input-sm sm:min-h-0 sm:w-36 sm:flex-none sm:text-sm"
            list="sandbox-categories"
            value={filterCategory}
            onChange={(e) => setFilterCategory(e.target.value)}
            placeholder="all categories"
          />
          <datalist id="sandbox-categories">
            {categoryOptions.map((c) => (
              <option key={c} value={c} />
            ))}
          </datalist>
          {filterCategory && (
            <button
              type="button"
              className="btn btn-ghost min-h-11 sm:btn-xs sm:min-h-0"
              onClick={() => setFilterCategory('')}
            >
              Clear
            </button>
          )}
        </div>
      </div>

      {error && (
        <div className="mb-4 text-sm text-error" role="alert">
          {error}
        </div>
      )}

      {loading ? (
        <p className="text-sm opacity-50">Loading…</p>
      ) : list.length === 0 ? (
        <div className="flex flex-col items-start gap-3 py-6">
          <p className="text-sm opacity-50">
            No sandboxes yet. Create one to enter the pen.
          </p>
          <button
            type="button"
            className="btn btn-primary min-h-11 sm:btn-sm sm:min-h-0"
            onClick={() => {
              setCreateError(null)
              setCreateOpen(true)
            }}
          >
            New sandbox
          </button>
        </div>
      ) : (
        <ul className="divide-y divide-base-300">
          {list.map((sb) => (
            <li
              key={sb.sandboxID}
              className="flex flex-col gap-3 py-3 sm:flex-row sm:items-center sm:justify-between sm:gap-4"
            >
              <div className="min-w-0">
                <Link
                  to={`/s/${sb.sandboxID}`}
                  className="link link-hover text-sm font-medium"
                >
                  {sb.name || sb.sandboxID.slice(0, 8)}
                </Link>
                <div className="mt-0.5 flex flex-wrap gap-2 text-xs opacity-55">
                  {sb.category ? (
                    <span>
                      {sb.category}
                      {sb.isDefault ? ' · default' : ''}
                    </span>
                  ) : (
                    <span>uncategorized</span>
                  )}
                  <span className="max-w-[10rem] truncate font-mono">
                    {sb.sandboxID.slice(0, 8)}…
                  </span>
                  <span
                    className={
                      sb.state === 'running'
                        ? 'text-success'
                        : sb.state === 'stopped'
                          ? 'text-warning'
                          : ''
                    }
                  >
                    {sb.state || 'unknown'}
                  </span>
                </div>
              </div>
              <div className="flex gap-2 sm:shrink-0">
                <Link
                  to={`/s/${sb.sandboxID}`}
                  className="btn btn-ghost min-h-11 flex-1 sm:btn-sm sm:min-h-0 sm:flex-none"
                >
                  Open
                </Link>
                <button
                  type="button"
                  className="btn btn-ghost min-h-11 flex-1 sm:btn-sm sm:min-h-0 sm:flex-none"
                  onClick={() => {
                    setEditError(null)
                    setEditing(sb)
                  }}
                >
                  Edit
                </button>
                <button
                  type="button"
                  className="btn btn-ghost min-h-11 flex-1 text-error sm:btn-sm sm:min-h-0 sm:flex-none"
                  onClick={() => void onDelete(sb)}
                >
                  Delete
                </button>
              </div>
            </li>
          ))}
        </ul>
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
