import { useEffect, useState, type CSSProperties } from 'react'
import { Link } from 'react-router-dom'
import { Banner, Button, Typography } from '@douyinfe/semi-ui-19'
import { setupApi, ApiError, type LlmReady } from '../api'

type Props = {
  status: LlmReady | null
  isAdmin: boolean
  onReady: (status: LlmReady) => void
  /** Compact layout for chat banner area */
  compact?: boolean
  /** When true, fetch /llm-ready on mount if status is null */
  autoLoad?: boolean
  /** Already on settings; hide the "go to settings" shortcut */
  inSettings?: boolean
}

const cardStyle: CSSProperties = {
  border: '1px solid var(--semi-color-border)',
  borderRadius: 8,
  padding: 16,
  display: 'flex',
  flexDirection: 'column',
  gap: 8,
  flex: 1,
  minWidth: 0,
}

function modeLabel(mode: string | undefined): string {
  switch (mode) {
    case 'free':
      return '当前：免费试用（FREE）'
    case 'byok':
      return '当前：自有 API Key（BYOK）'
    default:
      return '当前：尚未配置模型'
  }
}

export function LlmSetupChooser({
  status,
  isAdmin,
  onReady,
  compact,
  autoLoad,
  inSettings,
}: Props) {
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [local, setLocal] = useState<LlmReady | null>(status)

  useEffect(() => {
    setLocal(status)
  }, [status])

  useEffect(() => {
    if (!autoLoad || status != null) return
    let alive = true
    void setupApi
      .llmReady()
      .then((res) => {
        if (!alive) return
        setLocal(res)
        onReady(res)
      })
      .catch(() => {
        /* keep null; button still tries enableFree */
      })
    return () => {
      alive = false
    }
  }, [autoLoad, status, onReady])

  const apply = (res: LlmReady) => {
    setLocal(res)
    onReady(res)
  }

  const enableFree = async () => {
    if (busy) return
    setBusy(true)
    setError(null)
    try {
      const res = await setupApi.enableFreeLlm()
      apply(res)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const recheck = async () => {
    if (busy) return
    setBusy(true)
    setError(null)
    try {
      const res = await setupApi.llmReady()
      apply(res)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const mode = local?.mode ?? 'none'
  const alreadyFree = mode === 'free'
  const reason = local?.reason

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: compact ? 12 : 16 }}>
      {!compact && (
        <>
          <Typography.Title heading={5} style={{ margin: 0 }}>
            接通大脑
          </Typography.Title>
          <Typography.Text type="secondary">
            选择免费试用（有频率限制），或使用自己的 API Key（推荐生产）。
          </Typography.Text>
        </>
      )}
      <Typography.Text type="tertiary" size="small">
        {modeLabel(mode)}
      </Typography.Text>
      {reason ? (
        <Banner fullMode={false} type="warning" description={reason} closeIcon={null} />
      ) : null}
      {error ? (
        <Banner fullMode={false} type="danger" description={error} closeIcon={null} />
      ) : null}

      {!isAdmin ? (
        <Typography.Text type="tertiary">请联系管理员配置模型。</Typography.Text>
      ) : (
        <div
          style={{
            display: 'flex',
            flexDirection: compact ? 'column' : 'row',
            gap: 12,
            flexWrap: 'wrap',
          }}
        >
          <div style={cardStyle}>
            <Typography.Text strong>免费试用</Typography.Text>
            <Typography.Text type="tertiary" size="small">
              走 Roundpen 云端入口，按安装身份限频。适合快速体验；额度用尽或安装身份失效时，请改用自己的 Key。
            </Typography.Text>
            <div>
              {alreadyFree ? (
                <Button theme="light" type="primary" disabled>
                  已启用 FREE
                </Button>
              ) : (
                <Button
                  theme="solid"
                  type="primary"
                  loading={busy}
                  onClick={() => void enableFree()}
                >
                  一键启用 FREE
                </Button>
              )}
            </div>
          </div>
          <div style={cardStyle}>
            <Typography.Text strong>自己的 API Key（BYOK）</Typography.Text>
            <Typography.Text type="tertiary" size="small">
              在模型配置中添加 DeepSeek / Kimi / GLM / OpenRouter 或自定义上游，并设为默认。不受 FREE 配额约束。
            </Typography.Text>
            <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
              {!inSettings ? (
                <Link to="/admin/settings/llmgw">
                  <Button theme="light" type="primary">
                    前往系统管理 · 模型配置
                  </Button>
                </Link>
              ) : (
                <Typography.Text type="tertiary" size="small">
                  在下方添加提供商，并设为默认。
                </Typography.Text>
              )}
              <Button theme="borderless" type="tertiary" loading={busy} onClick={() => void recheck()}>
                重新检查
              </Button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
