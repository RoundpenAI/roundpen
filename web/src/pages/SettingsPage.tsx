import { useCallback, useEffect, useState } from 'react'
import { Link, Navigate, useNavigate } from 'react-router-dom'
import { adminSettings, type AppSettings, type SettingsResponse } from '../api'
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

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await adminSettings.get()
      setData(res)
      setForm(res.settings)
      setDirty(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load settings')
    } finally {
      setLoading(false)
    }
  }, [])

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
            <label className="form-control w-full max-w-md">
              <span className="label-text text-xs opacity-60">
                Default template / image
              </span>
              <input
                className="input input-bordered input-sm"
                value={form.defaultImage}
                onChange={(e) => patch({ defaultImage: e.target.value })}
              />
            </label>
            <label className="form-control w-full max-w-md">
              <span className="label-text text-xs opacity-60">
                Default sandbox TTL (seconds)
              </span>
              <input
                type="number"
                min={60}
                className="input input-bordered input-sm"
                value={form.defaultTtlSeconds}
                onChange={(e) =>
                  patch({ defaultTtlSeconds: Number(e.target.value) || 0 })
                }
              />
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
            <label className="form-control w-full max-w-md">
              <span className="label-text text-xs opacity-60">
                Preview token TTL (seconds)
              </span>
              <input
                type="number"
                min={60}
                className="input input-bordered input-sm"
                value={form.previewTokenTtlSeconds}
                onChange={(e) =>
                  patch({
                    previewTokenTtlSeconds: Number(e.target.value) || 0,
                  })
                }
              />
            </label>
          </section>

          <section className="mb-8 space-y-4">
            <h2 className="text-sm font-medium">Template builds</h2>
            <label className="form-control w-full max-w-md">
              <span className="label-text text-xs opacity-60">
                Builder (auto, docker, kaniko, or empty)
              </span>
              <input
                className="input input-bordered input-sm"
                placeholder="auto"
                value={form.templateBuilder}
                onChange={(e) => patch({ templateBuilder: e.target.value })}
              />
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
