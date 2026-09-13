import { useCallback, useEffect, useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { IconClose, IconPlus } from '@douyinfe/semi-icons'
import { Spin, Typography } from '@douyinfe/semi-ui-19'
import { agents, ApiError, type AgentSession } from '../api'

type SessionTabsProps = {
  assistantId: string
  currentId: string
}

export function SessionTabs({ assistantId, currentId }: SessionTabsProps) {
  const navigate = useNavigate()
  const [sessions, setSessions] = useState<AgentSession[]>([])
  const [actionBusy, setActionBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [draft, setDraft] = useState('')

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
    }
  }, [assistantId])

  useEffect(() => {
    void refresh()
  }, [refresh])

  // Pick up sessions created on this or another tab while away.
  useEffect(() => {
    if (!assistantId) return
    const onVisible = () => {
      if (document.visibilityState === 'visible') void refresh()
    }
    document.addEventListener('visibilitychange', onVisible)
    return () => document.removeEventListener('visibilitychange', onVisible)
  }, [assistantId, refresh])

  const openNewSession = async () => {
    if (actionBusy) return
    setActionBusy(true)
    try {
      const sess = await agents.createSession({
        title: '新会话',
        assistantId,
      })
      setSessions((prev) => [sess, ...prev.filter((s) => s.id !== sess.id)])
      navigate(`/a/${assistantId}/s/${sess.id}`)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    } finally {
      setActionBusy(false)
    }
  }

  const closeSession = async (id: string) => {
    const idx = sessions.findIndex((s) => s.id === id)
    if (id === currentId && idx >= 0) {
      const nextTab = sessions[idx + 1] ?? sessions[idx - 1]
      navigate(
        nextTab
          ? `/a/${assistantId}/s/${nextTab.id}`
          : `/a/${assistantId}`,
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

  return (
    <div className="rp-session-tabs">
      <div className="rp-session-tabs-scroll">
        {sessions.map((s) => {
          const active = s.id === currentId
          return (
            <div
              key={s.id}
              className={[
                'rp-session-tab',
                active ? 'is-active' : '',
              ]
                .filter(Boolean)
                .join(' ')}
              title={s.title || '新会话'}
            >
              {editingId === s.id ? (
                <input
                  autoFocus
                  value={draft}
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
                  className="rp-session-tab-input"
                />
              ) : (
                <button
                  type="button"
                  className="rp-session-tab-label"
                  onClick={() => navigate(`/a/${assistantId}/s/${s.id}`)}
                  onDoubleClick={() => {
                    setEditingId(s.id)
                    setDraft(s.title)
                  }}
                >
                  <span className="rp-session-tab-title">
                    {s.title || '新会话'}
                  </span>
                </button>
              )}
              <button
                type="button"
                className="rp-session-tab-close"
                aria-label="关闭会话"
                onClick={(e) => {
                  e.stopPropagation()
                  void closeSession(s.id)
                }}
              >
                <IconClose size="extra-small" />
              </button>
            </div>
          )
        })}
        <button
          type="button"
          className="rp-session-tab-add"
          title="新会话"
          aria-label="新会话"
          disabled={actionBusy}
          onClick={() => void openNewSession()}
        >
          {actionBusy ? <Spin size="small" /> : <IconPlus size="extra-small" />}
        </button>
        {error && (
          <Typography.Text
            type="danger"
            size="small"
            ellipsis
            style={{ marginInline: 8, minWidth: 0 }}
          >
            {error}
          </Typography.Text>
        )}
      </div>
    </div>
  )
}