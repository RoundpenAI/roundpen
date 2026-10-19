import type { Dispatch, SetStateAction } from 'react'
import { agents, type AgentMessage } from '../../api'
import { drainOutbox, outboxWaitingHint } from '../../lib/sessionOutbox'
import {
  detachStream,
  shouldApplyStreamOpen,
  streamLooksOpen,
  STREAM_CLOSED,
  STREAM_CONNECTING,
  STREAM_OPEN,
  type ChatStreamHandle,
} from '../../lib/sessionStreamConnect'
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
  finalizeOpenThoughts,
  isBrowserTool,
  isStreamingMessage,
  MESSAGE_PAGE_SIZE,
  nowIso,
  readPermMode,
  reconcileRefetch,
  upsertToolMessage,
  type PermReq,
} from './sessionChatHelpers'
import { parseQueueItems, type QueueItem } from './sessionQueue'

export type ChatSocketDeps = {
  id: string
  wsRef: { current: ChatStreamHandle | null }
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
  setShowBrowser: Dispatch<SetStateAction<boolean>>
  /** Queue snapshot changes (version-checked before this fires). */
  onQueue: (items: QueueItem[]) => void
  /** Mid-turn injection capability as reported by the runner. */
  setSteerCap: (v: boolean) => void
  refreshCommands: () => void
}

type StreamMsg = {
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
  ticketId?: string
  perm?: {
    requestId?: string
    title?: string
    options?: { optionId: string; name: string; kind?: string }[]
    ticketId?: string
  }
}

/** Parse one SSE chunk buffer; returns leftover incomplete text. */
function consumeSSE(
  chunk: string,
  onData: (json: string) => void,
): string {
  const parts = chunk.split('\n\n')
  const rest = parts.pop() ?? ''
  for (const block of parts) {
    const dataLines: string[] = []
    for (const line of block.split('\n')) {
      if (line.startsWith('data:')) {
        dataLines.push(line.slice(5).trimStart())
      }
    }
    if (dataLines.length) onData(dataLines.join('\n'))
  }
  return rest
}

