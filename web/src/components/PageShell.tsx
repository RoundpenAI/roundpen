import { type ReactNode } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { doLogout, useAuth } from '../auth'

export type AppSection = 'sandboxes' | 'templates' | 'settings'

const NAV: { id: AppSection; to: string; label: string; admin?: boolean }[] = [
  { id: 'sandboxes', to: '/', label: 'Sandboxes' },
  { id: 'templates', to: '/registry', label: 'Templates' },
  { id: 'settings', to: '/settings', label: 'Settings', admin: true },
]

type Props = {
  subtitle: string
  current: AppSection
  children: ReactNode
  maxWidthClass?: string
  className?: string
}

export function PageShell({
  subtitle,
  current,
  children,
  maxWidthClass = 'max-w-3xl',
  className = '',
}: Props) {
  const auth = useAuth()
  const navigate = useNavigate()
  const user = auth.status === 'ok' ? auth.user : null

  return (
    <div
      className={`mx-auto flex min-h-full ${maxWidthClass} flex-col px-4 pt-6 pb-[max(2rem,env(safe-area-inset-bottom))] sm:py-8 ${className}`}
    >
      <header className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <p className="font-display text-2xl font-semibold tracking-tight">
            Roundpen
          </p>
          <p className="mt-1 text-sm opacity-55">{subtitle}</p>
        </div>
        <div className="flex min-w-0 items-center gap-2 text-sm">
          {user && (
            <span className="max-w-[40vw] truncate opacity-60 sm:max-w-[12rem]">
              {user.username}
            </span>
          )}
          <button
            type="button"
            className="btn btn-ghost btn-sm min-h-11 shrink-0 sm:min-h-0"
            onClick={() => void doLogout().then(() => navigate('/login'))}
          >
            Sign out
          </button>
        </div>
      </header>

      <nav className="mb-6 flex flex-wrap gap-x-4 gap-y-2 border-b border-base-300 pb-4 text-sm">
        {NAV.map((item) => {
          if (item.admin && user?.role !== 'admin') return null
          if (item.id === current) {
            return (
              <span key={item.id} className="font-medium">
                {item.label}
              </span>
            )
          }
          return (
            <Link
              key={item.id}
              to={item.to}
              className="link link-hover min-h-11 inline-flex items-center opacity-55 sm:min-h-0"
            >
              {item.label}
            </Link>
          )
        })}
      </nav>

      {children}
    </div>
  )
}
