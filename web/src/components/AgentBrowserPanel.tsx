import {
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type KeyboardEvent,
  type MouseEvent as ReactMouseEvent,
} from 'react'
import { Banner, Button, Spin, Typography } from '@douyinfe/semi-ui-19'
import { agents, type AgentBrowserStatus, ApiError } from '../api'

type Props = {
  sessionId: string
  active: boolean
  busy?: boolean
  onClose?: () => void
}

/** Map a pointer event on an object-fit:contain <img> into CSS viewport coords. */
function mapPointerToViewport(
  ev: { clientX: number; clientY: number; currentTarget: HTMLImageElement },
  layoutW: number,
  layoutH: number,
): { x: number; y: number } | null {
  const img = ev.currentTarget
  const rect = img.getBoundingClientRect()
  if (rect.width <= 0 || rect.height <= 0 || layoutW <= 0 || layoutH <= 0) {
    return null
  }
  // Prefer natural aspect (screenshot pixels); fall back to declared viewport.
  const natW = img.naturalWidth || layoutW
  const natH = img.naturalHeight || layoutH
  const scale = Math.min(rect.width / natW, rect.height / natH)
  const dispW = natW * scale
  const dispH = natH * scale
  const offsetX = (rect.width - dispW) / 2
  const offsetY = (rect.height - dispH) / 2
  const px = ev.clientX - rect.left - offsetX
  const py = ev.clientY - rect.top - offsetY
  if (px < 0 || py < 0 || px > dispW || py > dispH) return null
  // CDP Input uses CSS pixels (= layout viewport), not device pixels.
  return { x: (px / dispW) * layoutW, y: (py / dispH) * layoutH }
}

const panelStyle: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  height: '100%',
  minHeight: 320,
  overflow: 'hidden',
  borderRadius: 'var(--semi-border-radius-medium)',
  border: '1px solid var(--semi-color-border)',
  background: 'var(--semi-color-bg-1)',
  outline: 'none',
}

const barStyle: CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: 8,
  borderBottom: '1px solid var(--semi-color-border)',
  padding: '8px 12px',
}

