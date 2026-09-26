import type { Dispatch, SetStateAction } from 'react'
import { agents, type AgentMessage } from '../../api'
import { drainOutbox, outboxWaitingHint } from '../../lib/sessionOutbox'
import {
  detachSocket,
  shouldApplySocketOpen,
  socketLooksOpen,
} from '../../lib/sessionWsConnect'
import {
  WS_CONNECT_TIMEOUT_MS,
  wsCloseDetail,
  wsConnectTimeoutDetail,
  wsReconnectDelayMs,
  type WsUiStatus,
} from '../../lib/sessionWsUi'
import {
  appendThoughtMessage,
  clearStreaming,
  isBrowserTool,
  isStreamingMessage,
  nowIso,
  readAutoMode,
  upsertToolMessage,
  type PermReq,
} from './sessionChatHelpers'
import { parseQueueItems, type QueueItem } from './sessionQueue'

export type ChatSocketDeps = {
  id: string
  wsRef: { current: WebSocket | null }
  outboxRef: { current: string[] }
  busyRef: { current: boolean }
  setBusy: Dispatch<SetStateAction<boolean>>
  setError: Dispatch<SetStateAction<string | null>>
  setMessages: Dispatch<SetStateAction<AgentMessage[]>>
  setPerm: Dispatch<SetStateAction<PermReq | null>>
  setStatusHint: Dispatch<SetStateAction<string | null>>
  setWsStatus: Dispatch<SetStateAction<WsUiStatus>>
  setWsDetail: Dispatch<SetStateAction<string | null>>
  setBrowserSeen: Dispatch<SetStateAction<boolean>>
  /** Queue snapshot changes (version-checked before this fires). */
  onQueue: (items: QueueItem[]) => void
  /** Mid-turn injection capability as reported by the runner. */
  setSteerCap: (v: boolean) => void
  refreshCommands: () => void
}

