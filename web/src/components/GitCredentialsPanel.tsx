import { useCallback, useEffect, useState } from 'react'
import { gitCredentials, type GitCredential } from '../api'

const controlClass =
  'input input-bordered w-full min-w-0 min-h-11 text-base sm:input-sm sm:min-h-0 sm:text-sm'
const selectClass =
  'select select-bordered w-full min-w-0 min-h-11 text-base sm:select-sm sm:min-h-0 sm:text-sm'

const PROVIDERS = [
  { value: 'gitea', label: 'Gitea' },
  { value: 'github', label: 'GitHub' },
  { value: 'gitlab', label: 'GitLab' },
  { value: 'generic', label: 'Other git host' },
] as const

export function GitCredentialsPanel() {
  const [list, setList] = useState<GitCredential[]>([])
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [host, setHost] = useState('git.eaxi.com')
  const [provider, setProvider] = useState('gitea')
  const [username, setUsername] = useState('')
  const [token, setToken] = useState('')

  const load = useCallback(async () => {
    setError(null)
    try {
      const res = await gitCredentials.list()
      setList(res.credentials ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load git credentials')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function onSave() {
    setSaving(true)
    setError(null)
    try {
      await gitCredentials.upsert({
        provider,
        host,
        username,
        token,
      })
      setToken('')
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'save failed')
    } finally {
      setSaving(false)
    }
  }

  async function onDelete(id: string) {
    setError(null)
    try {
      await gitCredentials.remove(id)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'delete failed')
    }
  }

  return (
    <section className="space-y-4">
      <h2 className="text-sm font-medium">Git personal tokens</h2>
      <p className="m-0 text-[0.8rem] leading-relaxed opacity-55">
        One personal token per git host. Roundpen stores it for your account and
        injects it into the Cloud Agent workspace for <code className="font-mono text-[0.75rem]">git</code>,
        and later <code className="font-mono text-[0.75rem]">tea</code> /{' '}
        <code className="font-mono text-[0.75rem]">gh</code> /{' '}
        <code className="font-mono text-[0.75rem]">glab</code> (issues, PRs).
        Tokens never go into the image, and we do not copy SSH keys from this machine.
      </p>
      {error ? (
        <p className="m-0 text-sm text-error" role="alert">
          {error}
        </p>
      ) : null}
      {list.length > 0 ? (
        <ul className="space-y-2 text-sm">
          {list.map((c) => (
            <li
              key={c.id}
              className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-base-300 px-3 py-2"
            >
              <div className="min-w-0">
                <div className="font-mono text-xs">{c.host}</div>
                <div className="text-[0.7rem] opacity-50">
                  {c.provider}
                  {c.username ? ` · ${c.username}` : ''}
                  {c.hasToken ? ' · personal token saved' : ''}
                </div>
              </div>
              <button
                type="button"
                className="btn btn-ghost btn-sm"
                onClick={() => void onDelete(c.id)}
              >
                Remove
              </button>
            </li>
          ))}
        </ul>
      ) : (
        <p className="m-0 text-sm opacity-45">No personal tokens yet. Add a host below.</p>
      )}
      <div className="grid gap-3 sm:grid-cols-2">
        <label className="form-control w-full gap-1.5">
          <span className="label-text text-xs opacity-60">Provider</span>
          <select
            className={selectClass}
            value={provider}
            onChange={(e) => setProvider(e.target.value)}
          >
            {PROVIDERS.map((p) => (
              <option key={p.value} value={p.value}>
                {p.label}
              </option>
            ))}
          </select>
        </label>
        <label className="form-control w-full gap-1.5">
          <span className="label-text text-xs opacity-60">Host</span>
          <input
            className={controlClass}
            value={host}
            onChange={(e) => setHost(e.target.value)}
            placeholder="git.eaxi.com"
          />
        </label>
        <label className="form-control w-full gap-1.5">
          <span className="label-text text-xs opacity-60">Username (optional)</span>
          <input
            className={controlClass}
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="git"
          />
        </label>
        <label className="form-control w-full gap-1.5">
          <span className="label-text text-xs opacity-60">Personal token</span>
          <input
            className={controlClass}
            type="password"
            autoComplete="new-password"
            value={token}
            onChange={(e) => setToken(e.target.value)}
            placeholder="PAT with repo + issues/PR scope"
          />
        </label>
      </div>
      <button
        type="button"
        className="btn btn-primary btn-sm"
        disabled={saving || !host.trim() || !token.trim()}
        onClick={() => void onSave()}
      >
        {saving ? 'Saving…' : 'Save personal token'}
      </button>
    </section>
  )
}