export function connectChatSocket(
  deps: ChatSocketDeps,
): (() => void) | undefined {
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
    setShowBrowser,
    onQueue,
    setSteerCap,
    refreshCommands,
  } = deps
  if (!id) return
  let disposed = false
  let attempt = 0
  let retryTimer: number | null = null
  let openTimer: number | null = null
  let announcedOpen = false
  let stream: ChatStreamHandle | null = null
  let abort: AbortController | null = null
  // Queue snapshots are versioned server-side; a fresh connection restarts the
  // sequence, so the high-water mark resets on every connect.
  let queueVer = 0
  setWsStatus('connecting')
  setWsDetail(null)

  const clearOpenTimer = () => {
    if (openTimer != null) {
      window.clearTimeout(openTimer)
      openTimer = null
    }
  }

  /** @returns true only on the connecting → open transition */
  const markOpen = (handle: ChatStreamHandle) => {
    if (!shouldApplyStreamOpen(disposed, wsRef.current, handle)) return false
    if (!streamLooksOpen(handle)) return false
    clearOpenTimer()
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

  const flushOutbox = () => {
    const { remaining, items } = drainOutbox(outboxRef.current)
    outboxRef.current = remaining
    for (const text of items) {
      setBusy(true)
      setStatusHint('Working…')
      setError(null)
      void agents.sessionAction(id, { type: 'prompt', text }).catch(() => {
        outboxRef.current = [text, ...outboxRef.current]
      })
    }
  }

  const refetchMessages = () => {
    agents
      .messages(id, { limit: MESSAGE_PAGE_SIZE })
      .then((res) => {
        if (disposed || !wsRef.current) return
        setMessages((prev) => reconcileRefetch(prev, res.messages ?? []))
      })
      .catch(() => undefined)
  }

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

  const restoreStreamingThought = (thought: string) => {
    if (!thought) return
    setMessages((prev) => {
      for (let i = prev.length - 1; i >= 0; i--) {
        const m = prev[i]
        const typ = m.meta?.type || m.role
        if (
          m.role === 'user' ||
          typ === 'agent_message' ||
          (m.role === 'assistant' && typ !== 'thought' && typ !== 'reasoning')
        ) {
          break
        }
        if (typ === 'thought' || typ === 'reasoning' || m.role === 'thought') {
          if (m.meta?.status === 'completed') break
          const next = [...prev]
          next[i] = {
            ...m,
            content: thought,
            meta: { ...m.meta, type: 'thought', status: 'in_progress' },
          }
          return next
        }
      }
      return [
        ...prev,
        {
          id: `thought-${Date.now()}`,
          sessionId: id,
          role: 'assistant',
          content: thought,
          meta: { type: 'thought', status: 'in_progress' },
          createdAt: nowIso(),
        },
      ]
    })
  }

  const handleFrame = (handle: ChatStreamHandle, raw: string) => {
    if (markOpen(handle)) {
      void agents
        .sessionAction(id, { type: 'auto', mode: readPermMode() })
        .catch(() => undefined)
    }
    try {
      const msg = JSON.parse(raw) as StreamMsg
      if (msg.type === 'hello') {
        return
      }
      if (msg.type === 'status') {
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
          if (msg.thought) {
            restoreStreamingThought(msg.thought)
          }
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
          flushOutbox()
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
            const base = finalizeOpenThoughts(prev)
            const last = base[base.length - 1]
            const lastTyp = last?.meta?.type
            if (
              last &&
              last.role === 'assistant' &&
              (lastTyp === 'agent_message' || !lastTyp) &&
              isStreamingMessage(last) &&
              !last.meta?.toolId
            ) {
              return [
                ...base.slice(0, -1),
                { ...last, content: last.content + e.text },
              ]
            }
            return [
              ...base,
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
                [e.title, e.text].filter(Boolean).join(' · ') || 'permission',
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
            if (e.type === 'tool_call') setShowBrowser(true)
          }
          setStatusHint('工作中')
          setMessages((prev) =>
            upsertToolMessage(finalizeOpenThoughts(prev), id, {
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
        const options = msg.options ?? []
        setStatusHint('等待你处理协助单…')
        setPerm({
          requestId: msg.requestId ?? '',
          title: msg.title ?? 'Permission',
          options,
          ticketId: msg.ticketId,
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
        flushOutbox()
        refreshCommands()
      } else if (msg.type === 'cleared') {
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

  const connect = () => {
    if (disposed) return
    if (retryTimer != null) {
      window.clearTimeout(retryTimer)
      retryTimer = null
    }
    clearOpenTimer()
    announcedOpen = false
    queueVer = 0
    setWsStatus('connecting')
    if (attempt === 0) setWsDetail(null)

    const prev = stream
    stream = null
    if (wsRef.current === prev) {
      wsRef.current = null
    }
    if (abort) {
      abort.abort()
      abort = null
    }
    detachStream(prev)

    const ac = new AbortController()
    abort = ac
    const handle: ChatStreamHandle = {
      readyState: STREAM_CONNECTING,
      close: () => {
        handle.readyState = STREAM_CLOSED
        ac.abort()
      },
    }
    stream = handle
    wsRef.current = handle

    openTimer = window.setTimeout(() => {
      openTimer = null
      if (disposed || wsRef.current !== handle) return
      if (streamLooksOpen(handle)) return
      detachStream(handle)
      if (wsRef.current === handle) {
        wsRef.current = null
      }
      if (stream === handle) {
        stream = null
      }
      scheduleReconnect(wsConnectTimeoutDetail())
    }, WS_CONNECT_TIMEOUT_MS)

    void (async () => {
      try {
        const res = await fetch(agents.sessionEventsUrl(id), {
          credentials: 'include',
          headers: { Accept: 'text/event-stream' },
          signal: ac.signal,
        })
        if (!res.ok || !res.body) {
          throw new Error(`events HTTP ${res.status}`)
        }
        handle.readyState = STREAM_OPEN
        if (markOpen(handle)) {
          void agents
            .sessionAction(id, { type: 'auto', mode: readPermMode() })
            .catch(() => undefined)
        }
        const reader = res.body.getReader()
        const dec = new TextDecoder()
        let buf = ''
        for (;;) {
          const { done, value } = await reader.read()
          if (done) break
          buf += dec.decode(value, { stream: true })
          buf = consumeSSE(buf, (json) => handleFrame(handle, json))
        }
        if (disposed || wsRef.current !== handle) return
        handle.readyState = STREAM_CLOSED
        wsRef.current = null
        if (stream === handle) stream = null
        scheduleReconnect(wsCloseDetail(1000, 'stream ended'))
      } catch (err) {
        if (ac.signal.aborted || disposed) return
        if (wsRef.current !== handle) return
        handle.readyState = STREAM_CLOSED
        wsRef.current = null
        if (stream === handle) stream = null
        const detail =
          err instanceof Error && err.message
            ? err.message
            : '事件流连接失败'
        scheduleReconnect(detail)
      }
    })()
  }

  const reconnectNowIfNeeded = () => {
    if (disposed) return
    if (document.visibilityState === 'hidden') return
    const cur = wsRef.current ?? stream
    if (
      cur &&
      (cur.readyState === STREAM_OPEN || cur.readyState === STREAM_CONNECTING)
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
    if (retryTimer != null) {
      window.clearTimeout(retryTimer)
      retryTimer = null
    }
    const s = stream
    stream = null
    if (wsRef.current === s) {
      wsRef.current = null
    }
    if (abort) {
      abort.abort()
      abort = null
    }
    detachStream(s)
  }
}
