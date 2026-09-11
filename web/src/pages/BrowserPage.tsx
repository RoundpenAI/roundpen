import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  ApiError,
  browserTasks,
  environments,
  type BrowserTask,
  type EnvironmentView,
} from '../api'
import { PageShell } from '../components/PageShell'
import { RuntimePanel } from '../components/RuntimePanel'

export function BrowserPage() {
  const navigate = useNavigate()
  const [envs, setEnvs] = useState<EnvironmentView[]>([])
  const [tasks, setTasks] = useState<BrowserTask[]>([])
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [kind, setKind] = useState<'explore' | 'verify'>('explore')
  const [url, setUrl] = useState(() =>
    typeof window !== 'undefined' ? window.location.origin : '',
  )
  const [brief, setBrief] = useState('')

  const load = useCallback(async () => {
    setError(null)
    try {
      const res = await environments.list()
      setEnvs(res.environments || [])
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load environments')
    } finally {
      setLoading(false)
    }
    try {
      const listed = await browserTasks.list()
      setTasks(listed.tasks || [])
    } catch {
      setTasks([])
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const browser = envs.find((e) => e.slot === 'browser')

  async function ensure() {
    setBusy(true)
    setError(null)
    try {
      await environments.ensureBrowser()
      await load()
    } catch (e) {
      if (e instanceof ApiError && e.setup?.length) {
        setError(e.message)
      } else {
        setError(e instanceof Error ? e.message : 'ensure failed')
      }
    } finally {
      setBusy(false)
    }
  }

  async function openDesktop() {
    setBusy(true)
    setError(null)
    try {
      const link = await environments.browserDesktop()
      const desktop = `/vnc.html?url=${encodeURIComponent(link.wsUrl)}`
      window.open(desktop, '_blank', 'noopener,noreferrer')
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'desktop link failed')
    } finally {
      setBusy(false)
    }
  }

  async function startTask() {
    const startURL = url.trim()
    if (!startURL || busy) return
    setBusy(true)
    setError(null)
    try {
      const created = await browserTasks.create({
        kind,
        url: startURL,
        brief: brief.trim() || undefined,
      })
      try {
        sessionStorage.setItem(
          `roundpen.pendingPrompt.${created.sessionId}`,
          created.prompt,
        )
      } catch {
        /* ignore */
      }
      navigate(`/a`, {
        state: { pendingPrompt: created.prompt },
      })
    } catch (e) {
      setError(e instanceof Error ? e.message : 'task failed')
      setBusy(false)
    }
  }

  return (
    <PageShell subtitle="Browser environment" current="browser" maxWidthClass="max-w-3xl">
      {error && (
        <div className="alert alert-error mb-4 text-sm">
          <span>{error}</span>
        </div>
      )}

      <section className="space-y-4">
        <div>
          <h1 className="font-display text-xl font-semibold">Browser</h1>
          <p className="mt-1 text-sm opacity-55">
            One fixed desktop per user: XFCE + Chrome in QEMU. Agents control Chrome
            over CDP; you can take over the display via VNC.
          </p>
        </div>

        <div className="rounded-lg border border-base-300 p-4">
          {loading ? (
            <p className="text-sm opacity-55">Loading…</p>
          ) : (
            <>
              <dl className="grid grid-cols-[auto_1fr] gap-x-4 gap-y-2 text-sm">
                <dt className="opacity-55">Status</dt>
                <dd className="font-medium">{browser?.status || 'absent'}</dd>
                <dt className="opacity-55">Sandbox</dt>
                <dd className="font-mono text-xs">{browser?.sandboxId || '—'}</dd>
                <dt className="opacity-55">Template</dt>
                <dd>{browser?.templateId || 'browser-desktop'}</dd>
              </dl>
              <div className="mt-4 flex flex-wrap gap-2">
                <button
                  type="button"
                  className="btn btn-primary btn-sm"
                  disabled={busy}
                  onClick={() => void ensure()}
                >
                  {busy ? 'Working…' : 'Start / resume'}
                </button>
                <button
                  type="button"
                  className="btn btn-outline btn-sm"
                  disabled={busy}
                  onClick={() => void openDesktop()}
                >
                  Open desktop
                </button>
              </div>
            </>
          )}
        </div>

        <RuntimePanel engineId="qemu" showPicker={false} />

        <div className="rounded-lg border border-base-300 p-4">
          <h2 className="font-display text-lg font-semibold">Explore & verify</h2>
          <p className="mt-1 text-sm opacity-55">
            Hand the agent a site. It first walks interactive controls (including
            hover-revealed actions), then probes whatever looks off. Results stay
            in a chat session.
          </p>

          <div className="mt-4 flex flex-wrap gap-2" role="group" aria-label="Task kind">
            <button
              type="button"
              className={`btn btn-sm ${kind === 'explore' ? 'btn-primary' : 'btn-ghost'}`}
              disabled={busy}
              onClick={() => setKind('explore')}
            >
              Explore
            </button>
            <button
              type="button"
              className={`btn btn-sm ${kind === 'verify' ? 'btn-primary' : 'btn-ghost'}`}
              disabled={busy}
              onClick={() => setKind('verify')}
            >
              Verify
            </button>
          </div>

          <label className="mt-4 block text-sm">
            <span className="opacity-60">Start URL</span>
            <input
              className="input input-bordered mt-1 w-full text-sm"
              type="url"
              value={url}
              onChange={(e) => setUrl(e.target.value)}
              placeholder="https://…"
              disabled={busy}
            />
          </label>

          <label className="mt-3 block text-sm">
            <span className="opacity-60">
              {kind === 'verify' ? 'What should be true' : 'Notes (optional)'}
            </span>
            <textarea
              className="textarea textarea-bordered mt-1 w-full text-sm"
              rows={3}
              value={brief}
              onChange={(e) => setBrief(e.target.value)}
              placeholder={
                kind === 'verify'
                  ? 'e.g. hovering a chat and clicking × removes it from the list'
                  : 'e.g. login as yourself, skip billing'
              }
              disabled={busy}
            />
          </label>

          <button
            type="button"
            className="btn btn-primary btn-sm mt-4"
            disabled={busy || !url.trim()}
            onClick={() => void startTask()}
          >
            {busy ? 'Starting…' : kind === 'verify' ? 'Start verify' : 'Start explore'}
          </button>

          {tasks.length > 0 && (
            <ul className="mt-5 space-y-2 text-sm" aria-label="Recent tasks">
              {tasks.map((t) => (
                <li
                  key={t.id}
                  className="flex items-center gap-2 rounded-md border border-base-300 px-3 py-2"
                >
                  <span className="shrink-0 capitalize opacity-60">{t.kind}</span>
                  <span className="min-w-0 flex-1 truncate font-mono text-xs">
                    {t.url}
                  </span>
                  {t.sessionId ? (
                    <Link to={`/a`} className="link link-hover shrink-0">
                      Open chat
                    </Link>
                  ) : (
                    <span className="shrink-0 opacity-40">{t.status}</span>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>

        <p className="text-xs opacity-45">
          Build the guest disk with{' '}
          <code className="font-mono">images/browser-qemu/build.sh</code> before
          first start. Desktop uses QEMU VNC over a Unix socket proxied as
          WebSocket (no guest noVNC).
        </p>
      </section>
    </PageShell>
  )
}
