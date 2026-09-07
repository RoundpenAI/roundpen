import { useCallback, useEffect, useMemo, useState } from 'react'
import {
  templates,
  templateDisplayName,
  type CreateTemplateResult,
  type Template,
} from '../api'
import { PageShell } from '../components/PageShell'
import {
  TemplateBuildDialog,
} from '../components/TemplateBuildDialog'
import { TemplateDetailDialog } from '../components/TemplateDetailDialog'
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
  const [list, setList] = useState<Template[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const [createOpen, setCreateOpen] = useState(false)
  const [createBusy, setCreateBusy] = useState(false)
  const [createError, setCreateError] = useState<string | null>(null)

  const [building, setBuilding] = useState<Template | null>(null)
  const [viewingId, setViewingId] = useState<string | null>(null)

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

  return (
    <PageShell
      subtitle="Environment images"
      current="templates"
      maxWidthClass="max-w-4xl"
    >

      <div className="mb-6 flex gap-2 border-b border-base-300 pb-6">
        <button
          type="button"
          className="btn btn-primary min-h-11 flex-1 sm:btn-sm sm:min-h-0 sm:flex-none"
          onClick={() => {
            setCreateError(null)
            setCreateOpen(true)
          }}
        >
          New image
        </button>
        <button
          type="button"
          className="btn btn-ghost min-h-11 flex-1 sm:btn-sm sm:min-h-0 sm:flex-none"
          onClick={() => void load()}
        >
          Refresh
        </button>
      </div>

      <p className="mb-4 text-sm opacity-55">
        Customize Agent (OCI) and Browser (qcow2) slot images. Built-ins are seeded
        on startup.
      </p>

      {error && (
        <div className="mb-4 text-sm text-error" role="alert">
          {error}
        </div>
      )}

      <p className="mb-4 text-xs leading-relaxed opacity-50">
        Rebuild reuses the build ID when the spec is unchanged; changing base image / RUN /
        start / ready allocates a new build. Optional tags resolve as{' '}
        <code className="font-mono">name:tag</code>.
      </p>

      {loading ? (
        <p className="text-sm opacity-50">Loading…</p>
      ) : list.length === 0 ? (
        <p className="text-sm opacity-50">No templates registered.</p>
      ) : (
        <>
          <ul className="divide-y divide-base-300 md:hidden">
            {list.map((tpl) => (
              <li key={tpl.templateID} className="py-3">
                <div className="min-w-0">
                  <div className="font-medium">{templateDisplayName(tpl)}</div>
                  <div className="mt-1 flex flex-wrap items-center gap-2 text-xs opacity-70">
                    <span className={statusBadge(tpl.buildStatus || 'waiting')}>
                      {tpl.buildStatus || 'waiting'}
                    </span>
                    <span>
                      {tpl.cpuCount}c · {tpl.memoryMB}MiB
                    </span>
                    <span>{tpl.spawnCount ?? 0} spawns</span>
                  </div>
                </div>
                <div className="mt-3 flex gap-2">
                  <button
                    type="button"
                    className="btn btn-ghost min-h-11 flex-1"
                    onClick={() => setViewingId(tpl.templateID)}
                  >
                    View
                  </button>
                  <button
                    type="button"
                    className="btn btn-ghost min-h-11 flex-1"
                    onClick={() => setBuilding(tpl)}
                  >
                    {tpl.buildStatus === 'building'
                      ? 'Logs'
                      : tpl.buildStatus === 'ready'
                        ? 'Rebuild'
                        : canBuild(tpl)
                          ? 'Build'
                          : 'Logs'}
                  </button>
                </div>
              </li>
            ))}
          </ul>
          <div className="hidden overflow-x-auto md:block">
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
                    <td className="whitespace-nowrap text-xs">
                      {tpl.cpuCount}c · {tpl.memoryMB}MiB · {tpl.diskSizeMB}MiB
                    </td>
                    <td className="whitespace-nowrap text-xs opacity-70">
                      {tpl.spawnCount ?? 0} spawns
                      {tpl.buildCount != null ? ` · ${tpl.buildCount} builds` : ''}
                    </td>
                    <td className="whitespace-nowrap text-xs opacity-70">
                      {formatWhen(tpl.updatedAt)}
                    </td>
                    <td className="text-right">
                      <div className="flex justify-end gap-1">
                        <button
                          type="button"
                          className="btn btn-ghost btn-xs"
                          onClick={() => setViewingId(tpl.templateID)}
                        >
                          View
                        </button>
                        <button
                          type="button"
                          className="btn btn-ghost btn-xs"
                          onClick={() => setBuilding(tpl)}
                        >
                          {tpl.buildStatus === 'building'
                            ? 'Logs'
                            : tpl.buildStatus === 'ready'
                              ? 'Rebuild'
                              : canBuild(tpl)
                                ? 'Build'
                                : 'Logs'}
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
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

      <TemplateDetailDialog
        open={viewingId != null}
        templateId={viewingId}
        onClose={() => setViewingId(null)}
        onSaved={(tpl) => {
          setList((prev) =>
            prev.map((row) => (row.templateID === tpl.templateID ? { ...row, ...tpl } : row)),
          )
          void load({ silent: true })
        }}
        onDeleted={() => {
          setViewingId(null)
          void load()
        }}
        onBuild={(tpl) => {
          setViewingId(null)
          setBuilding(tpl)
        }}
      />

      <TemplateBuildDialog
        open={building != null}
        template={building}
        baseOptions={readyBases}
        onClose={() => setBuilding(null)}
        onDone={handleBuildDone}
      />
    </PageShell>
  )
}

function canBuild(t: Template): boolean {
  return t.buildStatus === 'waiting' || t.buildStatus === 'error'
}
