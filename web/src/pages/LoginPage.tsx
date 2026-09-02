import { useState, type FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { doLogin, useAuth } from '../auth'

export function LoginPage() {
  const auth = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const from =
    (location.state as { from?: string } | null)?.from &&
    (location.state as { from: string }).from !== '/login'
      ? (location.state as { from: string }).from
      : '/'

  const [user, setUser] = useState('admin')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  if (auth.status === 'ok') {
    return <Navigate to={from} replace />
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await doLogin(user, password)
      navigate(from, { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'login failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="relative flex min-h-full items-center justify-center overflow-y-auto px-4 py-[max(1.5rem,env(safe-area-inset-bottom))]">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0"
        style={{
          background:
            'radial-gradient(ellipse 80% 60% at 50% -10%, oklch(35% 0.08 42 / 0.45), transparent 60%), radial-gradient(ellipse 50% 40% at 90% 90%, oklch(28% 0.04 155 / 0.35), transparent), linear-gradient(180deg, oklch(14% 0.015 55), oklch(18% 0.012 55))',
        }}
      />
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 opacity-[0.07]"
        style={{
          backgroundImage:
            'repeating-linear-gradient(-12deg, transparent, transparent 40px, oklch(70% 0.05 42) 40px, oklch(70% 0.05 42) 41px)',
        }}
      />

      <div className="relative w-full max-w-sm">
        <div className="mb-8 text-center">
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-md border border-primary/40 bg-base-200">
            <svg width="28" height="28" viewBox="0 0 32 32" fill="none" aria-hidden>
              <circle cx="16" cy="16" r="10" stroke="currentColor" className="text-primary" strokeWidth="2.5" />
              <circle cx="16" cy="16" r="3" className="fill-primary" />
            </svg>
          </div>
          <h1 className="font-display text-3xl font-semibold tracking-tight text-base-content">
            Roundpen
          </h1>
          <p className="mt-2 text-sm opacity-60">
            Sign in to your agent sandbox
          </p>
        </div>

        <form
          onSubmit={(e) => void onSubmit(e)}
          className="space-y-4 rounded-box border border-base-300 bg-base-200/80 p-6 backdrop-blur"
        >
          <label className="form-control w-full">
            <span className="label-text mb-1 text-xs opacity-70">Username or email</span>
            <input
              className="input input-bordered min-h-11 w-full text-base"
              autoComplete="username"
              autoCapitalize="none"
              autoCorrect="off"
              value={user}
              onChange={(e) => setUser(e.target.value)}
              required
            />
          </label>
          <label className="form-control w-full">
            <span className="label-text mb-1 text-xs opacity-70">Password</span>
            <input
              type="password"
              className="input input-bordered min-h-11 w-full text-base"
              autoComplete="current-password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              required
            />
          </label>
          {error && (
            <div className="text-sm text-error" role="alert">
              {error}
            </div>
          )}
          <button
            type="submit"
            className="btn btn-primary min-h-11 w-full"
            disabled={busy}
          >
            {busy ? 'Signing in…' : 'Sign in'}
          </button>
        </form>
      </div>
    </div>
  )
}
