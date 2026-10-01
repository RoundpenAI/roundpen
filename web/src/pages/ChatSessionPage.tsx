import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
} from 'react'
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
import { IconArrowUp } from '@douyinfe/semi-icons'
import type { MessageContent } from '@douyinfe/semi-ui-19/lib/es/aiChatInput/interface'
import {
  agents,
  assistantsApi,
  type AgentCommand,
  type AgentMessage,
  type AgentSession,
  ApiError,
} from '../api'
import { AgentBrowserPanel } from '../components/AgentBrowserPanel'
import { useAssistantLayout } from '../components/AssistantLayout'
import { SessionTabs } from '../components/SessionTabs'
import { chatDialogueRenderConfig } from '../components/chatDialogueRender'
import {
  agentMessagesToSemi,
  type SemiChatMessage,
} from '../lib/semiChatAdapter'
import {
  wsCanSendProp,
  wsInputPlaceholder,
  wsStatusLabel,
  type WsUiStatus,
} from '../lib/sessionWsUi'
import { enqueueOutbox, outboxWaitingHint } from '../lib/sessionOutbox'
import {
  buildSendPayload,
  contentsHaveSendableText,
  type SendContent,
  type SendPayload,
} from '../lib/slashCommand'
import {
  clearPendingPrompt,
  clearStreaming,
  isEphemeralMessageId,
  isStreamingMessage,
  MESSAGE_PAGE_SIZE,
  mergeHistoryWithLive,
  nowIso,
  readAutoMode,
  readPendingPrompt,
  ROLE_CONFIG,
  sentPending,
  shouldShowInDialogue,
  stashPendingPrompt,
  toSkillItem,
  type PermReq,
} from './chat/sessionChatHelpers'
import {
  newClientMsgId,
  queueBadge,
  queueHint,
  queueItemFor,
  type QueueItem,
} from './chat/sessionQueue'
import { RoutineSuggestions } from './chat/RoutineSuggestions'
import { clearComposer, composerEditorBridge } from './chat/editorBridge'
import { connectChatSocket } from './chat/sessionSocket'

