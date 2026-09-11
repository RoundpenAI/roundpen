import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react'
import { Link, NavLink, Outlet, useNavigate, useParams } from 'react-router-dom'
import {
  agents,
  type AgentProvider,
  type AgentSession,
  ApiError,
} from '../api'
import { doLogout, useAuth } from '../auth'
import { NAV, type AppSection } from './PageShell'

const SIDEBAR_KEY = 'roundpen.chats.sidebarCollapsed'

type ChatLayoutValue = {
  sessions: AgentSession[]
  providers: AgentProvider[]
  refresh: () => Promise<void>
}

const ChatLayoutContext = createContext<ChatLayoutValue | null>(null)

export function useChatLayout(): ChatLayoutValue {
  const ctx = useContext(ChatLayoutContext)
  if (!ctx) {
    throw new Error('useChatLayout must be used inside ChatLayout')
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

function formatWhen(iso: string): string {
  const t = Date.parse(iso)
  if (Number.isNaN(t)) return ''
  const diff = Date.now() - t
  if (diff < 60_000) return 'Just now'
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m`
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h`
  return new Date(t).toLocaleDateString()
}

export function ChatLayout() {
  const auth = useAuth()
  const navigate = useNavigate()
  const { id: activeId } = useParams()
  const user = auth.status === 'ok' ? auth.user : null

  const [sessions, setSessions] = useState<AgentSession[]>([])
  const [providers, setProviders] = useState<AgentProvider[]>([])
  const [error, setError] = useState<string | null>(null)
  const [collapsed, setCollapsed] = useState(readCollapsed)
  const [mobileOpen, setMobileOpen] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const [s, a] = await Promise.all([agents.sessions(), agents.list()])
      setSessions(s.sessions ?? [])
      setProviders(a.agents ?? [])
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
  }, [activeId])

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

  const endSession = async (sessionId: string) => {
    try {
      await agents.deleteSession(sessionId)
      if (sessionId === activeId) navigate('/a')
      await refresh()
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    }
  }

  const value = useMemo(
    () => ({ sessions, providers, refresh }),
    [sessions, providers, refresh],
  )

  const sidebar = (
    <aside
      className={`chat-sidebar ${collapsed ? 'chat-sidebar-collapsed' : ''}`}
    >
      <div className="chat-sidebar-top">
        <button
          type="button"
          className="chat-icon-btn"
          title={collapsed ? 'Open sidebar' : 'Close sidebar'}
          aria-label={collapsed ? 'Open sidebar' : 'Close sidebar'}
          onClick={() => {
            if (typeof window !== 'undefined' && window.matchMedia('(max-width: 767px)').matches) {
              setMobileOpen(false)
              return
            }
            toggleCollapsed()
          }}
        >
          <SidebarIcon />
        </button>
        {!collapsed && (
          <Link to="/a" className="font-display truncate text-lg font-semibold">
            Roundpen
          </Link>
        )}
        <Link
          to="/a"
          className="chat-icon-btn ml-auto"
          title="New chat"
          aria-label="New chat"
        >
          <NewChatIcon />
        </Link>
      </div>

      {!collapsed && (
        <Link to="/a" className="chat-new-btn">
          <NewChatIcon />
          New chat
        </Link>
      )}

      <nav className="chat-session-list" aria-label="Chats">
        {!collapsed && sessions.length === 0 && (
          <p className="px-3 py-6 text-xs opacity-45">No chats yet</p>
        )}
        {!collapsed &&
          sessions.map((s) => {
            const active = s.id === activeId
            return (
              <div
                key={s.id}
                className={`chat-session-row ${active ? 'active' : ''}`}
              >
                <NavLink
                  to={`/a`}
                  className="chat-session-link"
                  title={s.title || s.id}
                >
                  <span className="min-w-0 flex-1 truncate">
                    {s.title || 'New chat'}
                  </span>
                  <span className="shrink-0 text-[10px] opacity-40">
                    {formatWhen(s.updatedAt || s.createdAt)}
                  </span>
                </NavLink>
                <button
                  type="button"
                  className="chat-session-end"
                  title="Delete chat"
                  aria-label={`Delete ${s.title || 'chat'}`}
                  onMouseDown={(e) => {
                    e.preventDefault()
                    e.stopPropagation()
                  }}
                  onClick={(e) => {
                    e.preventDefault()
                    e.stopPropagation()
                    void endSession(s.id)
                  }}
                >
                  ×
                </button>
              </div>
            )
          })}
      </nav>

      <div className="chat-sidebar-foot">
        {!collapsed &&
          NAV.filter((item) => item.id !== 'assistants').map((item) => {
            if (item.admin && user?.role !== 'admin') return null
            return (
              <Link
                key={item.id}
                to={item.to}
                className="chat-nav-link"
                title={item.label}
              >
                {item.label}
              </Link>
            )
          })}
        {user && !collapsed && (
          <div className="mt-2 flex items-center gap-2 px-1 text-xs">
            <span className="min-w-0 flex-1 truncate opacity-55">
              {user.username}
            </span>
            <button
              type="button"
              className="btn btn-ghost btn-xs"
              onClick={() => void doLogout().then(() => navigate('/login'))}
            >
              Sign out
            </button>
          </div>
        )}
        {user && collapsed && (
          <button
            type="button"
            className="chat-icon-btn"
            title={`Sign out ${user.username}`}
            aria-label={`Sign out ${user.username}`}
            onClick={() => void doLogout().then(() => navigate('/login'))}
          >
            ⎋
          </button>
        )}
      </div>
    </aside>
  )

  return (
    <ChatLayoutContext.Provider value={value}>
      <div className="chat-app">
        {mobileOpen && (
          <button
            type="button"
            className="chat-sidebar-backdrop"
            aria-label="Close sidebar"
            onClick={() => setMobileOpen(false)}
          />
        )}
        <div className={`chat-sidebar-slot ${mobileOpen ? 'open' : ''}`}>
          {sidebar}
        </div>

        <div className="chat-main">
          <header className="chat-main-bar">
            <button
              type="button"
              className="chat-icon-btn md:hidden"
              aria-label="Open chats"
              onClick={() => setMobileOpen(true)}
            >
              <SidebarIcon />
            </button>
            <p className="min-w-0 truncate text-sm font-medium opacity-80">
              {activeId
                ? sessions.find((s) => s.id === activeId)?.title || 'Chat'
                : 'New chat'}
            </p>
            <div className="ml-auto hidden items-center gap-3 text-sm md:flex">
              {NAV.map((item) => {
                if (item.admin && user?.role !== 'admin') return null
                const current: AppSection = 'assistants'
                if (item.id === current) {
                  return (
                    <span key={item.id} className="opacity-40">
                      {item.label}
                    </span>
                  )
                }
                return (
                  <Link
                    key={item.id}
                    to={item.to}
                    className="link link-hover opacity-55"
                  >
                    {item.label}
                  </Link>
                )
              })}
            </div>
          </header>
          {error && (
            <p className="px-4 pt-2 text-sm text-error" role="alert">
              {error}
            </p>
          )}
          <div className="chat-main-body">
            <Outlet />
          </div>
        </div>
      </div>
    </ChatLayoutContext.Provider>
  )
}

function SidebarIcon() {
  return (
    <svg width="18" height="18" viewBox="0 0 24 24" fill="none" aria-hidden>
      <rect
        x="3.5"
        y="4.5"
        width="17"
        height="15"
        rx="2.5"
        stroke="currentColor"
        strokeWidth="1.6"
      />
      <path d="M9 5v14" stroke="currentColor" strokeWidth="1.6" />
    </svg>
  )
}

function NewChatIcon() {
  return (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" aria-hidden>
      <path
        d="M12 5v14M5 12h14"
        stroke="currentColor"
        strokeWidth="1.8"
        strokeLinecap="round"
      />
    </svg>
  )
}
