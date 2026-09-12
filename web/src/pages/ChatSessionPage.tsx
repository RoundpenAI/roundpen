import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import {
  AIChatDialogue,
  AIChatInput,
  Banner,
  Button,
  Modal,
  Spin,
  Typography,
} from '@douyinfe/semi-ui-19'
import type { MessageContent } from '@douyinfe/semi-ui-19/lib/es/aiChatInput/interface'
import {
  agents,
  assistantsApi,
  type AgentMessage,
  type AgentSession,
  ApiError,
} from '../api'
import { AgentBrowserPanel } from '../components/AgentBrowserPanel'
import {
  agentMessagesToSemi,
  type SemiChatMessage,
} from '../lib/semiChatAdapter'
import { chatDialogueRenderConfig } from '../components/chatDialogueRender'
import {
  WS_CONNECT_TIMEOUT_MS,
  wsCanSendProp,
  wsCloseDetail,
  wsConnectTimeoutDetail,
  wsInputPlaceholder,
  wsReconnectDelayMs,
  wsStatusLabel,
  type WsUiStatus,
} from '../lib/sessionWsUi'
import {
  drainOutbox,
  enqueueOutbox,
  outboxWaitingHint,
} from '../lib/sessionOutbox'
import {
  detachSocket,
  shouldApplySocketOpen,
  socketLooksOpen,
} from '../lib/sessionWsConnect'

type PermReq = {
  requestId: string
  title: string
  options: { optionId: string; name: string; kind?: string }[]
  ticketId?: string
}

const ROLE_CONFIG = {
  user: { name: '' },
  assistant: { name: '' },
  system: { name: '' },
}

const DIALOGUE_RENDER = chatDialogueRenderConfig()

function pickOrdinaryAllow(
  options: { optionId: string; kind?: string }[],
): string | null {
  const once = options.find(
    (o) =>
      o.kind === 'allow_once' ||
      o.optionId === 'allow_once' ||
      o.optionId === 'allow',
  )
  if (once) return once.optionId
  return options.find((o) => o.optionId === 'allow_tool')?.optionId ?? null
}

function readAutoMode(): boolean {
  try {
    const v = localStorage.getItem('roundpen.chat.auto')
    return v !== '0'
  } catch {
    return true
  }
}

function isBrowserTool(title: string): boolean {
  return title.startsWith('browser_')
}

function nowIso(): string {
  return new Date().toISOString()
}

function isStreamingMessage(m: AgentMessage): boolean {
  return m.meta?.status === 'in_progress' || m.meta?.status === 'pending'
}

function clearStreaming(prev: AgentMessage[]): AgentMessage[] {
  return prev.map((m) =>
    isStreamingMessage(m)
      ? { ...m, meta: { ...m.meta, status: 'completed' } }
      : m,
  )
}

function upsertToolMessage(
  prev: AgentMessage[],
  sessionId: string,
  patch: {
    toolId: string
    title?: string
    status?: string
    kind?: string
    input?: unknown
    output?: unknown
  },
): AgentMessage[] {
  const idx = prev.findIndex((m) => m.meta?.toolId === patch.toolId)
  if (idx >= 0) {
    const cur = prev[idx]
    const next = [...prev]
    next[idx] = {
      ...cur,
      content:
        typeof patch.output === 'string'
          ? patch.output
          : patch.output !== undefined
            ? formatUnknown(patch.output)
            : cur.content,
      meta: {
        ...cur.meta,
        type: 'tool_call',
        toolId: patch.toolId,
        title: patch.title || cur.meta?.title,
        status: patch.status || cur.meta?.status,
        kind: patch.kind || cur.meta?.kind,
        input: patch.input !== undefined ? patch.input : cur.meta?.input,
        output: patch.output !== undefined ? patch.output : cur.meta?.output,
      },
    }
    return next
  }
  return [
    ...prev,
    {
      id: `tool-${patch.toolId}`,
      sessionId,
      role: 'tool',
      content:
        typeof patch.output === 'string'
          ? patch.output
          : patch.output !== undefined
            ? formatUnknown(patch.output)
            : '',
      meta: {
        type: 'tool_call',
        toolId: patch.toolId,
        title: patch.title || patch.toolId,
        status: patch.status || 'pending',
        kind: patch.kind,
        input: patch.input,
        output: patch.output,
      },
      createdAt: nowIso(),
    },
  ]
}

