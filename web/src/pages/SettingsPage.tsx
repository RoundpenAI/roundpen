import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, Navigate, useNavigate } from 'react-router-dom'
import {
  adminSettings,
  templateDisplayName,
  templates,
  type AppSettings,
  type SettingsResponse,
  type Template,
} from '../api'
import { doLogout, useAuth } from '../auth'

const emptySettings: AppSettings = {
  allowPublicRegistration: false,
  defaultImage: 'host',
  defaultTtlSeconds: 1800,
  previewPublicUrl: '',
  previewTokenTtlSeconds: 900,
  templateBuilder: '',
  kanikoDestination: '',
  kanikoInsecure: false,
  kanikoSkipTlsVerify: false,
  kanikoExtraArgs: '',
}

const BUILDER_OPTIONS = [
  { value: 'auto', label: 'Auto (detect from backend / Kaniko config)' },
  { value: 'docker', label: 'Docker' },
  { value: 'kaniko', label: 'Kaniko' },
  { value: '', label: 'Disabled' },
] as const

const SANDBOX_TTL_OPTIONS = [
  { value: 900, label: '15 minutes' },
  { value: 1800, label: '30 minutes' },
  { value: 3600, label: '1 hour' },
  { value: 7200, label: '2 hours' },
  { value: 14400, label: '4 hours' },
] as const

const PREVIEW_TTL_OPTIONS = [
  { value: 300, label: '5 minutes' },
  { value: 900, label: '15 minutes' },
  { value: 1800, label: '30 minutes' },
  { value: 3600, label: '1 hour' },
] as const

function optionsWithCurrentValue<T extends { value: string; label: string }>(
  options: readonly T[],
  current: string,
): T[] {
  if (options.some((o) => o.value === current)) return [...options]
  return [...options, { value: current, label: `${current} (current)` } as T]
}

function ttlOptionsWithCurrent(
  options: readonly { value: number; label: string }[],
  current: number,
) {
  if (options.some((o) => o.value === current)) return [...options]
  return [
    ...options,
    { value: current, label: `${Math.round(current / 60)} min (current)` },
  ]
}

function templateRef(t: Template): string {
  const name = templateDisplayName(t)
  return name.includes('/') ? (name.split('/').pop() ?? name) : name
}

