import { useEffect, useState } from 'react'
import { useNavigate, useParams } from 'react-router-dom'
import { Banner, Button, Spin } from '@douyinfe/semi-ui-19'
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
      <div
        style={{
          flex: 1,
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          justifyContent: 'center',
          gap: 12,
          padding: 24,
        }}
      >
        <div role="alert">
          <Banner
            fullMode={false}
            type="danger"
            description={error}
            closeIcon={null}
          />
        </div>
        <Button
          theme="borderless"
          type="tertiary"
          onClick={() => navigate(`/a/${assistantId}`)}
        >
          打开助手详情
        </Button>
      </div>
    )
  }

  return (
    <div
      style={{
        flex: 1,
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
      }}
    >
      <Spin tip="正在打开对话…" />
    </div>
  )
}