function formatUnknown(value: unknown): string {
  if (value == null) return ''
  if (typeof value === 'string') return value
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function appendThoughtMessage(
  prev: AgentMessage[],
  sessionId: string,
  text: string,
): AgentMessage[] {
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
    if (typ === 'thought' || m.role === 'thought') {
      const next = [...prev]
      next[i] = {
        ...m,
        content: m.content + text,
        meta: { ...m.meta, type: 'thought', status: 'in_progress' },
      }
      return next
    }
  }
  return [
    ...prev,
    {
      id: `thought-${Date.now()}`,
      sessionId,
      role: 'assistant',
      content: text,
      meta: { type: 'thought', status: 'in_progress' },
      createdAt: nowIso(),
    },
  ]
}

function shouldShowInDialogue(m: AgentMessage): boolean {
  const typ = m.meta?.type || m.role
  if (typ === 'permission' || m.role === 'permission') {
    const outcome = m.meta?.outcome
    if (outcome === 'requested' || outcome === 'auto') return false
  }
  return true
}

function messageContentToPlainText(payload: MessageContent): string {
  const parts = payload.inputContents ?? []
  return parts
    .map((c) => (typeof c.text === 'string' ? c.text : ''))
    .join('')
    .trim()
}

function contentsHaveSendableText(
  contents: Array<{ text?: unknown }> | undefined,
): boolean {
  if (!contents?.length) return false
  return contents.some(
    (c) => typeof c.text === 'string' && c.text.trim().length > 0,
  )
}

const sentPending = new Set<string>()

function pendingKey(sessionId: string) {
  return `roundpen.pendingPrompt.${sessionId}`
}

function stashPendingPrompt(sessionId: string, text: string) {
  if (!sessionId || !text) return
  try {
    sessionStorage.setItem(pendingKey(sessionId), text)
  } catch {
    /* ignore */
  }
}

function readPendingPrompt(sessionId: string): string {
  try {
    return sessionStorage.getItem(pendingKey(sessionId)) ?? ''
  } catch {
    return ''
  }
}

function clearPendingPrompt(sessionId: string) {
  try {
    sessionStorage.removeItem(pendingKey(sessionId))
  } catch {
    /* ignore */
  }
}

