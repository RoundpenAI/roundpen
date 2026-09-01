import { useEffect, useState, type ReactNode } from 'react'
import { Navigate, useLocation } from 'react-router-dom'
import { ApiError, auth, type User } from './api'

type AuthState =
  | { status: 'loading' }
  | { status: 'anon' }
  | { status: 'ok'; user: User }

let cached: AuthState | null = null
const listeners = new Set<(s: AuthState) => void>()

function setAuth(s: AuthState) {
  cached = s
  listeners.forEach((l) => l(s))
}

export async function refreshAuth(): Promise<AuthState> {
  try {
    const user = await auth.me()
    const next: AuthState = { status: 'ok', user }
    setAuth(next)
    return next
  } catch (e) {
    const next: AuthState =
      e instanceof ApiError && e.status === 401
        ? { status: 'anon' }
        : { status: 'anon' }
    setAuth(next)
    return next
  }
}

export function useAuth(): AuthState {
  const [state, setState] = useState<AuthState>(cached ?? { status: 'loading' })

  useEffect(() => {
    listeners.add(setState)
    if (!cached || cached.status === 'loading') {
      void refreshAuth()
    } else {
      setState(cached)
    }
    return () => {
      listeners.delete(setState)
    }
  }, [])

  return state
}

export function RequireAuth({ children }: { children: ReactNode }) {
  const authState = useAuth()
  const location = useLocation()

  if (authState.status === 'loading') {
    return (
      <div className="flex h-full items-center justify-center text-sm opacity-60">
        Loading…
      </div>
    )
  }
  if (authState.status === 'anon') {
    return <Navigate to="/login" replace state={{ from: location.pathname }} />
  }
  return children
}

export async function doLogin(user: string, password: string) {
  const res = await auth.login(user, password)
  setAuth({ status: 'ok', user: res.user })
}

export async function doLogout() {
  try {
    await auth.logout()
  } finally {
    setAuth({ status: 'anon' })
  }
}