export function ChatSessionPage() {
  const { id = '', assistantId: assistantIdParam } = useParams<{
    id?: string
    assistantId?: string
  }>()
  const location = useLocation()
  const navigate = useNavigate()
  // Answering drops the ticket from the assistant sidebar's todo list.
  const { refresh: refreshAssistantLayout } = useAssistantLayout()
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
  // Follow new output only while the viewport is already near the bottom.
  // If the user scrolls up to read history, stop fighting their scroll.
  const stickToBottomRef = useRef(true)
  const [histReady, setHistReady] = useState(false)
  const [hasMoreHistory, setHasMoreHistory] = useState(false)
  const [loadingMore, setLoadingMore] = useState(false)
  const pendingScrollRestore = useRef<{ height: number; top: number } | null>(
    null,
  )
  const loadingMoreRef = useRef(false)
  const hasMoreRef = useRef(false)
  const [wsStatus, setWsStatus] = useState<WsUiStatus>('connecting')
  const [wsDetail, setWsDetail] = useState<string | null>(null)
  const wsOpen = wsStatus === 'open'
  const [autoMode, setAutoMode] = useState(readAutoMode)
  const [composerFocused, setComposerFocused] = useState(false)
  const [composerHasText, setComposerHasText] = useState(false)
  const [commands, setCommands] = useState<AgentCommand[]>([])
  const [queueItems, setQueueItems] = useState<QueueItem[]>([])
  const [cancelledIds, setCancelledIds] = useState<Set<string>>(new Set())
  // null until the server reports a capability bit: unknown steers optimistically
  // and lets the server fall back to queueing.
  const [steerCap, setSteerCap] = useState<boolean | null>(null)
  const composerContentsRef = useRef<SendContent[] | undefined>(undefined)
  const composerExtensions = useMemo(() => [composerEditorBridge()], [])
  const composerRef = useRef<{
    setContent: (content: string) => void
    focusEditor?: (pos?: string) => void
  } | null>(null)
  const fillSuggestion = (text: string) => {
    const safe = text
      .replaceAll('&', '&amp;')
      .replaceAll('<', '&lt;')
      .replaceAll('>', '&gt;')
    composerRef.current?.setContent(`<p>${safe}</p>`)
    composerRef.current?.focusEditor?.('end')
  }
  const busyRef = useRef(false)
  const messagesRef = useRef(messages)
  useEffect(() => {
    busyRef.current = busy
  }, [busy])
  useEffect(() => {
    messagesRef.current = messages
  }, [messages])
  const composerIdle = !composerHasText && !busy
  const showComposerSend = busy || composerHasText

  const chats: SemiChatMessage[] = useMemo(
    () => agentMessagesToSemi(messages.filter(shouldShowInDialogue)),
    [messages],
  )

  const skillItems = useMemo(() => commands.map(toSkillItem), [commands])

  // Catalog drives the "/" menu; a failure just means the composer behaves as
  // a plain prompt box.
  const refreshCommands = useCallback(() => {
    if (!id) return
    agents
      .commands(id)
      .then((res) => setCommands(res.commands ?? []))
      .catch(() => undefined)
  }, [id])

  useEffect(() => {
    refreshCommands()
  }, [refreshCommands])

  useEffect(() => {
    const incoming =
      (location.state as { pendingPrompt?: string } | null)?.pendingPrompt ?? ''
    if (!incoming) return
    stashPendingPrompt(id, incoming)
    navigate(location.pathname, { replace: true, state: {} })
  }, [id, location.pathname, location.state, navigate])

  useEffect(() => {
    loadingMoreRef.current = loadingMore
  }, [loadingMore])
  useEffect(() => {
    hasMoreRef.current = hasMoreHistory
  }, [hasMoreHistory])

  useEffect(() => {
    let cancelled = false
    setHistReady(false)
    setHasMoreHistory(false)
    ;(async () => {
      try {
        const [sess, hist] = await Promise.all([
          agents.getSession(id),
          agents.messages(id, { limit: MESSAGE_PAGE_SIZE }),
        ])
        if (cancelled) return
        setSession(sess)
        // History is the persisted base. If the WS status snapshot already
        // planted a live streaming bubble (refresh mid-turn), keep it on top
        // instead of skipping history entirely — that left only a half reply.
        setMessages((prev) =>
          mergeHistoryWithLive(hist.messages ?? [], prev, id),
        )
        setHasMoreHistory(Boolean(hist.hasMore))
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
    setQueueItems([])
    setCancelledIds(new Set())
    setSteerCap(null)
  }, [id])

  const handleUnqueue = useCallback(
    (messageId: string) => {
      const ws = wsRef.current
      if (!ws || ws.readyState !== WebSocket.OPEN) return
      // Optimistic bubbles carry the client id; the server knows the row id.
      const target = queueItemFor(queueItems, messageId)?.id ?? messageId
      ws.send(JSON.stringify({ type: 'unqueue', id: target }))
      // Optimistic: a queue frame (or a refusal error) reconciles it. A refused
      // pull-back keeps the message queued, and queued badges win over cancelled.
      setCancelledIds((prev) => new Set(prev).add(messageId))
    },
    [queueItems],
  )

  const applyQueue = useCallback((items: QueueItem[]) => {
    setQueueItems(items)
    // Items that reappear in the queue (refused pull-back, re-queued) drop
    // their cancelled mark.
    setCancelledIds((prev) => {
      if (prev.size === 0) return prev
      let changed = false
      const next = new Set(prev)
      for (const it of items) {
        if (next.delete(it.id)) changed = true
        if (it.clientMsgId && next.delete(it.clientMsgId)) changed = true
      }
      return changed ? next : prev
    })
  }, [])

  useEffect(
    () =>
      connectChatSocket({
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
        onQueue: applyQueue,
        setSteerCap,
        refreshCommands,
      }),
    [id, refreshCommands, applyQueue],
  )

  useEffect(() => {
    initialScrollDone.current = false
    stickToBottomRef.current = true
    pendingScrollRestore.current = null
  }, [id])

  useLayoutEffect(() => {
    const pending = pendingScrollRestore.current
    const scroller = scrollRef.current
    if (!pending || !scroller) return
    pendingScrollRestore.current = null
    scroller.scrollTop = pending.top + (scroller.scrollHeight - pending.height)
  }, [messages])

  useLayoutEffect(() => {
    if (!histReady) return
    const scroller = scrollRef.current
    const content = contentRef.current
    if (!scroller || !content) return

    const pinBottom = (force = false) => {
      if (!force && initialScrollDone.current && !stickToBottomRef.current) {
        return
      }
      scroller.scrollTop = scroller.scrollHeight
    }

    // Live updates after the first pin: jump only while still stuck to bottom.
    if (initialScrollDone.current) {
      pinBottom()
      return
    }

    // First paint after history: keep pinning while Markdown/layout grows,
    // otherwise refresh lands mid last-message.
    pinBottom(true)
    let settleTimer = 0
    const ro = new ResizeObserver(() => {
      pinBottom(true)
      window.clearTimeout(settleTimer)
      settleTimer = window.setTimeout(() => {
        pinBottom(true)
        initialScrollDone.current = true
        ro.disconnect()
      }, 120)
    })
    ro.observe(content)
    const maxWait = window.setTimeout(() => {
      pinBottom(true)
      initialScrollDone.current = true
      ro.disconnect()
    }, 2000)
    return () => {
      window.clearTimeout(settleTimer)
      window.clearTimeout(maxWait)
      ro.disconnect()
    }
  }, [histReady, chats, perm, statusHint, busy])

  const loadOlderMessages = useCallback(async () => {
    if (!id || loadingMoreRef.current || !hasMoreRef.current) return
    const oldest = messagesRef.current.find((m) => !isEphemeralMessageId(m.id))
    if (!oldest) return
    const scroller = scrollRef.current
    if (scroller) {
      pendingScrollRestore.current = {
        height: scroller.scrollHeight,
        top: scroller.scrollTop,
      }
    }
    loadingMoreRef.current = true
    setLoadingMore(true)
    try {
      const res = await agents.messages(id, {
        limit: MESSAGE_PAGE_SIZE,
        before: oldest.id,
      })
      const older = res.messages ?? []
      setMessages((prev) => {
        const seen = new Set(prev.map((m) => m.id))
        return [...older.filter((m) => !seen.has(m.id)), ...prev]
      })
      setHasMoreHistory(Boolean(res.hasMore))
    } catch {
      pendingScrollRestore.current = null
    } finally {
      loadingMoreRef.current = false
      setLoadingMore(false)
    }
  }, [id])

  const onScrollPane = () => {
    const el = scrollRef.current
    if (!el) return
    const gap = el.scrollHeight - el.scrollTop - el.clientHeight
    stickToBottomRef.current = gap <= 80
    if (el.scrollTop < 72) {
      void loadOlderMessages()
    }
  }
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

  const appendLocalUser = (text: string, clientMsgId?: string) => {
    stickToBottomRef.current = true
    setMessages((prev) => [
      ...clearStreaming(prev),
      {
        id: clientMsgId ?? `${Date.now()}-u`,
        sessionId: id,
        role: 'user',
        content: text,
        createdAt: nowIso(),
      },
    ])
  }

  const sendPrompt = (ws: WebSocket, text: string, clientMsgId: string) => {
    setBusy(true)
    setStatusHint('Working…')
    setError(null)
    appendLocalUser(text, clientMsgId)
    ws.send(JSON.stringify({ type: 'prompt', text, clientMsgId }))
  }

  const sendCommand = (ws: WebSocket, name: string, args: string) => {
    setBusy(true)
    setStatusHint('Working…')
    setError(null)
    appendLocalUser(`/${name}${args ? ` ${args}` : ''}`)
    ws.send(JSON.stringify({ type: 'command', name, args }))
  }

  // Route one composer payload: commands keep their own frames; text goes out
  // as steer, as queue, or as a plain turn when idle. Offline text lands in
  // the outbox and is delivered on reconnect.
  const dispatchOutgoing = (out: SendPayload, mode: 'steer' | 'queue') => {
    const ws = wsRef.current
    const open = ws && ws.readyState === WebSocket.OPEN
    if (out.kind === 'command') {
      // Commands are never queued offline: /clear acts on the live runner,
      // which only makes sense against an open socket.
      if (open) {
        sendCommand(ws, out.name, out.args)
      } else {
        setError('连接断开，命令未发送：请等待重连后再试')
      }
      return
    }
    const clientMsgId = newClientMsgId()
    if (!open) {
      // WeChat-style: show the bubble immediately; deliver when WS is ready.
      appendLocalUser(out.text, clientMsgId)
      outboxRef.current = enqueueOutbox(outboxRef.current, out.text)
      setStatusHint(outboxWaitingHint(outboxRef.current.length))
      setError(null)
      return
    }
    if (busy) {
      appendLocalUser(out.text, clientMsgId)
      if (mode === 'steer' && steerCap !== false) {
        // The server falls back to the queue when the provider cannot steer;
        // the queue frame then carries the badge.
        ws.send(JSON.stringify({ type: 'steer', text: out.text, clientMsgId }))
        return
      }
      ws.send(JSON.stringify({ type: 'prompt', text: out.text, clientMsgId }))
      setStatusHint(mode === 'steer' ? '该 agent 不支持插话，已排队' : '已排队')
      return
    }
    sendPrompt(ws, out.text, clientMsgId)
  }

  /** Send whatever is in the composer; the busy-state key routes here. */
  const composerSend = (mode: 'steer' | 'queue') => {
    const out = buildSendPayload(composerContentsRef.current, commands)
    if (!out) return
    clearComposer()
    setComposerHasText(false)
    setComposerFocused(false)
    dispatchOutgoing(out, mode)
  }

  /** Semi's own send key/Enter: the payload is already read from the editor. */
  const handleMessageSend = (payload: MessageContent) => {
    const out = buildSendPayload(payload.inputContents, commands)
    if (!out) return
    // clearContentOnGenerating is off (a mid-turn send must not wipe text typed
    // after it), so the hand-off clears the composer right here — otherwise the
    // sent text stays in the box while the session works on it.
    clearComposer()
    setComposerHasText(false)
    setComposerFocused(false)
    dispatchOutgoing(out, 'queue')
  }

  const dialogueRender = useMemo(
    () =>
      chatDialogueRenderConfig({
        badgeFor: (messageId) => queueBadge(queueItems, cancelledIds, messageId),
        onUnqueue: handleUnqueue,
      }),
    [queueItems, cancelledIds, handleUnqueue],
  )

  useEffect(() => {
    const pending = readPendingPrompt(id).trim()
    const ws = wsRef.current
    if (!pending || sentPending.has(id) || !histReady || busy) {
      return
    }
    sentPending.add(id)
    clearPendingPrompt(id)
    if (ws && ws.readyState === WebSocket.OPEN) {
      sendPrompt(ws, pending, newClientMsgId())
      return
    }
    appendLocalUser(pending, newClientMsgId())
    outboxRef.current = enqueueOutbox(outboxRef.current, pending)
    setStatusHint(outboxWaitingHint(outboxRef.current.length))
  }, [histReady, wsOpen, id, busy])

  const answerPerm = (optionId: string) => {
    if (!perm || !wsRef.current) return
    const chosen = perm.options.find((o) => o.optionId === optionId)
    const label = (chosen?.name || '').trim() || optionId
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
        .resolveTicket(perm.ticketId, { resolution, note: label })
        .then(() => refreshAssistantLayout())
        .catch(() => undefined)
    }
    setMessages((prev) => [
      ...prev,
      {
        id: `perm-${Date.now()}`,
        sessionId: id,
        role: 'permission',
        content: label,
        meta: {
          type: 'permission',
          title: perm.title,
          optionId,
          outcome: 'selected',
          options: perm.options,
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
      <SessionTabs
        assistantId={assistantIdParam || session?.assistantId || ''}
        currentId={id}
      />
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
              ? '自动模式：分类器放行安全操作，拦截危险操作'
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
            onScroll={onScrollPane}
          >
            <div
              ref={contentRef}
              style={{ maxWidth: 768, margin: '0 auto', minWidth: 0 }}
            >
              {loadingMore && (
                <div
                  style={{
                    display: 'flex',
                    justifyContent: 'center',
                    padding: '8px 0 12px',
                  }}
                >
                  <Spin size="small" />
                </div>
              )}
              {chats.length === 0 && !showWorking && (
                <Typography.Text
                  type="tertiary"
                  style={{ display: 'block', textAlign: 'center', padding: 64 }}
                >
                  打个招呼，开启对话…
                </Typography.Text>
              )}
              {chats.length > 0 && (
                <AIChatDialogue
                  align="leftRight"
                  mode="bubble"
                  chats={chats}
                  roleConfig={ROLE_CONFIG}
                  dialogueRenderConfig={dialogueRender}
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
                    {queueHint(queueItems.length) ?? statusHint ?? '工作中'}
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
              {chats.length === 0 && !composerHasText && !busy && (
                <RoutineSuggestions onPick={fillSuggestion} />
              )}
              <AIChatInput
                ref={composerRef}
                keepSkillAfterSend={false}
                generating={busy}
                canSend={wsCanSendProp(wsStatus) && composerHasText}
                showUploadButton={false}
                showUploadFile={false}
                showReference={false}
                round
                extensions={composerExtensions}
                clearContentOnGenerating={false}
                placeholder={wsInputPlaceholder(wsStatus)}
                renderConfigureArea={() => null}
                renderActionArea={({ menuItem, className }) => {
                  if (!showComposerSend) return null
                  // The last item is Semi's send key; while generating it
                  // renders as stop. The custom key next to it sends mid-turn.
                  const sendBtn = menuItem[menuItem.length - 1]
                  if (!busy) return <div className={className}>{sendBtn}</div>
                  return (
                    <div className={className}>
                      {sendBtn}
                      <button
                        type="button"
                        className="chat-midturn-send"
                        disabled={!composerHasText}
                        title="点击：插话到当前任务（该 agent 不支持时自动排队）；Shift+点击：仅排队"
                        onClick={(e) => composerSend(e.shiftKey ? 'queue' : 'steer')}
                      >
                        <IconArrowUp size="small" />
                      </button>
                    </div>
                  )
                }}
                skills={skillItems}
                skillHotKey="/"
                renderSkillItem={({ skill, className, onClick, onMouseEnter }) => (
                  <div
                    className={className}
                    onClick={onClick}
                    onMouseEnter={onMouseEnter}
                    role="button"
                    tabIndex={-1}
                  >
                    <span className="chat-skill-name">/{skill.label ?? skill.value}</span>
                    <span className="chat-skill-desc">{skill.description ?? ''}</span>
                    {skill.source === 'installed' && (
                      <span className="chat-skill-tag">已安装</span>
                    )}
                  </div>
                )}
                onFocus={() => {
                  setComposerFocused(true)
                  stickToBottomRef.current = true
                  // After the keyboard animation, visualViewport shrinks; pin
                  // the thread to the bottom so the composer sits on the keyboard.
                  const pin = () => {
                    const el = scrollRef.current
                    if (el) el.scrollTop = el.scrollHeight
                  }
                  requestAnimationFrame(pin)
                  window.setTimeout(pin, 50)
                  window.setTimeout(pin, 300)
                }}
                onBlur={() => setComposerFocused(false)}
                onContentChange={(contents) => {
                  composerContentsRef.current = contents as unknown as SendContent[]
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
