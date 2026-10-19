import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { useNavigate } from 'react-router-dom'
import { IconChevronDown, IconClose, IconEdit, IconPlus } from '@douyinfe/semi-icons'
import { Spin, Typography } from '@douyinfe/semi-ui-19'
import { agents, ApiError, type AgentSession } from '../api'

type SessionTabsProps = {
  assistantId: string
  currentId: string
  leading?: ReactNode
  trailing?: ReactNode
}

export function SessionTabs({
  assistantId,
  currentId,
  leading,
  trailing,
}: SessionTabsProps) {
  const navigate = useNavigate()
  const rootRef = useRef<HTMLDivElement>(null)
  const [sessions, setSessions] = useState<AgentSession[]>([])
  const [actionBusy, setActionBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [draft, setDraft] = useState('')
  const [open, setOpen] = useState(false)
  const [loaded, setLoaded] = useState(false)

  const refresh = useCallback(async () => {
    if (!assistantId) return
    try {
      const res = await agents.sessions()
      setSessions(
        (res.sessions ?? []).filter((s) => s.assistantId === assistantId),
      )
      setError(null)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    } finally {
      setLoaded(true)
    }
  }, [assistantId])

  useEffect(() => {
    void refresh()
  }, [refresh])

  useEffect(() => {
    if (!assistantId) return
    const onVisible = () => {
      if (document.visibilityState === 'visible') void refresh()
    }
    document.addEventListener('visibilitychange', onVisible)
    return () => document.removeEventListener('visibilitychange', onVisible)
  }, [assistantId, refresh])

  useEffect(() => {
    if (!open) return
    const onDoc = (e: MouseEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) setOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey)
    }
  }, [open])

  const openNewSession = async () => {
    if (actionBusy) return
    setActionBusy(true)
    setOpen(false)
    try {
      const sess = await agents.createSession({
        title: '新会话',
        assistantId,
      })
      setSessions((prev) => [sess, ...prev.filter((s) => s.id !== sess.id)])
      navigate(`/a/${assistantId}/s/${sess.id}`)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
      setOpen(true)
    } finally {
      setActionBusy(false)
    }
  }

  const closeSession = async (id: string) => {
    const idx = sessions.findIndex((s) => s.id === id)
    if (id === currentId && idx >= 0) {
      const nextTab = sessions[idx + 1] ?? sessions[idx - 1]
      navigate(
        nextTab ? `/a/${assistantId}/s/${nextTab.id}` : `/a/${assistantId}`,
      )
    }
    setSessions((prev) => prev.filter((s) => s.id !== id))
    try {
      await agents.deleteSession(id)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    }
  }

  const commitRename = async (id: string) => {
    const title = draft.trim()
    setEditingId(null)
    if (!title) return
    try {
      const updated = await agents.renameSession(id, title)
      setSessions((prev) => prev.map((s) => (s.id === id ? updated : s)))
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    }
  }

  if (!assistantId) return null

  const current = sessions.find((s) => s.id === currentId)
  const currentTitle = current?.title || '新会话'

  return (
    <div className="rp-session-tabs" ref={rootRef}>
      {leading}
      <span className="rp-session-crumb-sep" aria-hidden>
        /
      </span>
      <button
        type="button"
        className="rp-session-crumb"
        aria-haspopup="listbox"
        aria-expanded={open}
        title={currentTitle}
        onClick={() => setOpen((v) => !v)}
      >
        <span className="rp-session-crumb-title">{currentTitle}</span>
        <IconChevronDown size="small" />
      </button>
      {trailing}
      {open && (
        <div className="rp-session-menu" role="listbox" aria-label="会话">
          <button
            type="button"
            className="rp-session-menu-new"
            disabled={actionBusy}
            onClick={() => void openNewSession()}
          >
            {actionBusy ? <Spin size="small" /> : <IconPlus size="small" />}
            新会话
          </button>
          {!loaded ? (
            <Typography.Text type="tertiary" size="small" style={{ padding: 8 }}>
              加载中…
            </Typography.Text>
          ) : sessions.length === 0 ? (
            <Typography.Text type="tertiary" size="small" style={{ padding: 8 }}>
              还没有会话
            </Typography.Text>
          ) : (
            sessions.map((s) => {
              const active = s.id === currentId
              const title = s.title || '新会话'
              return (
                <div
                  key={s.id}
                  className={['rp-session-menu-row', active ? 'is-active' : '']
                    .filter(Boolean)
                    .join(' ')}
                  role="option"
                  aria-selected={active}
                >
                  {editingId === s.id ? (
                    <input
                      autoFocus
                      value={draft}
                      className="rp-session-tab-input"
                      onChange={(e) => setDraft(e.target.value)}
                      onBlur={() => void commitRename(s.id)}
                      onKeyDown={(e) => {
                        if (e.key === 'Enter') {
                          e.preventDefault()
                          void commitRename(s.id)
                        } else if (e.key === 'Escape') {
                          setEditingId(null)
                        }
                      }}
                      onMouseDown={(e) => e.stopPropagation()}
                    />
                  ) : (
                    <button
                      type="button"
                      className="rp-session-menu-label"
                      onClick={() => {
                        setOpen(false)
                        if (!active) navigate(`/a/${assistantId}/s/${s.id}`)
                      }}
                    >
                      {title}
                    </button>
                  )}
                  <button
                    type="button"
                    className="rp-session-menu-icon"
                    aria-label="改名"
                    onClick={() => {
                      setEditingId(s.id)
                      setDraft(s.title)
                    }}
                  >
                    <IconEdit size="small" />
                  </button>
                  <button
                    type="button"
                    className="rp-session-menu-icon"
                    aria-label="关闭会话"
                    onClick={() => void closeSession(s.id)}
                  >
                    <IconClose size="small" />
                  </button>
                </div>
              )
            })
          )}
          {error && (
            <Typography.Text
              type="danger"
              size="small"
              style={{ display: 'block', padding: '4px 8px' }}
            >
              {error}
            </Typography.Text>
          )}
        </div>
      )}
    </div>
  )
}
