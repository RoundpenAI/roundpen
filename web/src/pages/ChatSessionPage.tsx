import { useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import {
  agents,
  assistantsApi,
  type AgentMessage,
  type AgentSession,
  ApiError,
} from '../api'
import { AgentBrowserPanel } from '../components/AgentBrowserPanel'
import { ChatComposer, ChatComposerDock } from '../components/ChatComposer'
import { MarkdownBody } from '../components/MarkdownBody'
import {
  ToolCallGroup,
  type ToolCallData,
} from '../components/ToolCallCard'

type ChatLine =
  | {
      id: string
      kind: 'user' | 'assistant' | 'agent_message' | 'system' | 'event' | 'thought' | 'permission'
      text: string
      streaming?: boolean
      title?: string
      optionId?: string
      outcome?: string
      at?: number
      durationMs?: number
    }
  | {
      id: string
      kind: 'tool_call'
      tool: ToolCallData
    }

type PermReq = {
  requestId: string
  title: string
  options: { optionId: string; name: string; kind?: string }[]
  ticketId?: string
}

function permButtonClass(optionId: string): string {
  switch (optionId) {
    case 'allow_all':
      return 'btn btn-sm btn-primary'
    case 'allow_tool':
      return 'btn btn-sm btn-secondary'
    case 'allow':
      return 'btn btn-sm'
    case 'reject_tool':
    case 'reject':
      return 'btn btn-sm btn-ghost'
    default:
      return 'btn btn-sm'
  }
}

function upsertToolLine(
  prev: ChatLine[],
  patch: Partial<ToolCallData> & { toolId: string },
): ChatLine[] {
  const idx = prev.findIndex(
    (l) => l.kind === 'tool_call' && l.tool.toolId === patch.toolId,
  )
  if (idx >= 0) {
    const cur = prev[idx]
    if (cur.kind !== 'tool_call') return prev
    const next = [...prev]
    next[idx] = {
      ...cur,
      tool: {
        ...cur.tool,
        ...patch,
        title: patch.title || cur.tool.title,
        status: patch.status || cur.tool.status,
        input: patch.input !== undefined ? patch.input : cur.tool.input,
        output: patch.output !== undefined ? patch.output : cur.tool.output,
        kind: patch.kind || cur.tool.kind,
      },
    }
    return next
  }
  return [
    ...prev,
    {
      id: `tool-${patch.toolId}`,
      kind: 'tool_call',
      tool: {
        toolId: patch.toolId,
        title: patch.title || patch.toolId,
        status: patch.status || 'pending',
        kind: patch.kind,
        input: patch.input,
        output: patch.output,
      },
    },
  ]
}

type TextLine = Exclude<ChatLine, { kind: 'tool_call' }>

type ChatBlock =
  | { kind: 'line'; line: TextLine }
  | { kind: 'turn'; id: string; thoughts: TextLine[]; tools: ToolCallData[] }

function messageToChatLine(m: AgentMessage): ChatLine {
  const meta = m.meta
  const typ = meta?.type || m.role
  if (m.role === 'tool' || typ === 'tool_call' || (meta?.toolId && typ !== 'permission')) {
    return {
      id: m.id,
      kind: 'tool_call',
      tool: {
        toolId: meta?.toolId || m.id,
        title: meta?.title || m.content || meta?.toolId || 'tool',
        status: meta?.status || 'completed',
        kind: meta?.kind,
        input: meta?.input,
        output: meta?.output,
      },
    }
  }
  if (m.role === 'thought' || typ === 'thought') {
    const at = Date.parse(m.createdAt)
    return {
      id: m.id,
      kind: 'thought',
      text: m.content,
      at: Number.isNaN(at) ? undefined : at,
      durationMs: meta?.durationMs,
    }
  }
  if (m.role === 'permission' || typ === 'permission') {
    return {
      id: m.id,
      kind: 'permission',
      text: m.content,
      title: meta?.title,
      optionId: meta?.optionId,
      outcome: meta?.outcome,
    }
  }
  return {
    id: m.id,
    kind:
      m.role === 'user'
        ? 'user'
        : m.role === 'assistant'
          ? 'assistant'
          : 'event',
    text: m.content,
  }
}

function ensureTurn(out: ChatBlock[], id: string): Extract<ChatBlock, { kind: 'turn' }> {
  const last = out[out.length - 1]
  if (last?.kind === 'turn') return last
  const turn: Extract<ChatBlock, { kind: 'turn' }> = {
    kind: 'turn',
    id: `turn-${id}`,
    thoughts: [],
    tools: [],
  }
  out.push(turn)
  return turn
}

function groupChatLines(lines: ChatLine[], quiet = true): ChatBlock[] {
  const out: ChatBlock[] = []
  for (const line of lines) {
    if (line.kind === 'permission') continue
    // Quiet mode: tools/thoughts live in 详情「此刻」, not the transcript.
    if (quiet && (line.kind === 'thought' || line.kind === 'tool_call')) {
      continue
    }
    if (line.kind === 'thought') {
      ensureTurn(out, line.id).thoughts.push(line)
      continue
    }
    if (line.kind === 'tool_call') {
      ensureTurn(out, line.id).tools.push(line.tool)
      continue
    }
    out.push({ kind: 'line', line })
  }
  return out
}

function appendThought(prev: ChatLine[], text: string): ChatLine[] {
  for (let i = prev.length - 1; i >= 0; i--) {
    const l = prev[i]
    if (
      l.kind === 'user' ||
      l.kind === 'assistant' ||
      l.kind === 'agent_message'
    ) {
      break
    }
    if (l.kind === 'thought') {
      const next = [...prev]
      next[i] = { ...l, text: l.text + text, streaming: true }
      return next
    }
  }
  return [
    ...prev,
    {
      id: `thought-${Date.now()}`,
      kind: 'thought',
      text,
      streaming: true,
      at: Date.now(),
    },
  ]
}

function thoughtSeconds(thoughts: TextLine[], now: number): number {
  const summed = thoughts.reduce((s, t) => s + (t.durationMs ?? 0), 0)
  if (summed > 0) return summed / 1000
  const times = thoughts
    .map((t) => t.at)
    .filter((n): n is number => typeof n === 'number' && n > 0)
  if (times.length === 0) return thoughts.some((t) => t.streaming) ? 0 : 1
  const start = Math.min(...times)
  const streaming = thoughts.some((t) => t.streaming)
  const end = streaming ? now : Math.max(...times)
  return Math.max(0, (end - start) / 1000)
}

function formatThoughtSecs(sec: number, streaming?: boolean): string {
  if (streaming && sec < 0.5) return 'Thought'
  const n = Math.max(1, Math.round(sec || 1))
  return `Thought ${n}s`
}

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

function permLabel(outcome?: string): string {
  switch (outcome) {
    case 'auto':
      return 'Auto-allowed'
    case 'cancelled':
      return 'Denied'
    case 'requested':
      return 'Asked'
    default:
      return 'Allowed'
  }
}

function ThoughtBlock({
  text,
  streaming,
  seconds,
}: {
  text: string
  streaming?: boolean
  seconds?: number
}) {
  const [open, setOpen] = useState(false)
  return (
    <div className="chat-thought">
      <button
        type="button"
        className="chat-thought-summary"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span className={`chat-tool-chevron ${open ? 'open' : ''}`} aria-hidden>
          ▸
        </span>
        {streaming ? (
          <span className="loading loading-spinner loading-xs shrink-0 opacity-50" />
        ) : null}
        <span>{formatThoughtSecs(seconds ?? 0, streaming)}</span>
      </button>
      {open && <div className="chat-thought-body">{text}</div>}
    </div>
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
  const [lines, setLines] = useState<ChatLine[]>([])
  const [input, setInput] = useState('')
  const [busy, setBusy] = useState(false)
  const [statusHint, setStatusHint] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [perm, setPerm] = useState<PermReq | null>(null)
  const [showBrowser, setShowBrowser] = useState(false)
  const [browserSeen, setBrowserSeen] = useState(false)
  const wsRef = useRef<WebSocket | null>(null)
  const bottomRef = useRef<HTMLDivElement | null>(null)
  const [histReady, setHistReady] = useState(false)
  const [wsOpen, setWsOpen] = useState(false)
  const [autoMode, setAutoMode] = useState(readAutoMode)
  const [now, setNow] = useState(() => Date.now())

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
        setLines((hist.messages ?? []).map(messageToChatLine))
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
    if (!id) return
    let disposed = false
    let attempt = 0
    let retryTimer: number | null = null
    let ws: WebSocket | null = null

    const bind = (socket: WebSocket) => {
    socket.onmessage = (ev) => {
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
        if (msg.type === 'event' && msg.event) {
          const e = msg.event
          if (e.type === 'agent_message' && e.text) {
            setStatusHint(null)
            setLines((prev) => {
              const last = prev[prev.length - 1]
              if (
                last?.kind === 'agent_message' &&
                'streaming' in last &&
                last.streaming
              ) {
                return [
                  ...prev.slice(0, -1),
                  { ...last, text: last.text + e.text },
                ]
              }
              return [
                ...prev,
                {
                  id: `stream-${Date.now()}`,
                  kind: 'agent_message',
                  text: e.text ?? '',
                  streaming: true,
                },
              ]
            })
            return
          }
          if (e.type === 'agent_thought' && e.text) {
            setStatusHint(e.text)
            setLines((prev) => appendThought(prev, e.text ?? ''))
            return
          }
          if (e.type === 'permission') {
            if ((e.status || 'auto') === 'auto') return
            setLines((prev) => [
              ...prev,
              {
                id: `perm-${Date.now()}`,
                kind: 'permission',
                text: [e.title, e.text].filter(Boolean).join(' · ') || 'permission',
                title: e.title,
                optionId: e.text,
                outcome: e.status || 'auto',
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
              // Quiet mode: do not auto-open browser; user opens from 协助/详情.
            }
            if (
              e.type === 'tool_call_update' &&
              (status === 'completed' || status === 'failed')
            ) {
              setStatusHint('工作中')
            } else {
              setStatusHint('工作中')
            }
            setLines((prev) =>
              upsertToolLine(prev, {
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
          setLines((prev) => [
            ...prev,
            {
              id: `${Date.now()}-${prev.length}`,
              kind: 'event',
              text: e.text ?? e.type ?? '',
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
          setLines((prev) =>
            prev.map((l) =>
              (l.kind === 'agent_message' || l.kind === 'thought') && l.streaming
                ? { ...l, streaming: false }
                : l,
            ),
          )
        } else if (msg.type === 'error') {
          setBusy(false)
          setStatusHint(null)
          setLines((prev) =>
            prev.map((l) =>
              (l.kind === 'agent_message' || l.kind === 'thought') && l.streaming
                ? { ...l, streaming: false }
                : l,
            ),
          )
          setError(msg.message ?? 'error')
        }
      } catch {
        /* ignore */
      }
    }
    socket.onerror = () => {
      if (!disposed) setError('WebSocket error')
    }
    socket.onclose = () => {
      if (wsRef.current !== socket) return
      wsRef.current = null
      setWsOpen(false)
      if (disposed) return
      setBusy(false)
      setStatusHint(null)
      const delay = Math.min(1000 * 2 ** attempt, 15000)
      attempt += 1
      retryTimer = window.setTimeout(connect, delay)
    }
    }

    const connect = () => {
      if (disposed) return
      const socket = new WebSocket(agents.sessionWsUrl(id))
      ws = socket
      wsRef.current = socket
      socket.onopen = () => {
        if (disposed) return
        attempt = 0
        setError(null)
        setWsOpen(true)
        socket.send(JSON.stringify({ type: 'auto', enabled: readAutoMode() }))
      }
      bind(socket)
    }

    connect()
    return () => {
      disposed = true
      if (retryTimer != null) window.clearTimeout(retryTimer)
      setWsOpen(false)
      wsRef.current = null
      if (
        ws &&
        (ws.readyState === WebSocket.OPEN ||
          ws.readyState === WebSocket.CONNECTING)
      ) {
        ws.close(1000, 'page dispose')
      }
    }
  }, [id])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [lines, perm, statusHint, busy])

  useEffect(() => {
    if (!busy) return
    const t = window.setInterval(() => setNow(Date.now()), 500)
    return () => window.clearInterval(t)
  }, [busy])

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

  const sendPrompt = (ws: WebSocket, text: string) => {
    setInput('')
    setBusy(true)
    setStatusHint('Working…')
    setError(null)
    setLines((prev) => [
      ...prev.map((l) =>
        (l.kind === 'agent_message' || l.kind === 'thought') && l.streaming
          ? { ...l, streaming: false }
          : l,
      ),
      { id: `${Date.now()}-u`, kind: 'user', text },
    ])
    ws.send(JSON.stringify({ type: 'prompt', text }))
  }

  const send = () => {
    const text = input.trim()
    if (!text || !wsRef.current || wsRef.current.readyState !== WebSocket.OPEN)
      return
    sendPrompt(wsRef.current, text)
  }

  useEffect(() => {
    const pending = readPendingPrompt(id).trim()
    const ws = wsRef.current
    if (
      !pending ||
      sentPending.has(id) ||
      !histReady ||
      !wsOpen ||
      !ws ||
      ws.readyState !== WebSocket.OPEN
    ) {
      return
    }
    sentPending.add(id)
    clearPendingPrompt(id)
    sendPrompt(ws, pending)
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
    setLines((prev) => [
      ...prev,
      {
        id: `perm-${Date.now()}`,
        kind: 'permission',
        text: `协助单已处理 · ${optionId}`,
        title: perm.title,
        optionId,
        outcome: 'selected',
      },
    ])
    setPerm(null)
    setStatusHint('工作中')
  }

  const showWorking =
    busy &&
    !perm &&
    (statusHint != null ||
      !lines.some(
        (l) => l.kind === 'agent_message' && 'streaming' in l && l.streaming,
      ))

  return (
    <div className="chat-thread">
      <div className="flex flex-wrap items-center gap-2 px-4 py-2 text-xs">
        {session?.assistantId && (
          <Link
            to={`/a/${session.assistantId}`}
            className="link link-hover opacity-70"
          >
            助手详情
          </Link>
        )}
        {session && (
          <span className="opacity-40">
            {busy ? '工作中' : session.status}
          </span>
        )}
        <span className={wsOpen ? 'opacity-40' : 'text-warning'}>
          {wsOpen ? '已连接' : '重连中'}
        </span>
        <button
          type="button"
          className={`btn btn-xs ml-auto ${autoMode ? 'btn-primary' : 'btn-ghost'}`}
          title={
            autoMode
              ? '自动批准常见工具权限'
              : '工具权限需你确认（协助单）'
          }
          onClick={toggleAuto}
        >
          Auto
        </button>
        <button
          type="button"
          className="btn btn-ghost btn-xs"
          onClick={() => setShowBrowser((v) => !v)}
        >
          {showBrowser ? '收起浏览器' : '打开画面'}
        </button>
      </div>

      {error && (
        <p className="px-4 pb-2 text-sm text-error" role="alert">
          {error}
        </p>
      )}

      <div
        className={
          showBrowser
            ? 'grid min-h-0 min-w-0 flex-1 gap-0 [grid-template-rows:minmax(0,1fr)_auto] lg:grid-cols-[minmax(0,1fr)_minmax(280px,40%)] lg:[grid-template-rows:none]'
            : 'flex min-h-0 min-w-0 flex-1 flex-col'
        }
      >
        <div className="chat-pane">
          <div className="chat-pane-scroll space-y-4 px-4 pt-3 text-sm">
            <div className="mx-auto w-full min-w-0 max-w-3xl space-y-4">
              {lines.length === 0 && !showWorking && (
                <p className="py-16 text-center text-sm opacity-40">
                  Waiting for the agent…
                </p>
              )}
              {groupChatLines(lines).map((block) => {
                if (block.kind === 'turn') {
                  const streaming = block.thoughts.some((t) => t.streaming)
                  const thoughtText = block.thoughts
                    .map((t) => t.text)
                    .filter(Boolean)
                    .join('\n\n')
                  return (
                    <div
                      key={block.id}
                      className="chat-turn-meta min-w-0 max-w-full sm:mr-8"
                    >
                      {block.thoughts.length > 0 && (
                        <ThoughtBlock
                          text={thoughtText}
                          streaming={streaming}
                          seconds={thoughtSeconds(block.thoughts, now)}
                        />
                      )}
                      {block.tools.length > 0 && (
                        <ToolCallGroup calls={block.tools} alwaysStats />
                      )}
                    </div>
                  )
                }
                const l = block.line
                if (l.kind === 'thought') {
                  return (
                    <div key={l.id} className="min-w-0 max-w-full sm:mr-8">
                      <ThoughtBlock
                        text={l.text}
                        streaming={l.streaming}
                        seconds={thoughtSeconds([l], now)}
                      />
                    </div>
                  )
                }
                if (l.kind === 'permission') {
                  if (l.outcome === 'requested' || l.outcome === 'auto') {
                    return null
                  }
                  return (
                    <p key={l.id} className="chat-perm">
                      {permLabel(l.outcome)} {l.title || l.text}
                      {l.optionId ? ` · ${l.optionId}` : ''}
                    </p>
                  )
                }
                const isUser = l.kind === 'user'
                const isQuiet = l.kind === 'system' || l.kind === 'event'
                return (
                  <div
                    key={l.id}
                    className={
                      isUser
                        ? 'ml-10 min-w-0 max-w-full rounded-2xl bg-primary/10 px-4 py-2.5'
                        : 'min-w-0 max-w-full sm:mr-8'
                    }
                  >
                    {isQuiet ? (
                      <span className="opacity-50">{l.text}</span>
                    ) : (
                      <div className="inline">
                        <MarkdownBody text={l.text} />
                        {l.kind === 'agent_message' && l.streaming && (
                          <span
                            className="chat-caret ml-0.5 inline-block"
                            aria-hidden
                          />
                        )}
                      </div>
                    )}
                  </div>
                )
              })}
              {showWorking && (
                <div
                  className="flex min-w-0 items-center gap-2 text-sm opacity-60 sm:mr-8"
                  aria-live="polite"
                >
                  <span className="loading loading-spinner loading-xs shrink-0" />
                  <span className="min-w-0 truncate">
                    {statusHint ?? '工作中'}
                    {session?.assistantId && (
                      <>
                        {' · '}
                        <Link
                          to={`/a/${session.assistantId}`}
                          className="link link-hover"
                        >
                          查看此刻
                        </Link>
                      </>
                    )}
                  </span>
                </div>
              )}
              <div ref={bottomRef} />
            </div>
          </div>

          <ChatComposerDock>
            {perm && (
              <div className="chat-composer-dock-inner mb-3 rounded-xl border border-warning/40 bg-warning/10 px-4 py-3">
                <p className="mb-1 text-sm font-medium">协助单</p>
                <p className="mb-1 text-sm">{perm.title}</p>
                <p className="mb-2 text-xs opacity-60">
                  为什么找你：工具需要你的确认才能继续。
                  {perm.ticketId ? ` · #${perm.ticketId.slice(0, 8)}` : ''}
                </p>
                <div className="flex flex-wrap gap-2">
                  {perm.options.map((o) => (
                    <button
                      key={o.optionId}
                      type="button"
                      className={permButtonClass(o.optionId)}
                      disabled={!wsOpen}
                      onClick={() => answerPerm(o.optionId)}
                    >
                      {o.name || o.optionId}
                    </button>
                  ))}
                  {showBrowser || browserSeen ? (
                    <button
                      type="button"
                      className="btn btn-sm btn-ghost"
                      onClick={() => setShowBrowser(true)}
                    >
                      打开协助画面
                    </button>
                  ) : null}
                </div>
              </div>
            )}
            <div className="chat-composer-dock-inner">
              <ChatComposer
                value={input}
                onChange={setInput}
                onSend={send}
                busy={busy}
                disabled={!wsOpen}
                lockInput={false}
                hint={
                  wsOpen
                    ? 'Enter to send · Shift+Enter for newline'
                    : 'Disconnected — reconnecting…'
                }
                placeholder={wsOpen ? 'Message the agent…' : 'Reconnecting…'}
                onCancel={() =>
                  wsRef.current?.send(JSON.stringify({ type: 'cancel' }))
                }
              />
            </div>
          </ChatComposerDock>
        </div>

        {showBrowser && id && (
          <div className="min-h-[240px] min-w-0 border-t border-base-300 lg:min-h-0 lg:border-t-0 lg:border-l">
            <AgentBrowserPanel
              sessionId={id}
              active={showBrowser}
              busy={busy || browserSeen}
              onClose={() => setShowBrowser(false)}
            />
          </div>
        )}
      </div>
    </div>
  )
}
