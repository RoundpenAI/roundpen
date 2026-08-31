import { useEffect, useId, useState, type FormEvent } from 'react'
import {
  templateDisplayName,
  templates,
  type Template,
  type TemplateDetail,
  type TemplatePatch,
} from '../api'

type Props = {
  open: boolean
  templateId: string | null
  busy?: boolean
  error?: string | null
  onClose: () => void
  onSaved: (tpl: Template) => void
  onDeleted: (templateID: string) => void
  onBuild: (tpl: Template) => void
}

function formatWhen(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

export function TemplateDetailDialog({
  open,
  templateId,
  busy = false,
  error = null,
  onClose,
  onSaved,
  onDeleted,
  onBuild,
}: Props) {
  const titleId = useId()
  const [detail, setDetail] = useState<TemplateDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saveOk, setSaveOk] = useState(false)
  const [saving, setSaving] = useState(false)
  const [deleteBusy, setDeleteBusy] = useState(false)

  const [description, setDescription] = useState('')
  const [cpuCount, setCpuCount] = useState(1)
  const [memoryMB, setMemoryMB] = useState(512)
  const [diskSizeMB, setDiskSizeMB] = useState(5120)
  const [isPublic, setIsPublic] = useState(true)

  function syncFormFromDetail(d: TemplateDetail) {
    setDescription(d.description ?? '')
    setCpuCount(d.cpuCount)
    setMemoryMB(d.memoryMB)
    setDiskSizeMB(d.diskSizeMB)
    setIsPublic(d.public)
  }

  useEffect(() => {
    if (!open || !templateId) {
      setDetail(null)
      return
    }
    setLoading(true)
    setLoadError(null)
    setSaveError(null)
    setSaveOk(false)
    void templates
      .get(templateId)
      .then((d) => {
        setDetail(d)
        syncFormFromDetail(d)
      })
      .catch((e) =>
        setLoadError(e instanceof Error ? e.message : 'failed to load'),
      )
      .finally(() => setLoading(false))
  }, [open, templateId])

  if (!open || !templateId) return null

  const readOnly = detail?.builtin ?? false
  const displayName = detail ? templateDisplayName(detail) : templateId.slice(0, 8)

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (!detail || readOnly || saving) return
    setSaveError(null)
    setSaveOk(false)
    setSaving(true)
    const patch: TemplatePatch = {
      description,
      public: isPublic,
      cpuCount: cpuCount > 0 ? cpuCount : 1,
      memoryMB: memoryMB > 0 ? memoryMB : 512,
      diskSizeMB: diskSizeMB > 0 ? diskSizeMB : 5120,
    }
    try {
      const updated = await templates.update(detail.templateID, patch)
      onSaved(updated)
      const refreshed = await templates.get(detail.templateID)
      setDetail(refreshed)
      syncFormFromDetail(refreshed)
      setSaveOk(true)
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'save failed')
    } finally {
      setSaving(false)
    }
  }

  async function onDelete() {
    if (!detail || readOnly) return
    if (!confirm(`Delete template “${displayName}”? This cannot be undone.`)) return
    setDeleteBusy(true)
    try {
      await templates.remove(detail.templateID)
      onDeleted(detail.templateID)
      onClose()
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'delete failed')
    } finally {
      setDeleteBusy(false)
    }
  }

  return (
    <dialog className="modal modal-open" aria-labelledby={titleId}>
      <div className="modal-box max-w-lg">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <h3 id={titleId} className="font-display text-lg font-semibold truncate">
              {displayName}
            </h3>
            <p className="mt-1 font-mono text-xs opacity-50 truncate">{templateId}</p>
          </div>
          {readOnly && <span className="badge badge-ghost badge-sm shrink-0">Built-in</span>}
        </div>

        {loading ? (
          <p className="mt-6 text-sm opacity-50">Loading…</p>
        ) : loadError ? (
          <p className="mt-6 text-sm text-error" role="alert">
            {loadError}
          </p>
        ) : detail ? (
          <>
            <dl className="mt-4 grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-xs opacity-70">
              <dt>Status</dt>
              <dd>{detail.buildStatus || 'waiting'}</dd>
              <dt>Namespace</dt>
              <dd className="font-mono">{detail.namespace}</dd>
              <dt>Profile</dt>
              <dd>{detail.profile || 'dev'}</dd>
              <dt>Usage</dt>
              <dd>
                {detail.spawnCount ?? 0} spawns · {detail.buildCount ?? 0} builds
              </dd>
              <dt>Updated</dt>
              <dd>{formatWhen(detail.updatedAt)}</dd>
            </dl>

            {detail.builds.length > 0 && (
              <div className="mt-4">
                <p className="mb-2 text-xs font-medium opacity-60">Build history</p>
                <ul className="max-h-32 space-y-1 overflow-y-auto text-xs opacity-70">
                  {detail.builds.map((b) => (
                    <li key={b.buildID} className="font-mono truncate">
                      {b.buildID.slice(0, 8)}… · {b.status}
                      {b.artifactRef ? ` · ${b.artifactRef}` : ''}
                    </li>
                  ))}
                </ul>
              </div>
            )}

            <form onSubmit={(e) => void submit(e)} className="mt-5 flex flex-col gap-4">
              <label className="form-control w-full gap-1.5">
                <span className="text-xs font-medium opacity-60">Description</span>
                <textarea
                  className="textarea textarea-bordered textarea-sm w-full"
                  rows={2}
                  value={description}
                  onChange={(e) => setDescription(e.target.value)}
                  readOnly={readOnly}
                />
              </label>

              <div className="grid grid-cols-3 gap-3">
                <label className="form-control gap-1.5">
                  <span className="text-xs font-medium opacity-60">CPU</span>
                  <input
                    type="number"
                    min={1}
                    className="input input-bordered input-sm"
                    value={cpuCount}
                    onChange={(e) => setCpuCount(Number(e.target.value) || 1)}
                    readOnly={readOnly}
                  />
                </label>
                <label className="form-control gap-1.5">
                  <span className="text-xs font-medium opacity-60">Memory</span>
                  <input
                    type="number"
                    min={128}
                    step={128}
                    className="input input-bordered input-sm"
                    value={memoryMB}
                    onChange={(e) => setMemoryMB(Number(e.target.value) || 512)}
                    readOnly={readOnly}
                  />
                </label>
                <label className="form-control gap-1.5">
                  <span className="text-xs font-medium opacity-60">Disk</span>
                  <input
                    type="number"
                    min={512}
                    step={512}
                    className="input input-bordered input-sm"
                    value={diskSizeMB}
                    onChange={(e) => setDiskSizeMB(Number(e.target.value) || 5120)}
                    readOnly={readOnly}
                  />
                </label>
              </div>

              <label className="flex cursor-pointer items-center gap-2.5">
                <input
                  type="checkbox"
                  className="checkbox checkbox-sm"
                  checked={isPublic}
                  onChange={(e) => setIsPublic(e.target.checked)}
                  disabled={readOnly}
                />
                <span className="text-sm">Public template</span>
              </label>

              {(error || saveError) && (
                <p className="text-sm text-error" role="alert">
                  {error || saveError}
                </p>
              )}
              {saveOk && !saveError && (
                <p className="text-sm text-success">Settings saved.</p>
              )}

              <div className="modal-action mt-1 flex-wrap">
                {!readOnly && (
                  <button
                    type="button"
                    className="btn btn-ghost btn-sm text-error mr-auto"
                    disabled={busy || saving || deleteBusy}
                    onClick={() => void onDelete()}
                  >
                    {deleteBusy ? 'Deleting…' : 'Delete'}
                  </button>
                )}
                <button
                  type="button"
                  className="btn btn-ghost btn-sm"
                  disabled={busy || saving || deleteBusy}
                  onClick={onClose}
                >
                  Close
                </button>
                <button
                  type="button"
                  className="btn btn-ghost btn-sm"
                  disabled={busy || saving || deleteBusy}
                  onClick={() => onBuild(detail)}
                >
                  {detail.buildStatus === 'waiting' || detail.buildStatus === 'error'
                    ? 'Build'
                    : 'Logs'}
                </button>
                {!readOnly && (
                  <button
                    type="submit"
                    className="btn btn-primary btn-sm"
                    disabled={busy || saving || deleteBusy}
                  >
                    {saving ? 'Saving…' : 'Save'}
                  </button>
                )}
              </div>
            </form>
          </>
        ) : null}
      </div>
      <form method="dialog" className="modal-backdrop">
        <button type="button" disabled={busy || deleteBusy} onClick={onClose}>
          close
        </button>
      </form>
    </dialog>
  )
}
