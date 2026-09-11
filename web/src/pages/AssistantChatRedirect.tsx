import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { assistantsApi, ApiError } from '../api'

/** Ensures a primary session then redirects into the chat route. */
export function AssistantChatRedirect() {
  const { assistantId = '' } = useParams()
  const navigate = useNavigate()
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    void (async () => {
      try {
        const { sessionId } = await assistantsApi.ensureSession(assistantId)
        if (!cancelled) {
          navigate(`/a/${assistantId}/s/${sessionId}`, { replace: true })
        }
      } catch (e) {
        if (!cancelled) {
          setError(e instanceof ApiError ? e.message : String(e))
        }
      }
    })()
    return () => {
      cancelled = true
    }
  }, [assistantId, navigate])

  if (error) {
    return (
      <div className="chat-pane flex flex-col items-center justify-center gap-3 p-6">
        <p className="text-error" role="alert">
          {error}
        </p>
        <button
          type="button"
          className="btn btn-ghost btn-sm"
          onClick={() => navigate(`/a/${assistantId}`)}
        >
          打开助手详情
        </button>
      </div>
    )
  }

  return (
    <div className="chat-pane flex items-center justify-center opacity-50">
      正在打开对话…
    </div>
  )
}
