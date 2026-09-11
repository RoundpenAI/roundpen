import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react'
import { Link, NavLink, Navigate, Outlet, useNavigate, useParams } from 'react-router-dom'
import {
  assistantsApi,
  ApiError,
  type Assistant,
  type AssistTicket,
} from '../api'
import { doLogout, useAuth } from '../auth'
import { NAV } from './PageShell'

const SIDEBAR_KEY = 'roundpen.assistants.sidebarCollapsed'

type AssistantLayoutValue = {
  assistants: Assistant[]
  refresh: () => Promise<void>
}

const AssistantLayoutContext = createContext<AssistantLayoutValue | null>(null)

export function useAssistantLayout(): AssistantLayoutValue {
  const ctx = useContext(AssistantLayoutContext)
  if (!ctx) {
    throw new Error('useAssistantLayout must be used inside AssistantLayout')
  }
  return ctx
}

function readCollapsed(): boolean {
  try {
    return localStorage.getItem(SIDEBAR_KEY) === '1'
  } catch {
    return false
  }
}

function bioLine(a: Assistant): string {
  const bio = a.bio.trim()
  if (bio) return bio.length > 40 ? `${bio.slice(0, 37)}…` : bio
  return '补充简介以便派活'
}

export function AssistantLayout() {
  const auth = useAuth()
  const navigate = useNavigate()
  const { assistantId } = useParams()
  const user = auth.status === 'ok' ? auth.user : null

  const [assistants, setAssistants] = useState<Assistant[]>([])
  const [pending, setPending] = useState<AssistTicket[]>([])
  const [pendingOpen, setPendingOpen] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [collapsed, setCollapsed] = useState(readCollapsed)
  const [mobileOpen, setMobileOpen] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const [res, pend] = await Promise.all([
        assistantsApi.list(),
        assistantsApi.pendingTickets().catch(() => ({ count: 0, tickets: [] })),
      ])
      setAssistants(res.assistants ?? [])
      setPending(pend.tickets ?? [])
      setError(null)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  useEffect(() => {
    document.body.classList.add('chat-lock')
    return () => document.body.classList.remove('chat-lock')
  }, [])

  useEffect(() => {
    setMobileOpen(false)
  }, [assistantId])

  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      const next = !prev
      try {
        localStorage.setItem(SIDEBAR_KEY, next ? '1' : '0')
      } catch {
        /* ignore */
      }
      return next
    })
  }

  const openAssistant = async (id: string) => {
    try {
      const { sessionId } = await assistantsApi.ensureSession(id)
      navigate(`/a/${id}/s/${sessionId}`)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    }
  }

  const value = useMemo(
    () => ({ assistants, refresh }),
    [assistants, refresh],
  )

  const sidebar = (
    <aside
      className={`chat-sidebar ${collapsed ? 'chat-sidebar-collapsed' : ''}`}
    >
      <div className="chat-sidebar-top">
        <button
          type="button"
          className="chat-icon-btn"
          title={collapsed ? '打开侧栏' : '收起侧栏'}
          aria-label={collapsed ? '打开侧栏' : '收起侧栏'}
          onClick={() => {
            if (
              typeof window !== 'undefined' &&
              window.matchMedia('(max-width: 767px)').matches
            ) {
              setMobileOpen(false)
              return
            }
            toggleCollapsed()
          }}
        >
          ☰
        </button>
        {!collapsed && (
          <Link to="/a" className="font-display truncate text-lg font-semibold">
            Roundpen
          </Link>
        )}
        <Link
          to="/a/new"
          className="chat-icon-btn ml-auto"
          title="新建助手"
          aria-label="新建助手"
        >
          +
        </Link>
        <button
          type="button"
          className="chat-icon-btn relative"
          title="待处理协助单"
          aria-label="待处理"
          onClick={() => setPendingOpen((v) => !v)}
        >
          ◎
          {pending.length > 0 && (
            <span className="absolute -right-0.5 -top-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-warning px-0.5 text-[10px] text-warning-content">
              {pending.length}
            </span>
          )}
        </button>
      </div>

      {pendingOpen && !collapsed && (
        <div className="border-b border-base-300 px-3 py-2 text-xs">
          <p className="mb-1 font-medium opacity-70">待处理</p>
          {pending.length === 0 && (
            <p className="opacity-45">没有待处理协助单</p>
          )}
          {pending.map((t) => (
            <button
              key={t.id}
              type="button"
              className="mb-1 block w-full truncate rounded px-2 py-1 text-left hover:bg-base-200"
              onClick={() => {
                setPendingOpen(false)
                void openAssistant(t.assistantId)
              }}
            >
              {t.title}
            </button>
          ))}
        </div>
      )}

      {!collapsed && (
        <Link to="/a/new" className="chat-new-btn">
          新建助手
        </Link>
      )}

      <nav className="chat-session-list" aria-label="助手">
        {!collapsed && assistants.length === 0 && (
          <p className="px-3 py-6 text-xs opacity-45">还没有助手</p>
        )}
        {!collapsed &&
          assistants.map((a) => {
            const active = a.id === assistantId
            return (
              <div
                key={a.id}
                className={`chat-session-row ${active ? 'active' : ''}`}
              >
                <button
                  type="button"
                  className="chat-session-link text-left"
                  title={a.name}
                  onClick={() => void openAssistant(a.id)}
                >
                  <span className="min-w-0 flex-1">
                    <span className="block truncate font-medium">{a.name}</span>
                    <span className="block truncate text-[10px] opacity-45">
                      {bioLine(a)}
                    </span>
                  </span>
                </button>
                <NavLink
                  to={`/a/${a.id}`}
                  className="chat-session-end"
                  title="助手详情"
                  aria-label={`${a.name} 详情`}
                  onClick={(e) => e.stopPropagation()}
                >
                  ···
                </NavLink>
              </div>
            )
          })}
      </nav>

      <div className="chat-sidebar-foot">
        {!collapsed &&
          NAV.filter((item) => item.id !== 'assistants' && !item.advanced).map(
            (item) => {
              if (item.admin && user?.role !== 'admin') return null
              return (
                <Link key={item.id} to={item.to} className="chat-nav-link">
                  {item.label}
                </Link>
              )
            },
          )}
        {!collapsed && user && (
          <button
            type="button"
            className="chat-nav-link w-full text-left"
            onClick={() => void doLogout().then(() => navigate('/login'))}
          >
            退出 ({user.username})
          </button>
        )}
      </div>
    </aside>
  )

  return (
    <AssistantLayoutContext.Provider value={value}>
      <div className="chat-app">
        {mobileOpen && (
          <button
            type="button"
            className="chat-mobile-backdrop"
            aria-label="关闭菜单"
            onClick={() => setMobileOpen(false)}
          />
        )}
        <div className={`chat-shell ${mobileOpen ? 'chat-mobile-open' : ''}`}>
          {sidebar}
          <main className="chat-main">
            {error && (
              <p className="px-4 py-2 text-sm text-error" role="alert">
                {error}
              </p>
            )}
            <Outlet />
          </main>
        </div>
        <button
          type="button"
          className="chat-mobile-menu-btn"
          aria-label="菜单"
          onClick={() => setMobileOpen(true)}
        >
          ☰
        </button>
      </div>
    </AssistantLayoutContext.Provider>
  )
}

export function AssistantsIndexRedirect() {
  const { assistants, refresh } = useAssistantLayout()
  const [ready, setReady] = useState(false)

  useEffect(() => {
    void refresh().finally(() => setReady(true))
  }, [refresh])

  if (!ready && assistants.length === 0) {
    return (
      <div className="chat-pane flex items-center justify-center opacity-50">
        加载中…
      </div>
    )
  }
  if (assistants.length === 0) {
    return <Navigate to="/a/new" replace />
  }
  const first = assistants[0]
  return <Navigate to={`/a/${first.id}/chat`} replace />
}
