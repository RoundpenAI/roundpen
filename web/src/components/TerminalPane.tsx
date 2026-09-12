import { useCallback, useEffect, useRef, useState } from 'react'
import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { Button, Spin, Typography } from '@douyinfe/semi-ui-19'
import { IconRefresh } from '@douyinfe/semi-icons'
import { terminalWsUrl } from '../api'

type Props = {
  sandboxId: string
  disabled?: boolean
}

type ConnState = 'connecting' | 'open' | 'closed'

/**
 * Terminal UX after Ctrl+D / exit:
 * - shell ends → WebSocket closes → show a clear "session ended" line
 * - Enter or the Reconnect button opens a fresh PTY (scrollback kept)
 * - intentional disconnect on unmount does not auto-reconnect
 */
export function TerminalPane({ sandboxId, disabled }: Props) {
  const hostRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const [conn, setConn] = useState<ConnState>('connecting')
  const [epoch, setEpoch] = useState(0)

  const reconnect = useCallback(() => {
    setEpoch((n) => n + 1)
  }, [])

  useEffect(() => {
    if (disabled || !hostRef.current) return

    let term = termRef.current
    let fit = fitRef.current
    if (!term) {
      term = new Terminal({
        cursorBlink: true,
        fontFamily: '"IBM Plex Mono", ui-monospace, monospace',
        fontSize: 13,
        theme: {
          background: '#161310',
          foreground: '#e8e0d4',
          cursor: '#c46a3a',
          selectionBackground: '#c46a3a55',
        },
      })
      fit = new FitAddon()
      term.loadAddon(fit)
      term.open(hostRef.current)
      termRef.current = term
      fitRef.current = fit
    }
    try {
      fit?.fit()
    } catch {
      /* ignore */
    }

    setConn('connecting')
    if (epoch > 0) {
      term.writeln('\r\n\x1b[90m[new session]\x1b[0m')
    }

    const ws = new WebSocket(terminalWsUrl(sandboxId))
    ws.binaryType = 'arraybuffer'
    wsRef.current = ws
    let closedByUs = false

    ws.onopen = () => {
      setConn('open')
      ws.send(JSON.stringify({ rows: term!.rows, cols: term!.cols }))
      term!.focus()
    }

    ws.onmessage = (ev) => {
      if (ev.data instanceof ArrayBuffer) {
        term!.write(new Uint8Array(ev.data))
      } else if (typeof ev.data === 'string') {
        term!.write(ev.data)
      }
    }

    ws.onclose = () => {
      if (wsRef.current === ws) wsRef.current = null
      setConn('closed')
      if (!closedByUs) {
        term!.writeln(
          '\r\n\x1b[90m[session ended — Enter or Reconnect for a new shell]\x1b[0m',
        )
      }
    }

    const onData = term.onData((data) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(new TextEncoder().encode(data))
        return
      }
      // Shell already exited: Enter starts a fresh session.
      if (data === '\r' || data === '\n') {
        reconnect()
      }
    })

    const onResize = term.onResize(({ cols, rows }) => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(JSON.stringify({ rows, cols }))
      }
    })

    const ro = new ResizeObserver(() => {
      try {
        fit?.fit()
      } catch {
        /* ignore */
      }
    })
    ro.observe(hostRef.current)

    return () => {
      closedByUs = true
      onData.dispose()
      onResize.dispose()
      ro.disconnect()
      if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) {
        ws.close()
      }
      if (wsRef.current === ws) wsRef.current = null
    }
  }, [sandboxId, disabled, epoch, reconnect])

  useEffect(() => {
    return () => {
      termRef.current?.dispose()
      termRef.current = null
      fitRef.current = null
    }
  }, [sandboxId, disabled])

  if (disabled) {
    return (
      <div
        style={{
          display: 'flex',
          height: '100%',
          alignItems: 'center',
          justifyContent: 'center',
          padding: 16,
          textAlign: 'center',
        }}
      >
        <Typography.Text type="tertiary" size="small">
          Terminal needs a running sandbox.
        </Typography.Text>
      </div>
    )
  }

  const statusLabel =
    conn === 'open'
      ? 'connected'
      : conn === 'connecting'
        ? 'connecting…'
        : 'disconnected'

  return (
    <div
      style={{
        position: 'relative',
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        minHeight: 0,
      }}
    >
      <div
        style={{
          display: 'flex',
          flexShrink: 0,
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 8,
          borderBottom: '1px solid var(--semi-color-border)',
          padding: '4px 12px',
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <Typography.Text
            size="small"
            type="tertiary"
            style={{
              fontSize: 11,
              fontWeight: 500,
              letterSpacing: '0.06em',
              textTransform: 'uppercase',
            }}
          >
            Terminal
          </Typography.Text>
          {conn === 'connecting' ? (
            <Spin size="small" />
          ) : (
            <Typography.Text
              size="small"
              type="tertiary"
              style={{
                fontFamily: 'var(--semi-font-family-code)',
                fontSize: 11,
              }}
            >
              {statusLabel}
            </Typography.Text>
          )}
        </div>
        <Button
          theme="borderless"
          type="tertiary"
          size="small"
          icon={<IconRefresh />}
          disabled={conn === 'connecting'}
          onClick={() => reconnect()}
          title="Open a new shell session"
        >
          Reconnect
        </Button>
      </div>
      <div ref={hostRef} style={{ flex: 1, minHeight: 0, overflow: 'hidden' }} />
    </div>
  )
}