export function ChatSessionPage() {
  const { id = '' } = useParams()
  const location = useLocation()
  const navigate = useNavigate()
  const [session, setSession] = useState<AgentSession | null>(null)
  const [messages, setMessages] = useState<AgentMessage[]>([])
  const [busy, setBusy] = useState(false)
  const [statusHint, setStatusHint] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [perm, setPerm] = useState<PermReq | null>(null)
  const [showBrowser, setShowBrowser] = useState(false)
  const [browserSeen, setBrowserSeen] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)
  const outboxRef = useRef<string[]>([])
  const scrollRef = useRef<HTMLDivElement | null>(null)
  const contentRef = useRef<HTMLDivElement | null>(null)
  const initialScrollDone = useRef(false)
  const [histReady, setHistReady] = useState(false)
  const [wsStatus, setWsStatus] = useState<WsUiStatus>('connecting')
  const [wsDetail, setWsDetail] = useState<string | null>(null)
  const wsOpen = wsStatus === 'open'
  const [autoMode, setAutoMode] = useState(readAutoMode)
  const [composerFocused, setComposerFocused] = useState(false)
  const [composerHasText, setComposerHasText] = useState(false)
  const composerIdle = !composerHasText && !busy
  const showComposerSend = busy || composerHasText

  const chats: SemiChatMessage[] = useMemo(
    () => agentMessagesToSemi(messages.filter(shouldShowInDialogue)),
    [messages],
  )

  useEffect(() => {
    const incoming =
      (location.state as { pendingPrompt?: string } | null)?.pendingPrompt ?? ''
    if (!incoming) return
    stashPendingPrompt(id, incoming)
    navigate(location.pathname, { replace: true, state: {} })
  }, [id, location.pathname, location.state, navigate])

  useEffect(() => {
    let cancelled = false
    setHistReady(false)
    ;(async () => {
      try {
        const [sess, hist] = await Promise.all([
          agents.getSession(id),
          agents.messages(id),
        ])
        if (cancelled) return
        setSession(sess)
        setMessages(hist.messages ?? [])
        setHistReady(true)
      } catch (e) {
        if (!cancelled) setError(e instanceof ApiError ? e.message : String(e))
      }
    })()
    return () => {
      cancelled = true
    }
  }, [id])

  useEffect(() => {
    outboxRef.current = []
  }, [id])

  useEffect(() => {
    if (!id) return
    let disposed = false
    let attempt = 0
    let retryTimer: number | null = null
    let openTimer: number | null = null
    let pollTimer: number | null = null
    let announcedOpen = false
    let ws: WebSocket | null = null
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
      setBusy(false)
      const waiting = outboxWaitingHint(outboxRef.current.length)
      setStatusHint(waiting)
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
          flushOutbox(socket)
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
          }
          if (msg.type === 'hello') {
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
              setStatusHint(e.text)
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
            const options = msg.options ?? []
            const autoOpt = readAutoMode() ? pickOrdinaryAllow(options) : null
            if (autoOpt && wsRef.current?.readyState === WebSocket.OPEN) {
              wsRef.current.send(
                JSON.stringify({
                  type: 'permission',
                  requestId: msg.requestId ?? '',
                  optionId: autoOpt,
                }),
              )
              return
            }
            setStatusHint('等待你处理协助单…')
            setPerm({
              requestId: msg.requestId ?? '',
              title: msg.title ?? 'Permission',
              options,
              ticketId: (msg as { ticketId?: string }).ticketId,
            })
          } else if (msg.type === 'done') {
            setBusy(false)
            setStatusHint(null)
            setMessages((prev) => clearStreaming(prev))
          } else if (msg.type === 'error') {
            setBusy(false)
            setStatusHint(null)
            setMessages((prev) => clearStreaming(prev))
            const msgText = msg.message ?? 'error'
            setError(msgText)
            setWsStatus('error')
            setWsDetail(msgText)
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
        flushOutbox(socket)
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
  }, [id])

  useEffect(() => {
    initialScrollDone.current = false
  }, [id])

  useLayoutEffect(() => {
    if (!histReady) return
    const scroller = scrollRef.current
    const content = contentRef.current
    if (!scroller || !content) return

    const pinBottom = () => {
      scroller.scrollTop = scroller.scrollHeight
    }

    // Live updates after the first pin: jump once per change.
    if (initialScrollDone.current) {
      pinBottom()
      return
    }

    // First paint after history: keep pinning while Markdown/layout grows,
    // otherwise refresh lands mid last-message.
    pinBottom()
    let settleTimer = 0
    const ro = new ResizeObserver(() => {
      pinBottom()
      window.clearTimeout(settleTimer)
      settleTimer = window.setTimeout(() => {
        pinBottom()
        initialScrollDone.current = true
        ro.disconnect()
      }, 120)
    })
    ro.observe(content)
    const maxWait = window.setTimeout(() => {
      pinBottom()
      initialScrollDone.current = true
      ro.disconnect()
    }, 2000)
    return () => {
      window.clearTimeout(settleTimer)
      window.clearTimeout(maxWait)
      ro.disconnect()
    }
  }, [histReady, chats, perm, statusHint, busy])

  const toggleAuto = () => {
    const next = !autoMode
    setAutoMode(next)
    try {
      localStorage.setItem('roundpen.chat.auto', next ? '1' : '0')
    } catch {
      /* ignore */
    }
    const ws = wsRef.current
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ type: 'auto', enabled: next }))
    }
  }

  const appendLocalUser = (text: string) => {
    setMessages((prev) => [
      ...clearStreaming(prev),
      {
        id: `${Date.now()}-u`,
        sessionId: id,
        role: 'user',
        content: text,
        createdAt: nowIso(),
      },
    ])
  }

  const sendPrompt = (ws: WebSocket, text: string) => {
    setBusy(true)
    setStatusHint('Working…')
    setError(null)
    appendLocalUser(text)
    ws.send(JSON.stringify({ type: 'prompt', text }))
  }

  const handleMessageSend = (payload: MessageContent) => {
    const text = messageContentToPlainText(payload)
    if (!text) return
    setComposerHasText(false)
    setComposerFocused(false)
    const ws = wsRef.current
    if (ws && ws.readyState === WebSocket.OPEN) {
      sendPrompt(ws, text)
      return
    }
    // WeChat-style: show the bubble immediately; deliver when WS is ready.
    appendLocalUser(text)
    outboxRef.current = enqueueOutbox(outboxRef.current, text)
    setStatusHint(outboxWaitingHint(outboxRef.current.length))
    setError(null)
  }

  useEffect(() => {
    const pending = readPendingPrompt(id).trim()
    const ws = wsRef.current
    if (!pending || sentPending.has(id) || !histReady) {
      return
    }
    sentPending.add(id)
    clearPendingPrompt(id)
    if (ws && ws.readyState === WebSocket.OPEN) {
      sendPrompt(ws, pending)
      return
    }
    appendLocalUser(pending)
    outboxRef.current = enqueueOutbox(outboxRef.current, pending)
    setStatusHint(outboxWaitingHint(outboxRef.current.length))
  }, [histReady, wsOpen, id])

  const answerPerm = (optionId: string) => {
    if (!perm || !wsRef.current) return
    wsRef.current.send(
      JSON.stringify({
        type: 'permission',
        requestId: perm.requestId,
        optionId,
      }),
    )
    if (perm.ticketId) {
      const resolution =
        optionId.includes('reject') || optionId === 'reject'
          ? 'reject'
          : 'allow_once'
      void assistantsApi
        .resolveTicket(perm.ticketId, { resolution, note: optionId })
        .catch(() => undefined)
    }
    setMessages((prev) => [
      ...prev,
      {
        id: `perm-${Date.now()}`,
        sessionId: id,
        role: 'permission',
        content: `协助单已处理 · ${optionId}`,
        meta: {
          type: 'permission',
          title: perm.title,
          optionId,
          outcome: 'selected',
        },
        createdAt: nowIso(),
      },
    ])
    setPerm(null)
    setStatusHint('工作中')
  }

  const showWorking =
    busy &&
    !perm &&
    (statusHint != null ||
      !messages.some(
        (m) =>
          isStreamingMessage(m) &&
          (m.meta?.type === 'agent_message' ||
            (m.role === 'assistant' &&
              !m.meta?.type &&
              !m.meta?.toolId)),
      ))

  return (
    <div className="chat-thread">
      <div
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'center',
          gap: 8,
          padding: '8px 16px',
        }}
      >
        {session?.assistantId && (
          <Link
            to={`/a/${session.assistantId}`}
            style={{ color: 'var(--semi-color-link)', fontSize: 12 }}
          >
            助手详情
          </Link>
        )}
        {session && (
          <Typography.Text type="tertiary" size="small">
            {busy ? '工作中' : session.status}
          </Typography.Text>
        )}
        <Typography.Text
          type={wsStatus === 'open' ? 'tertiary' : 'danger'}
          size="small"
          title={wsDetail ?? undefined}
        >
          {wsStatusLabel(wsStatus, wsDetail)}
        </Typography.Text>
        <Button
          size="small"
          theme={autoMode ? 'solid' : 'borderless'}
          type={autoMode ? 'primary' : 'tertiary'}
          style={{ marginLeft: 'auto' }}
          title={
            autoMode
              ? '自动批准常见工具权限'
              : '工具权限需你确认（协助单）'
          }
          onClick={toggleAuto}
        >
          Auto
        </Button>
        <Button
          size="small"
          theme="borderless"
          type="tertiary"
          onClick={() => setShowBrowser((v) => !v)}
        >
          {showBrowser ? '收起浏览器' : '打开画面'}
        </Button>
      </div>

      {error && (
        <div role="alert" style={{ padding: '0 16px 8px' }}>
          <Banner
            fullMode={false}
            type="danger"
            description={error}
            closeIcon={null}
          />
        </div>
      )}

      <div
        className={
          showBrowser ? 'chat-browser-split is-open' : 'chat-browser-split'
        }
      >
        <div className="chat-pane">
          <div
            ref={scrollRef}
            className="chat-pane-scroll"
            style={{ padding: '12px 16px', fontSize: 14 }}
          >
            <div
              ref={contentRef}
              style={{ maxWidth: 768, margin: '0 auto', minWidth: 0 }}
            >
              {chats.length === 0 && !showWorking && (
                <Typography.Text
                  type="tertiary"
                  style={{ display: 'block', textAlign: 'center', padding: 64 }}
                >
                  Waiting for the agent…
                </Typography.Text>
              )}
              {chats.length > 0 && (
                <AIChatDialogue
                  align="leftRight"
                  mode="bubble"
                  chats={chats}
                  roleConfig={ROLE_CONFIG}
                  dialogueRenderConfig={DIALOGUE_RENDER}
                />
              )}
              {showWorking && (
                <div
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 8,
                    fontSize: 14,
                    opacity: 0.7,
                  }}
                  aria-live="polite"
                >
                  <Spin size="small" />
                  <Typography.Text ellipsis style={{ minWidth: 0 }}>
                    {statusHint ?? '工作中'}
                    {session?.assistantId && (
                      <>
                        {' · '}
                        <Link
                          to={`/a/${session.assistantId}`}
                          style={{ color: 'var(--semi-color-link)' }}
                        >
                          查看此刻
                        </Link>
                      </>
                    )}
                  </Typography.Text>
                </div>
              )}
            </div>
          </div>

          <div
            className={[
              'chat-composer-dock',
              composerIdle ? 'is-idle' : '',
              composerFocused ? 'is-focused' : '',
            ]
              .filter(Boolean)
              .join(' ')}
          >
            <div className="chat-composer-dock-inner">
              <AIChatInput
                keepSkillAfterSend={false}
                generating={busy}
                canSend={wsCanSendProp(wsStatus) && composerHasText}
                showUploadButton={false}
                showUploadFile={false}
                showReference={false}
                round
                placeholder={wsInputPlaceholder(wsStatus)}
                renderConfigureArea={() => null}
                renderActionArea={({ menuItem, className }) => {
                  if (!showComposerSend) return null
                  const sendBtn = menuItem[menuItem.length - 1]
                  return <div className={className}>{sendBtn}</div>
                }}
                onFocus={() => setComposerFocused(true)}
                onBlur={() => setComposerFocused(false)}
                onContentChange={(contents) => {
                  setComposerHasText(contentsHaveSendableText(contents))
                }}
                onMessageSend={handleMessageSend}
                onStopGenerate={() =>
                  wsRef.current?.send(JSON.stringify({ type: 'cancel' }))
                }
              />
            </div>
          </div>
        </div>

        {showBrowser && id && (
          <div className="chat-browser-side">
            <AgentBrowserPanel
              sessionId={id}
              active={showBrowser}
              busy={busy || browserSeen}
              onClose={() => setShowBrowser(false)}
            />
          </div>
        )}
      </div>

      <Modal
        title="协助单"
        visible={perm != null}
        closable={false}
        maskClosable={false}
        onCancel={() => undefined}
        footer={
          <div
            style={{
              display: 'flex',
              flexWrap: 'wrap',
              gap: 8,
              justifyContent: 'flex-end',
            }}
          >
            {perm?.options.map((o) => (
              <Button
                key={o.optionId}
                type={
                  o.optionId === 'allow_all' || o.optionId === 'allow'
                    ? 'primary'
                    : o.optionId.includes('reject')
                      ? 'tertiary'
                      : 'secondary'
                }
                disabled={!wsOpen}
                onClick={() => answerPerm(o.optionId)}
              >
                {o.name || o.optionId}
              </Button>
            ))}
            {showBrowser || browserSeen ? (
              <Button type="tertiary" onClick={() => setShowBrowser(true)}>
                打开协助画面
              </Button>
            ) : null}
          </div>
        }
      >
        <Typography.Paragraph>{perm?.title}</Typography.Paragraph>
        <Typography.Text type="tertiary" size="small">
          为什么找你：工具需要你的确认才能继续。
          {perm?.ticketId ? ` · #${perm.ticketId.slice(0, 8)}` : ''}
        </Typography.Text>
      </Modal>
    </div>
  )
}