export function SettingsPage() {
  const auth = useAuth()
  const navigate = useNavigate()
  const [data, setData] = useState<SettingsResponse | null>(null)
  const [form, setForm] = useState<AppSettings>(emptySettings)
  const [error, setError] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [dirty, setDirty] = useState(false)
  const [templateList, setTemplateList] = useState<Template[]>([])

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const [res, tpls] = await Promise.all([
        adminSettings.get(),
        templates.list().catch(() => [] as Template[]),
      ])
      setData(res)
      setForm(res.settings)
      setTemplateList(tpls.filter((t) => t.buildStatus === 'ready'))
      setDirty(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load settings')
    } finally {
      setLoading(false)
    }
  }, [])

  const defaultImageOptions = useMemo(() => {
    const fromTemplates = templateList.flatMap((tpl) => {
      const ref = templateRef(tpl)
      return [{ value: ref, label: `${templateDisplayName(tpl)} (${tpl.cpuCount}c / ${tpl.memoryMB}MiB)` }]
    })
    const seen = new Set<string>()
    const unique = fromTemplates.filter((o) => {
      if (seen.has(o.value)) return false
      seen.add(o.value)
      return true
    })
    return optionsWithCurrentValue(unique, form.defaultImage)
  }, [templateList, form.defaultImage])

  const builderOptions = useMemo(
    () => optionsWithCurrentValue(BUILDER_OPTIONS, form.templateBuilder),
    [form.templateBuilder],
  )

  const sandboxTtlOptions = useMemo(
    () => ttlOptionsWithCurrent(SANDBOX_TTL_OPTIONS, form.defaultTtlSeconds),
    [form.defaultTtlSeconds],
  )

  const previewTtlOptions = useMemo(
    () => ttlOptionsWithCurrent(PREVIEW_TTL_OPTIONS, form.previewTokenTtlSeconds),
    [form.previewTokenTtlSeconds],
  )

  useEffect(() => {
    void load()
  }, [load])

  if (auth.status === 'loading') {
    return (
      <div className="flex h-full items-center justify-center text-sm opacity-60">
        Loading…
      </div>
    )
  }
  if (auth.status === 'anon') {
    return <Navigate to="/login" replace />
  }
  if (auth.user.role !== 'admin') {
    return <Navigate to="/" replace />
  }

  function patch(partial: Partial<AppSettings>) {
    setForm((prev) => ({ ...prev, ...partial }))
    setDirty(true)
  }

  async function onSave() {
    setSaving(true)
    setSaveError(null)
    try {
      const res = await adminSettings.update(form)
      setData(res)
      setForm(res.settings)
      setDirty(false)
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : 'save failed')
    } finally {
      setSaving(false)
    }
  }

  const user = auth.user

  return (
    <div className="mx-auto flex min-h-full max-w-3xl flex-col px-4 py-8">
      <header className="mb-8 flex items-end justify-between gap-4">
        <div>
          <p className="font-display text-2xl font-semibold tracking-tight">
            Roundpen
          </p>
          <p className="mt-1 text-sm opacity-55">System settings</p>
        </div>
        <div className="flex items-center gap-3 text-sm">
          <span className="opacity-60">{user.username}</span>
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
        <Link to="/registry" className="link link-hover opacity-55">
          Templates
        </Link>
        <span className="font-medium">Settings</span>
      </nav>

      {error && (
        <div className="mb-4 text-sm text-error" role="alert">
          {error}
        </div>
      )}

      {loading ? (
        <p className="text-sm opacity-50">Loading…</p>
      ) : (
        <>
          <section className="mb-8 space-y-4">
            <h2 className="text-sm font-medium">General</h2>
            <label className="flex items-center gap-3 text-sm">
              <input
                type="checkbox"
                className="checkbox checkbox-sm"
                checked={form.allowPublicRegistration}
                onChange={(e) =>
                  patch({ allowPublicRegistration: e.target.checked })
                }
              />
              Allow public registration
            </label>
            <label className="form-control w-full max-w-md gap-1.5">
              <span className="label-text text-xs opacity-60">
                Default template / image
              </span>
              {defaultImageOptions.length > 0 ? (
                <select
                  className="select select-bordered select-sm w-full"
                  value={form.defaultImage}
                  onChange={(e) => patch({ defaultImage: e.target.value })}
                >
                  {defaultImageOptions.map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  className="input input-bordered input-sm"
                  value={form.defaultImage}
                  onChange={(e) => patch({ defaultImage: e.target.value })}
                />
              )}
            </label>
            <label className="form-control w-full max-w-md gap-1.5">
              <span className="label-text text-xs opacity-60">
                Default sandbox TTL
              </span>
              <select
                className="select select-bordered select-sm w-full"
                value={form.defaultTtlSeconds}
                onChange={(e) =>
                  patch({ defaultTtlSeconds: Number(e.target.value) })
                }
              >
                {sandboxTtlOptions.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </label>
          </section>

          <section className="mb-8 space-y-4">
            <h2 className="text-sm font-medium">Preview</h2>
            <label className="form-control w-full max-w-md">
              <span className="label-text text-xs opacity-60">
                Public preview base URL
              </span>
              <input
                className="input input-bordered input-sm"
                placeholder="http://127.0.0.1:19001"
                value={form.previewPublicUrl}
                onChange={(e) => patch({ previewPublicUrl: e.target.value })}
              />
            </label>
            <label className="form-control w-full max-w-md gap-1.5">
              <span className="label-text text-xs opacity-60">
                Preview token TTL
              </span>
              <select
                className="select select-bordered select-sm w-full"
                value={form.previewTokenTtlSeconds}
                onChange={(e) =>
                  patch({ previewTokenTtlSeconds: Number(e.target.value) })
                }
              >
                {previewTtlOptions.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </label>
          </section>

          <section className="mb-8 space-y-4">
            <h2 className="text-sm font-medium">Template builds</h2>
            <label className="form-control w-full max-w-md gap-1.5">
              <span className="label-text text-xs opacity-60">
                Template build engine
              </span>
              <select
                className="select select-bordered select-sm w-full"
                value={form.templateBuilder}
                onChange={(e) => patch({ templateBuilder: e.target.value })}
              >
                {builderOptions.map((o) => (
                  <option key={o.value || '__disabled'} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </label>
            <label className="form-control w-full max-w-md">
              <span className="label-text text-xs opacity-60">
                Kaniko destination prefix
              </span>
              <input
                className="input input-bordered input-sm"
                placeholder="registry.example/roundpen"
                value={form.kanikoDestination}
                onChange={(e) => patch({ kanikoDestination: e.target.value })}
              />
            </label>
            <label className="flex items-center gap-3 text-sm">
              <input
                type="checkbox"
                className="checkbox checkbox-sm"
                checked={form.kanikoInsecure}
                onChange={(e) => patch({ kanikoInsecure: e.target.checked })}
              />
              Kaniko insecure registry
            </label>
            <label className="flex items-center gap-3 text-sm">
              <input
                type="checkbox"
                className="checkbox checkbox-sm"
                checked={form.kanikoSkipTlsVerify}
                onChange={(e) =>
                  patch({ kanikoSkipTlsVerify: e.target.checked })
                }
              />
              Kaniko skip TLS verify
            </label>
            <label className="form-control w-full max-w-md">
              <span className="label-text text-xs opacity-60">
                Kaniko extra args
              </span>
              <input
                className="input input-bordered input-sm"
                placeholder="--snapshot-mode=redo"
                value={form.kanikoExtraArgs}
                onChange={(e) => patch({ kanikoExtraArgs: e.target.value })}
              />
            </label>
          </section>

          {data?.system && (
            <section className="mb-8 rounded-lg border border-base-300 p-4 text-sm">
              <h2 className="mb-3 font-medium">System (read-only)</h2>
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-xs opacity-70">
                <dt>Backend</dt>
                <dd className="font-mono">{data.system.backend}</dd>
                <dt>HTTP addr</dt>
                <dd className="font-mono">{data.system.httpAddr}</dd>
                <dt>Data root</dt>
                <dd className="font-mono break-all">{data.system.dataRoot}</dd>
                <dt>Docker host</dt>
                <dd className="font-mono break-all">{data.system.dockerHost}</dd>
                <dt>Active builder</dt>
                <dd className="font-mono">
                  {data.system.templateBuilderActive || 'disabled'}
                </dd>
              </dl>
              {data.system.templateBuilderHint && (
                <p className="mt-3 text-xs opacity-55">
                  {data.system.templateBuilderHint}
                </p>
              )}
              <p className="mt-3 text-xs opacity-45">
                Changes to backend, database, and listen address require
                environment variables and a process restart.
              </p>
            </section>
          )}

          {saveError && (
            <div className="mb-4 text-sm text-error" role="alert">
              {saveError}
            </div>
          )}

          <div className="flex gap-3">
            <button
              type="button"
              className="btn btn-primary btn-sm"
              disabled={saving || !dirty}
              onClick={() => void onSave()}
            >
              {saving ? 'Saving…' : 'Save settings'}
            </button>
            <button
              type="button"
              className="btn btn-ghost btn-sm"
              disabled={loading || saving}
              onClick={() => void load()}
            >
              Reload
            </button>
          </div>
        </>
      )}
    </div>
  )
}
