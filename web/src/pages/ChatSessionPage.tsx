import { useEffect, useRef, useState } from 'react'
import { Link, useLocation, useNavigate, useParams } from 'react-router-dom'
import {
  agents,
  type AgentMessage,
  type AgentSession,
  ApiError,
} from '../api'
import { AgentBrowserPanel } from '../components/AgentBrowserPanel'
import { ChatComposer } from '../components/ChatComposer'
import { MarkdownBody } from '../components/MarkdownBody'
import {
  ToolCallCard,
  type ToolCallData,
} from '../components/ToolCallCard'

type ChatLine =
  | {
      id: string
      kind: 'user' | 'assistant' | 'agent_message' | 'system' | 'event'
      text: string
      streaming?: boolean
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

function isBrowserTool(title: string): boolean {
  return title.startsWith('browser_')
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
        setLines(
          (hist.messages ?? []).map((m: AgentMessage) => ({
            id: m.id,
            kind:
              m.role === 'user'
                ? 'user'
                : m.role === 'assistant'
                  ? 'assistant'
                  : 'system',
            text: m.content,
          })),
        )
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
    const ws = new WebSocket(agents.sessionWsUrl(id))
    wsRef.current = ws
    ws.onopen = () => {
      if (disposed) return
      setError(null)
      setWsOpen(true)
    }
    ws.onmessage = (ev) => {
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
            return
          }
          if (e.type === 'tool_call' || e.type === 'tool_call_update') {
            const toolId = (e.toolId ?? '').trim() || `anon-${Date.now()}`
            const title = (e.title ?? '').trim()
            const status = (e.status ?? '').trim()
            if (isBrowserTool(title)) {
              setBrowserSeen(true)
              setShowBrowser(true)
            }
            if (
              e.type === 'tool_call_update' &&
              (status === 'completed' || status === 'failed')
            ) {
              setStatusHint('Working…')
            } else {
              setStatusHint(
                title ? `Running ${title}…` : 'Running tool…',
              )
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
          setStatusHint('Waiting for permission…')
          setPerm({
            requestId: msg.requestId ?? '',
            title: msg.title ?? 'Permission',
            options: msg.options ?? [],
          })
        } else if (msg.type === 'done') {
          setBusy(false)
          setStatusHint(null)
          setLines((prev) =>
            prev.map((l) =>
              l.kind === 'agent_message' && l.streaming
                ? { ...l, streaming: false }
                : l,
            ),
          )
        } else if (msg.type === 'error') {
          setBusy(false)
          setStatusHint(null)
          setLines((prev) =>
            prev.map((l) =>
              l.kind === 'agent_message' && l.streaming
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
    ws.onerror = () => {
      if (!disposed) setError('WebSocket error')
    }
    ws.onclose = (ev) => {
      if (disposed) return
      if (ev.code !== 1000 && ev.code !== 1001) {
        setError((prev) => prev ?? `WebSocket closed (${ev.code})`)
      }
    }
    return () => {
      disposed = true
      setWsOpen(false)
      wsRef.current = null
      if (
        ws.readyState === WebSocket.OPEN ||
        ws.readyState === WebSocket.CONNECTING
      ) {
        ws.close(1000, 'page dispose')
      }
    }
  }, [id])

  useEffect(() => {
    bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
  }, [lines, perm, statusHint, busy])

  const sendPrompt = (ws: WebSocket, text: string) => {
    setInput('')
    setBusy(true)
    setStatusHint('Working…')
    setError(null)
    setLines((prev) => [
      ...prev.map((l) =>
        l.kind === 'agent_message' && l.streaming
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
    setPerm(null)
    setStatusHint('Working…')
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
        {session?.sandboxId && (
          <Link to={`/s/${session.sandboxId}`} className="link link-hover opacity-70">
            Open workbench
          </Link>
        )}
        {session && (
          <span className="opacity-40">
            {session.providerId} · {session.status}
          </span>
        )}
        <button
          type="button"
          className="btn btn-ghost btn-xs ml-auto"
          onClick={() => setShowBrowser((v) => !v)}
        >
          {showBrowser ? 'Hide browser' : 'Show browser'}
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
        <div className="flex min-h-0 min-w-0 flex-col overflow-hidden">
          <div className="min-h-0 min-w-0 flex-1 space-y-4 overflow-x-hidden overflow-y-auto px-4 py-3 text-sm">
            <div className="mx-auto w-full min-w-0 max-w-3xl space-y-4">
              {lines.length === 0 && !showWorking && (
                <p className="py-16 text-center text-sm opacity-40">
                  Waiting for the agent…
                </p>
              )}
              {lines.map((l) => {
                if (l.kind === 'tool_call') {
                  return (
                    <div key={l.id} className="min-w-0 max-w-full sm:mr-8">
                      <ToolCallCard call={l.tool} />
                    </div>
                  )
                }
                const isUser = l.kind === 'user'
                const isSystem = l.kind === 'system' || l.kind === 'event'
                const useMarkdown = !isSystem
                return (
                  <div
                    key={l.id}
                    className={
                      isUser
                        ? 'ml-10 min-w-0 max-w-full rounded-2xl bg-primary/10 px-4 py-2.5'
                        : 'min-w-0 max-w-full sm:mr-8'
                    }
                  >
                    {useMarkdown ? (
                      <div className="inline">
                        <MarkdownBody text={l.text} />
                        {l.kind === 'agent_message' && l.streaming && (
                          <span
                            className="chat-caret ml-0.5 inline-block"
                            aria-hidden
                          />
                        )}
                      </div>
                    ) : (
                      <span className="opacity-50">{l.text}</span>
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
                  <span className="min-w-0 truncate">{statusHint ?? 'Working…'}</span>
                </div>
              )}
              <div ref={bottomRef} />
            </div>
          </div>

          {perm && (
            <div className="border-t border-base-300 bg-warning/10 px-4 py-3">
              <div className="mx-auto w-full max-w-3xl">
                <p className="mb-1 text-sm font-medium">
                  Allow tool: {perm.title}
                </p>
                <p className="mb-2 text-xs opacity-60">
                  Prefer session allow so sandboxed agents ask less often.
                </p>
                <div className="flex flex-wrap gap-2">
                  {perm.options.map((o) => (
                    <button
                      key={o.optionId}
                      type="button"
                      className={permButtonClass(o.optionId)}
                      onClick={() => answerPerm(o.optionId)}
                    >
                      {o.name || o.optionId}
                    </button>
                  ))}
                </div>
              </div>
            </div>
          )}

          <div className="min-w-0 px-4 pb-[max(1rem,env(safe-area-inset-bottom))] pt-2">
            <div className="mx-auto w-full min-w-0 max-w-3xl">
              <ChatComposer
                value={input}
                onChange={setInput}
                onSend={send}
                busy={busy}
                onCancel={() =>
                  wsRef.current?.send(JSON.stringify({ type: 'cancel' }))
                }
              />
            </div>
          </div>
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
