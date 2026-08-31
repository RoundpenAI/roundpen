import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { sandboxes, SUGGESTED_CATEGORIES, type Sandbox } from '../api'
import { doLogout, useAuth } from '../auth'
import {
  SandboxCreateDialog,
  type SandboxCreateValues,
} from '../components/SandboxCreateDialog'
import {
  SandboxEditDialog,
  type SandboxEditValues,
} from '../components/SandboxEditDialog'

export function SandboxesPage() {
  const auth = useAuth()
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

  const user = auth.status === 'ok' ? auth.user : null

  return (
    <div className="mx-auto flex min-h-full max-w-3xl flex-col px-4 py-8">
      <header className="mb-8 flex items-end justify-between gap-4">
        <div>
          <p className="font-display text-2xl font-semibold tracking-tight">
            Roundpen
          </p>
          <p className="mt-1 text-sm opacity-55">Your sandboxes</p>
        </div>
        <div className="flex items-center gap-3 text-sm">
          {user && <span className="opacity-60">{user.username}</span>}
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            onClick={() => void doLogout().then(() => navigate('/login'))}
          >
            Sign out
          </button>
        </div>
      </header>

      <nav className="mb-6 flex gap-4 border-b border-base-300 pb-4 text-sm">
        <span className="font-medium">Sandboxes</span>
        <Link to="/registry" className="link link-hover opacity-55">
          Templates
        </Link>
      </nav>

      <div className="mb-6 flex flex-wrap items-center gap-3 border-b border-base-300 pb-6">
        <button
          type="button"
          className="btn btn-primary btn-sm"
          onClick={() => {
            setCreateError(null)
            setCreateOpen(true)
          }}
        >
          New sandbox
        </button>
        <button
          type="button"
          className="btn btn-ghost btn-sm"
          onClick={() => void load()}
        >
          Refresh
        </button>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <span className="text-xs opacity-55">Filter</span>
          <input
            className="input input-bordered input-xs w-36"
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
              className="btn btn-ghost btn-xs"
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
            className="btn btn-primary btn-sm"
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
              className="flex items-center justify-between gap-4 py-3"
            >
              <div className="min-w-0">
                <Link
                  to={`/s/${sb.sandboxID}`}
                  className="text-sm font-medium link link-hover"
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
                  <span className="font-mono truncate max-w-[10rem]">
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
              <div className="flex shrink-0 gap-2">
                <Link to={`/s/${sb.sandboxID}`} className="btn btn-sm btn-ghost">
                  Open
                </Link>
                <button
                  type="button"
                  className="btn btn-sm btn-ghost"
                  onClick={() => {
                    setEditError(null)
                    setEditing(sb)
                  }}
                >
                  Edit
                </button>
                <button
                  type="button"
                  className="btn btn-sm btn-ghost text-error"
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
    </div>
  )
}
