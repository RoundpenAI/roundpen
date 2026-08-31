import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  templates,
  templateDisplayName,
  type CreateTemplateResult,
  type Template,
} from '../api'
import { doLogout, useAuth } from '../auth'
import {
  TemplateBuildDialog,
} from '../components/TemplateBuildDialog'
import {
  TemplateCreateDialog,
  type TemplateCreateValues,
} from '../components/TemplateCreateDialog'

function statusBadge(status: string): string {
  switch (status) {
    case 'ready':
      return 'badge badge-success badge-sm'
    case 'building':
      return 'badge badge-info badge-sm'
    case 'error':
      return 'badge badge-error badge-sm'
    case 'waiting':
      return 'badge badge-warning badge-sm'
    default:
      return 'badge badge-ghost badge-sm'
  }
}

function formatWhen(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

export function TemplatesPage() {
  const auth = useAuth()
  const navigate = useNavigate()
  const [list, setList] = useState<Template[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const [createOpen, setCreateOpen] = useState(false)
  const [createBusy, setCreateBusy] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  const [building, setBuilding] = useState<Template | null>(null)

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

  const user = auth.status === 'ok' ? auth.user : null

  return (
    <div className="mx-auto flex min-h-full max-w-4xl flex-col px-4 py-8">
      <header className="mb-8 flex items-end justify-between gap-4">
        <div>
          <p className="font-display text-2xl font-semibold tracking-tight">
            Roundpen
          </p>
          <p className="mt-1 text-sm opacity-55">Templates</p>
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
        <Link to="/" className="link link-hover opacity-55">
          Sandboxes
        </Link>
        <span className="font-medium">Templates</span>
        {user?.role === 'admin' && (
          <Link to="/settings" className="link link-hover opacity-55">
            Settings
          </Link>
        )}
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
          New template
        </button>
        <button type="button" className="btn btn-ghost btn-sm" onClick={() => void load()}>
          Refresh
        </button>
      </div>

      {error && (
        <div className="mb-4 text-sm text-error" role="alert">
          {error}
        </div>
      )}

      <p className="mb-4 text-xs leading-relaxed opacity-50">
        Built-in templates (host, base, python, node, code-agent) are seeded when
        roundpend starts. Any rows you see beyond those may be leftovers from
        earlier builds in the local database.
      </p>

      {loading ? (
        <p className="text-sm opacity-50">Loading…</p>
      ) : list.length === 0 ? (
        <p className="text-sm opacity-50">No templates registered.</p>
      ) : (
        <div className="overflow-x-auto">
          <table className="table table-sm">
            <thead>
              <tr className="text-xs opacity-55">
                <th>Name</th>
                <th>Status</th>
                <th>Resources</th>
                <th>Usage</th>
                <th>Updated</th>
                <th />
              </tr>
            </thead>
            <tbody>
              {list.map((tpl) => (
                <tr key={tpl.templateID}>
                  <td>
                    <div className="font-medium">{templateDisplayName(tpl)}</div>
                    <div className="mt-0.5 font-mono text-xs opacity-45">
                      {tpl.templateID.slice(0, 8)}…
                    </div>
                  </td>
                  <td>
                    <span className={statusBadge(tpl.buildStatus || 'waiting')}>
                      {tpl.buildStatus || 'waiting'}
                    </span>
                  </td>
                  <td className="text-xs whitespace-nowrap">
                    {tpl.cpuCount}c · {tpl.memoryMB}MiB · {tpl.diskSizeMB}MiB
                  </td>
                  <td className="text-xs whitespace-nowrap opacity-70">
                    {tpl.spawnCount ?? 0} spawns
                    {tpl.buildCount != null ? ` · ${tpl.buildCount} builds` : ''}
                  </td>
                  <td className="text-xs whitespace-nowrap opacity-70">
                    {formatWhen(tpl.updatedAt)}
                  </td>
                  <td className="text-right">
                    <button
                      type="button"
                      className="btn btn-ghost btn-xs"
                      onClick={() => setBuilding(tpl)}
                    >
                      {canBuild(tpl) ? 'Build' : 'Logs'}
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
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

      <TemplateBuildDialog
        open={building != null}
        template={building}
        baseOptions={readyBases}
        onClose={() => setBuilding(null)}
        onDone={handleBuildDone}
      />
    </div>
  )
}

function canBuild(t: Template): boolean {
  return t.buildStatus === 'waiting' || t.buildStatus === 'error'
}