/** Live System Agent browser preview with optional human takeover. */
export function AgentBrowserPanel({
  sessionId,
  active,
  busy = false,
  onClose,
}: Props) {
  const [status, setStatus] = useState<AgentBrowserStatus | null>(null)
  const [frameUrl, setFrameUrl] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [takeoverBusy, setTakeoverBusy] = useState(false)
  const [takeoverLocal, setTakeoverLocal] = useState(false)
  const panelRef = useRef<HTMLDivElement | null>(null)
  const imgRef = useRef<HTMLImageElement | null>(null)
  const frameUrlRef = useRef<string | null>(null)
  const inputLockRef = useRef(false)
  const lastPointerRef = useRef({ x: 0, y: 0 })

  const refreshFrame = async () => {
    if (inputLockRef.current) return
    try {
      const blob = await agents.browserScreenshotBlob(sessionId)
      const url = URL.createObjectURL(blob)
      if (frameUrlRef.current) URL.revokeObjectURL(frameUrlRef.current)
      frameUrlRef.current = url
      setFrameUrl(url)
      setError(null)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    }
  }

  useEffect(() => {
    return () => {
      if (frameUrlRef.current) URL.revokeObjectURL(frameUrlRef.current)
    }
  }, [])

  const takeover = takeoverLocal || status?.takeover === true

  useEffect(() => {
    if (!active || !sessionId) return
    let cancelled = false
    const tick = async () => {
      try {
        const st = await agents.browserStatus(sessionId)
        if (cancelled) return
        setStatus(st)
        setTakeoverLocal(st.takeover)
        if (st.attached) {
          await refreshFrame()
        }
      } catch (e) {
        if (!cancelled) {
          setError(e instanceof ApiError ? e.message : String(e))
        }
      }
    }
    void tick()
    // During takeover, poll slower so we don't thrash the <img> under the cursor.
    const ms = takeover ? 1200 : busy ? 450 : 700
    const id = window.setInterval(() => void tick(), ms)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, sessionId, busy, takeover])

  // Non-passive wheel listener so we can preventDefault and scroll the remote page.
  useEffect(() => {
    const el = imgRef.current
    if (!el || !takeover) return
    const onWheel = (ev: WheelEvent) => {
      ev.preventDefault()
      ev.stopPropagation()
      const layoutW = status?.width || 1280
      const layoutH = status?.height || 800
      const pt = mapPointerToViewport(
        { clientX: ev.clientX, clientY: ev.clientY, currentTarget: el },
        layoutW,
        layoutH,
      )
      const x = pt?.x ?? lastPointerRef.current.x
      const y = pt?.y ?? lastPointerRef.current.y
      lastPointerRef.current = { x, y }
      void (async () => {
        inputLockRef.current = true
        try {
          await agents.browserInput(sessionId, {
            type: 'wheel',
            x,
            y,
            deltaX: ev.deltaX,
            deltaY: ev.deltaY,
          })
          // Brief pause then refresh so scroll is visible.
          window.setTimeout(() => {
            inputLockRef.current = false
            void refreshFrame()
          }, 120)
        } catch (e) {
          inputLockRef.current = false
          setError(e instanceof ApiError ? e.message : String(e))
        }
      })()
    }
    el.addEventListener('wheel', onWheel, { passive: false })
    return () => el.removeEventListener('wheel', onWheel)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [takeover, sessionId, status?.width, status?.height, frameUrl])

  const width = status?.width || 1280
  const height = status?.height || 800
  const attached = status?.attached === true || frameUrl != null

  const setTakeover = async (enabled: boolean) => {
    setTakeoverBusy(true)
    setTakeoverLocal(enabled)
    setError(null)
    try {
      const res = await agents.setBrowserTakeover(sessionId, enabled)
      setStatus({
        sessionId,
        hubId: status?.hubId ?? '',
        attached: res.attached !== false,
        takeover: res.takeover,
        url: res.url || status?.url || '',
        width: res.width || status?.width || 1280,
        height: res.height || status?.height || 800,
      })
      setTakeoverLocal(res.takeover)
      if (enabled) {
        await refreshFrame()
        panelRef.current?.focus()
      }
    } catch (e) {
      setTakeoverLocal(!enabled)
      setError(e instanceof ApiError ? e.message : String(e))
    } finally {
      setTakeoverBusy(false)
    }
  }

  const sendClick = async (ev: ReactMouseEvent<HTMLImageElement>) => {
    if (!takeover) return
    const pt = mapPointerToViewport(ev, width, height)
    if (!pt) return
    lastPointerRef.current = pt
    inputLockRef.current = true
    try {
      await agents.browserInput(sessionId, { type: 'click', x: pt.x, y: pt.y })
      window.setTimeout(() => {
        inputLockRef.current = false
        void refreshFrame()
      }, 150)
    } catch (e) {
      inputLockRef.current = false
      setError(e instanceof ApiError ? e.message : String(e))
    }
  }

  const sendMove = (ev: ReactMouseEvent<HTMLImageElement>) => {
    if (!takeover) return
    const pt = mapPointerToViewport(ev, width, height)
    if (!pt) return
    lastPointerRef.current = pt
  }

  const onKeyDown = async (ev: KeyboardEvent<HTMLDivElement>) => {
    if (!takeover) return
    if (ev.metaKey || ev.ctrlKey) return
    if (ev.key === 'Tab') ev.preventDefault()
    try {
      if (ev.key.length === 1 && !ev.altKey) {
        await agents.browserInput(sessionId, { type: 'type', text: ev.key })
      } else if (
        [
          'Enter',
          'Backspace',
          'Delete',
          'Escape',
          'Tab',
          'ArrowUp',
          'ArrowDown',
          'ArrowLeft',
          'ArrowRight',
          'PageUp',
          'PageDown',
          'Home',
          'End',
          ' ',
        ].includes(ev.key)
      ) {
        ev.preventDefault()
        const key = ev.key === ' ' ? 'Space' : ev.key
        await agents.browserInput(sessionId, { type: 'key', key })
      } else {
        return
      }
      void refreshFrame()
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    }
  }

  return (
    <div
      ref={panelRef}
      className="agent-browser-panel"
      style={panelStyle}
      tabIndex={takeover ? 0 : -1}
      onKeyDown={onKeyDown}
    >
      <div style={barStyle}>
        <Typography.Text
          size="small"
          type="tertiary"
          style={{
            fontSize: 11,
            fontWeight: 500,
            letterSpacing: '0.06em',
            textTransform: 'uppercase',
            flexShrink: 0,
          }}
        >
          Browser
        </Typography.Text>
        <Typography.Text
          ellipsis={{ showTooltip: true }}
          type="tertiary"
          size="small"
          style={{
            flex: 1,
            minWidth: 0,
            fontFamily: 'var(--semi-font-family-code)',
            fontSize: 11,
          }}
        >
          {status?.url || (attached ? '(blank)' : 'not attached')}
        </Typography.Text>
        {onClose && (
          <Button theme="borderless" type="tertiary" size="small" onClick={onClose}>
            Hide
          </Button>
        )}
      </div>

      <div
        style={{
          ...barStyle,
          flexWrap: 'wrap',
          background: 'var(--semi-color-fill-0)',
        }}
      >
        {takeover ? (
          <>
            <Button
              theme="solid"
              type="warning"
              size="small"
              loading={takeoverBusy}
              onClick={() => void setTakeover(false)}
            >
              Resume agent
            </Button>
            <Typography.Text type="warning" size="small">
              Click / scroll / type on the preview.
            </Typography.Text>
          </>
        ) : (
          <>
            <Button
              theme="solid"
              type="primary"
              size="small"
              loading={takeoverBusy}
              onClick={() => void setTakeover(true)}
            >
              {attached ? 'Takeover' : 'Open & Takeover'}
            </Button>
            <Typography.Text type="tertiary" size="small">
              {attached
                ? 'Pause the agent and solve captchas yourself.'
                : 'Start the System Agent browser, then interact.'}
            </Typography.Text>
          </>
        )}
      </div>

      {error && (
        <Banner
          fullMode={false}
          type="danger"
          description={error}
          closeIcon={null}
          style={{ margin: 0, borderRadius: 0 }}
        />
      )}

      <div
        style={{
          position: 'relative',
          display: 'flex',
          flex: 1,
          minHeight: 220,
          alignItems: 'center',
          justifyContent: 'center',
          overflow: 'hidden',
          background: 'oklch(12% 0.01 55)',
        }}
      >
        {frameUrl ? (
          <>
            <img
              ref={imgRef}
              src={frameUrl}
              alt="Agent browser"
              style={{
                maxHeight: '100%',
                maxWidth: '100%',
                userSelect: 'none',
                objectFit: 'contain',
                cursor: takeover ? 'crosshair' : undefined,
              }}
              draggable={false}
              onClick={(e) => {
                if (!takeover) {
                  void setTakeover(true)
                  return
                }
                void sendClick(e)
              }}
              onMouseMove={sendMove}
            />
            {!takeover && (
              <Button
                theme="solid"
                type="tertiary"
                size="small"
                style={{
                  position: 'absolute',
                  left: '50%',
                  bottom: 12,
                  transform: 'translateX(-50%)',
                }}
                onClick={() => void setTakeover(true)}
              >
                Click preview or Takeover to interact
              </Button>
            )}
          </>
        ) : (
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              alignItems: 'center',
              gap: 12,
              padding: 16,
              textAlign: 'center',
            }}
          >
            {takeoverBusy ? (
              <Spin tip="Starting browser…" />
            ) : (
              <Typography.Text type="tertiary" size="small">
                No live frame yet. Open the browser to start.
              </Typography.Text>
            )}
            <Button
              theme="solid"
              type="primary"
              size="small"
              loading={takeoverBusy}
              onClick={() => void setTakeover(true)}
            >
              Open &amp; Takeover
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}