export function connectChatSocket(deps: ChatSocketDeps): (() => void) | undefined {
  const {
    id,
    wsRef,
    outboxRef,
    busyRef,
    setBusy,
    setError,
    setMessages,
    setPerm,
    setStatusHint,
    setWsStatus,
    setWsDetail,
    setBrowserSeen,
    onQueue,
    setSteerCap,
    refreshCommands,
  } = deps
  if (!id) return
  let disposed = false
  let attempt = 0
  let retryTimer: number | null = null
  let openTimer: number | null = null
  let pollTimer: number | null = null
  let announcedOpen = false
  let ws: WebSocket | null = null
  // Queue snapshots are versioned server-side; a fresh connection restarts the
  // sequence, so the high-water mark resets on every connect.
  let queueVer = 0
  // Reset before connect so Strict Mode remount cannot leave chrome on
  // 「已连接」while the live socket is still connecting / null.
  setWsStatus('connecting')
  setWsDetail(null)

  const clearOpenTimer = () => {
    if (openTimer != null) {
      window.clearTimeout(openTimer)
      openTimer = null
    }
  }

  const clearPollTimer = () => {
    if (pollTimer != null) {
      window.clearInterval(pollTimer)
      pollTimer = null
    }
  }

  /** @returns true only on the connecting → open transition */
  const markOpen = (socket: WebSocket) => {
    if (!shouldApplySocketOpen(disposed, wsRef.current, socket)) return false
    if (!socketLooksOpen(socket)) return false
    clearOpenTimer()
    clearPollTimer()
    attempt = 0
    setError(null)
    setWsDetail(null)
    setWsStatus('open')
    if (announcedOpen) return false
    announcedOpen = true
    return true
  }

  const scheduleReconnect = (detail: string) => {
    setWsStatus('error')
    setWsDetail(detail)
    // Do NOT clear busy here: an in-flight turn on the server keeps running
    // while this tab reconnects; the status snapshot will reconcile it.
    if (!busyRef.current) {
      setStatusHint(outboxWaitingHint(outboxRef.current.length))
    }
    const delay = wsReconnectDelayMs(attempt)
    attempt += 1
    if (retryTimer != null) {
      window.clearTimeout(retryTimer)
    }
    retryTimer = window.setTimeout(connect, delay)
  }

  const flushOutbox = (socket: WebSocket) => {
    const { remaining, items } = drainOutbox(outboxRef.current)
    outboxRef.current = remaining
    for (const text of items) {
      setBusy(true)
      setStatusHint('Working…')
      setError(null)
      socket.send(JSON.stringify({ type: 'prompt', text }))
    }
  }

  const refetchMessages = () => {
    agents
      .messages(id)
      .then((res) => {
        if (disposed || !wsRef.current) return
        setMessages(res.messages ?? [])
      })
      .catch(() => undefined)
  }

  // Re-seed the live assistant bubble from the server's authoritative
  // snapshot after a reconnect, so a partial reply is never doubled.
  const restoreStreamingReply = (reply: string) => {
    setMessages((prev) => {
      const last = prev[prev.length - 1]
      const lastTyp = last?.meta?.type
      const isLiveAssistant =
        last &&
        last.role === 'assistant' &&
        !last.meta?.toolId &&
        isStreamingMessage(last) &&
        (lastTyp === 'agent_message' || !lastTyp)
      if (isLiveAssistant) {
        return [
          ...prev.slice(0, -1),
          {
            ...last,
            content: reply,
            meta: {
              ...last.meta,
              type: 'agent_message',
              status: 'in_progress',
            },
          },
        ]
      }
      return [
        ...prev,
        {
          id: `stream-${Date.now()}`,
          sessionId: id,
          role: 'assistant',
          content: reply,
          meta: { type: 'agent_message', status: 'in_progress' },
          createdAt: nowIso(),
        },
      ]
    })
  }

  const bind = (socket: WebSocket) => {
    socket.onmessage = (ev) => {
      // Any server frame means the upgrade completed — sync UI even if
      // onopen was missed (seen with Vite proxy + slow ACP Start).
      if (markOpen(socket)) {
        try {
          socket.send(
            JSON.stringify({ type: 'auto', enabled: readAutoMode() }),
          )
        } catch {
          /* ignore */
        }
      }
      try {
        const msg = JSON.parse(String(ev.data)) as {
          type: string
          event?: {
            type?: string
            text?: string
            title?: string
            status?: string
            kind?: string
            toolId?: string
            input?: unknown
            output?: unknown
          }
          message?: string
          stopReason?: string
          requestId?: string
          title?: string
          options?: { optionId: string; name: string; kind?: string }[]
          busy?: boolean
          reply?: string
          thought?: string
          pending?: unknown
          queueVersion?: number
          version?: number
          items?: unknown
          steer?: boolean
          perm?: {
            requestId?: string
            title?: string
            options?: { optionId: string; name: string; kind?: string }[]
            ticketId?: string
          }
        }
        if (msg.type === 'hello') {
          return
        }
        // Server snapshot of the runner: reconcile busy/reply/thought/perm
        // so a reconnect into an in-flight turn is seamless.
        if (msg.type === 'status') {
          // The queue snapshot is authoritative here (reconnect baseline);
          // version-check so an overtaken frame cannot revive stale items.
          const ver = typeof msg.queueVersion === 'number' ? msg.queueVersion : 0
          if (ver >= queueVer) {
            queueVer = ver
            onQueue(parseQueueItems(msg.pending))
          }
          if (typeof msg.steer === 'boolean') {
            setSteerCap(msg.steer)
          }
          if (msg.busy) {
            setBusy(true)
            setError(null)
            setStatusHint('工作中')
            if (msg.reply) {
              restoreStreamingReply(msg.reply)
            }
            if (msg.perm) {
              setPerm({
                requestId: msg.perm.requestId ?? '',
                title: msg.perm.title ?? 'Permission',
                options: msg.perm.options ?? [],
                ticketId: msg.perm.ticketId,
              })
            } else {
              setPerm(null)
            }
          } else {
            setBusy(false)
            setStatusHint(null)
            setPerm(null)
            setMessages((prev) => clearStreaming(prev))
            refetchMessages()
            flushOutbox(socket)
          }
          return
        }
        if (msg.type === 'queue') {
          const ver = typeof msg.version === 'number' ? msg.version : 0
          if (ver >= queueVer) {
            queueVer = ver
            onQueue(parseQueueItems(msg.items))
          }
          if (typeof msg.steer === 'boolean') {
            setSteerCap(msg.steer)
          }
          return
        }
        if (msg.type === 'event' && msg.event) {
          const e = msg.event
          if (e.type === 'agent_message' && e.text) {
            setStatusHint(null)
            setMessages((prev) => {
              const last = prev[prev.length - 1]
              const lastTyp = last?.meta?.type
              if (
                last &&
                last.role === 'assistant' &&
                (lastTyp === 'agent_message' || !lastTyp) &&
                isStreamingMessage(last) &&
                !last.meta?.toolId
              ) {
                return [
                  ...prev.slice(0, -1),
                  { ...last, content: last.content + e.text },
                ]
              }
              return [
                ...prev,
                {
                  id: `stream-${Date.now()}`,
                  sessionId: id,
                  role: 'assistant',
                  content: e.text ?? '',
                  meta: { type: 'agent_message', status: 'in_progress' },
                  createdAt: nowIso(),
                },
              ]
            })
            return
          }
          if (e.type === 'agent_thought' && e.text) {
            setMessages((prev) => appendThoughtMessage(prev, id, e.text ?? ''))
            return
          }
          if (e.type === 'permission') {
            if ((e.status || 'auto') === 'auto') return
            setMessages((prev) => [
              ...prev,
              {
                id: `perm-${Date.now()}`,
                sessionId: id,
                role: 'permission',
                content:
                  [e.title, e.text].filter(Boolean).join(' · ') ||
                  'permission',
                meta: {
                  type: 'permission',
                  title: e.title,
                  optionId: e.text,
                  outcome: e.status || 'auto',
                },
                createdAt: nowIso(),
              },
            ])
            return
          }
          if (e.type === 'tool_call' || e.type === 'tool_call_update') {
            const toolId = (e.toolId ?? '').trim() || `anon-${Date.now()}`
            const title = (e.title ?? '').trim()
            const status = (e.status ?? '').trim()
            if (isBrowserTool(title)) {
              setBrowserSeen(true)
            }
            setStatusHint('工作中')
            setMessages((prev) =>
              upsertToolMessage(prev, id, {
                toolId,
                title: title || undefined,
                status: status || undefined,
                kind: e.kind,
                input: e.input,
                output: e.output,
              }),
            )
            return
          }
          setMessages((prev) => [
            ...prev,
            {
              id: `${Date.now()}-${prev.length}`,
              sessionId: id,
              role: 'system',
              content: e.text ?? e.type ?? '',
              meta: { type: 'event' },
              createdAt: nowIso(),
            },
          ])
        } else if (msg.type === 'permission_request') {
          // Auto mode is answered server-side by the policy classifier; a
          // frame reaching the UI always means a human decision is needed.
          const options = msg.options ?? []
          setStatusHint('等待你处理协助单…')
          setPerm({
            requestId: msg.requestId ?? '',
            title: msg.title ?? 'Permission',
            options,
            ticketId: (msg as { ticketId?: string }).ticketId,
          })
        } else if (msg.type === 'permission_resolved') {
          setPerm((cur) =>
            cur && cur.requestId === msg.requestId ? null : cur,
          )
          return
        } else if (msg.type === 'done') {
          setBusy(false)
          setStatusHint(null)
          setPerm(null)
          setMessages((prev) => clearStreaming(prev))
          refetchMessages()
          flushOutbox(socket)
          refreshCommands()
        } else if (msg.type === 'cleared') {
          // /clear only appends a marker row: the transcript stays, the
          // model context restarts after the marker.
          setPerm(null)
          outboxRef.current = []
          onQueue([])
          setMessages((prev) => clearStreaming(prev))
          refetchMessages()
          refreshCommands()
        } else if (msg.type === 'error') {
          setBusy(false)
          setStatusHint(null)
          setMessages((prev) => clearStreaming(prev))
          const msgText = msg.message ?? 'error'
          setError(msgText)
          setWsStatus('error')
          setWsDetail(msgText)
          refetchMessages()
        }
      } catch {
        /* ignore */
      }
    }
    socket.onerror = () => {
      if (!disposed && wsRef.current === socket) {
        setWsDetail('WebSocket 错误')
      }
    }
    socket.onclose = (ev) => {
      clearOpenTimer()
      if (disposed) return
      if (wsRef.current !== socket) return
      wsRef.current = null
      scheduleReconnect(wsCloseDetail(ev.code, ev.reason || ''))
    }
  }

  const connect = () => {
    if (disposed) return
    if (retryTimer != null) {
      window.clearTimeout(retryTimer)
      retryTimer = null
    }
    clearOpenTimer()
    clearPollTimer()
    announcedOpen = false
    queueVer = 0
    setWsStatus('connecting')
    if (attempt === 0) setWsDetail(null)

    const prev = ws
    ws = null
    if (wsRef.current === prev) {
      wsRef.current = null
    }
    detachSocket(prev)

    const socket = new WebSocket(agents.sessionWsUrl(id))
    ws = socket
    wsRef.current = socket

    const onBecameOpen = () => {
      if (!markOpen(socket)) return
      try {
        socket.send(
          JSON.stringify({ type: 'auto', enabled: readAutoMode() }),
        )
      } catch {
        /* ignore */
      }
    }

    socket.onopen = onBecameOpen
    bind(socket)

    if (socketLooksOpen(socket)) {
      onBecameOpen()
    }
    pollTimer = window.setInterval(() => {
      if (disposed || wsRef.current !== socket) {
        clearPollTimer()
        return
      }
      if (socketLooksOpen(socket)) {
        onBecameOpen()
      }
    }, 100)

    openTimer = window.setTimeout(() => {
      openTimer = null
      clearPollTimer()
      if (disposed || wsRef.current !== socket) return
      // Heal: socket is live but UI missed onopen.
      if (socketLooksOpen(socket)) {
        onBecameOpen()
        return
      }
      detachSocket(socket)
      if (wsRef.current === socket) {
        wsRef.current = null
      }
      if (ws === socket) {
        ws = null
      }
      scheduleReconnect(wsConnectTimeoutDetail())
    }, WS_CONNECT_TIMEOUT_MS)
  }

  const reconnectNowIfNeeded = () => {
    if (disposed) return
    if (document.visibilityState === 'hidden') return
    const cur = wsRef.current ?? ws
    if (
      cur &&
      (cur.readyState === WebSocket.OPEN ||
        cur.readyState === WebSocket.CONNECTING)
    ) {
      return
    }
    attempt = 0
    connect()
  }

  document.addEventListener('visibilitychange', reconnectNowIfNeeded)

  connect()
  return () => {
    disposed = true
    document.removeEventListener('visibilitychange', reconnectNowIfNeeded)
    clearOpenTimer()
    clearPollTimer()
    if (retryTimer != null) {
      window.clearTimeout(retryTimer)
      retryTimer = null
    }
    const s = ws
    ws = null
    if (wsRef.current === s) {
      wsRef.current = null
    }
    detachSocket(s)
  }
}
