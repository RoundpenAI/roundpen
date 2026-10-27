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
import { IconArrowUp, IconMenu, IconMore, IconUser } from '@douyinfe/semi-icons'
import type { MessageContent } from '@douyinfe/semi-ui-19/lib/es/aiChatInput/interface'
import {
  agents,
  assistantsApi,
  projects,
  setupApi,
  type AgentCommand,
  type AgentMessage,
  type AgentSession,
  type LlmReady,
  type ProjectWorker,
  ApiError,
} from '../api'
import { AgentBrowserPanel } from '../components/AgentBrowserPanel'
import { useOpenAppMenu } from '../components/AppShell'
import { useAssistantLayout } from '../components/AssistantLayout'
import { CoordinatorWorkersSheet } from '../components/CoordinatorWorkersSheet'
import { SessionTabs } from '../components/SessionTabs'
import { chatDialogueRenderConfig } from '../components/chatDialogueRender'
import { DeliveryActionBar } from '../components/DeliveryActionBar'
import { inferDeliveryNeed } from '../lib/deliveryNeed'
import {
  loadProjectChatContext,
  type ProjectChatContext,
} from '../lib/projectChatContext'
import { LlmSetupChooser } from '../components/LlmSetupChooser'
import { useAuth } from '../auth'
import { useT } from '../i18n'
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
  readPendingPrompt,
  readPermMode,
  writePermMode,
  type PermMode,
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
import { syncVisualViewportInsets } from '../lib/visualViewport'
import { clearComposer, composerEditorBridge } from './chat/editorBridge'
import { connectChatSocket } from './chat/sessionSocket'
import {
  STREAM_OPEN,
  type ChatStreamHandle,
} from '../lib/sessionStreamConnect'

function looksLikeLLMNotConfigured(msg: string | null | undefined): boolean {
  if (!msg) return false
  const m = msg.toLowerCase()
  return (
    msg.includes('LLM 尚未配置') ||
    msg.includes('LLM 网关未启用') ||
    msg.includes('尚未配置上游') ||
    m.includes('unsupported protocol scheme') ||
    m.includes('llm gateway')
  )
}

/** FREE cloud entry (or any upstream) hit rate limit / quota — cue BYOK. */
function looksLikeFreeQuotaExceeded(msg: string | null | undefined): boolean {
  if (!msg) return false
  const m = msg.toLowerCase()
  return (
    msg.includes('免费额度') ||
    msg.includes('额度已用尽') ||
    m.includes('too many requests') ||
    m.includes('rate limit') ||
    m.includes('quota') ||
    /\b429\b/.test(m) ||
    m.includes('llm http 429')
  )
}

export function ChatSessionPage() {
  const { id = '', assistantId: assistantIdParam } = useParams<{
    id?: string
    assistantId?: string
  }>()
  const location = useLocation()
  const navigate = useNavigate()
  // Answering drops the ticket from the assistant sidebar's todo list.
  const {
    assistants,
    refresh: refreshAssistantLayout,
    openAssistantList,
  } = useAssistantLayout()
  const openAppMenu = useOpenAppMenu()
  const t = useT()
  const auth = useAuth()
  const isAdmin = auth.status === 'ok' && auth.user.role === 'admin'
  const [session, setSession] = useState<AgentSession | null>(null)
  const [messages, setMessages] = useState<AgentMessage[]>([])
  const [busy, setBusy] = useState(false)
  const [statusHint, setStatusHint] = useState<string | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [llmStatus, setLlmStatus] = useState<LlmReady | null>(null)
  const [perm, setPerm] = useState<PermReq | null>(null)
  const [showBrowser, setShowBrowser] = useState(false)
  const [browserSeen, setBrowserSeen] = useState(false)
  const [moreOpen, setMoreOpen] = useState(false)
  const moreRef = useRef<HTMLDivElement>(null)
  const [projectCtx, setProjectCtx] = useState<ProjectChatContext | null>(null)
  const [activeWorker, setActiveWorker] = useState<ProjectWorker | undefined>()
  useEffect(() => {
    if (!moreOpen) return
    const onDoc = (e: MouseEvent) => {
      if (!moreRef.current?.contains(e.target as Node)) setMoreOpen(false)
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setMoreOpen(false)
    }
    document.addEventListener('mousedown', onDoc)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDoc)
      document.removeEventListener('keydown', onKey)
    }
  }, [moreOpen])

  const chatAssistantId = assistantIdParam || session?.assistantId || ''
  const chatAssistant =
    assistants.find((a) => a.id === chatAssistantId) ?? null
  useEffect(() => {
    let cancelled = false
    setProjectCtx(null)
    setActiveWorker(undefined)
    if (!chatAssistantId) return
    void loadProjectChatContext(chatAssistantId, id).then((ctx) => {
      if (!cancelled) setProjectCtx(ctx)
    })
    return () => {
      cancelled = true
    }
  }, [chatAssistantId, id])
  const onActiveWorkerChange = useCallback((w: ProjectWorker | undefined) => {
    setActiveWorker(w)
  }, [])
  const wsRef = useRef<ChatStreamHandle | null>(null)

  const sendAction = (body: Record<string, unknown>) => {
    if (!id) return
    void agents.sessionAction(id, body).catch((err: unknown) => {
      const msg = err instanceof Error ? err.message : '发送失败'
      setError(msg)
    })
  }
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
  const [permMode, setPermMode] = useState<PermMode>(readPermMode)
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
  const composerRef = useRef<AIChatInput | null>(null)
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
  const workerBusy = Boolean(activeWorker) && !projectCtx?.worker
  const generating = busy || workerBusy
  const composerIdle = !composerHasText && !generating
  const showComposerSend = generating || composerHasText

  const handleStopGenerate = useCallback(() => {
    if (busy) {
      sendAction({ type: 'cancel' })
      // Worker live view: also mark WRK cancelled so the queue advances.
      const self = projectCtx?.worker
      if (self && projectCtx) {
        void projects.cancelWorker(projectCtx.project.key, self.key).catch(() => undefined)
      }
      return
    }
    if (!projectCtx || !activeWorker) return
    void projects
      .cancelWorker(projectCtx.project.key, activeWorker.key)
      .then((res) => {
        setActiveWorker(
          res.worker.status === 'running' || res.worker.status === 'queued'
            ? res.worker
            : undefined,
        )
        setStatusHint(null)
      })
      .catch((err: unknown) => {
        setError(err instanceof Error ? err.message : String(err))
      })
  }, [busy, projectCtx, activeWorker, id])

  const chats: SemiChatMessage[] = useMemo(
    () => agentMessagesToSemi(messages.filter(shouldShowInDialogue)),
    [messages],
  )

  const deliveryNeed = useMemo(() => inferDeliveryNeed(messages), [messages])

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
      if (!wsRef.current || wsRef.current.readyState !== STREAM_OPEN) return
      // Optimistic bubbles carry the client id; the server knows the row id.
      const target = queueItemFor(queueItems, messageId)?.id ?? messageId
      sendAction({ type: 'unqueue', id: target })
      // Optimistic: a queue frame (or a refusal error) reconciles it. A refused
      // pull-back keeps the message queued, and queued badges win over cancelled.
      setCancelledIds((prev) => new Set(prev).add(messageId))
    },
    [queueItems, id],
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
        setShowBrowser,
        onQueue: applyQueue,
        setSteerCap,
        refreshCommands,
      }),
    [id, refreshCommands, applyQueue],
  )

  useEffect(() => {
    let alive = true
    setupApi
      .llmReady()
      .then((res) => {
        if (!alive) return
        setLlmStatus(res)
      })
      .catch(() => {
        /* leave alone; send-time errors still surface */
      })
    return () => {
      alive = false
    }
  }, [id])

  const showLlmGuide =
    (llmStatus != null && !llmStatus.ready) ||
    looksLikeLLMNotConfigured(error) ||
    looksLikeFreeQuotaExceeded(error)

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
  const choosePermMode = (next: PermMode) => {
    setPermMode(next)
    writePermMode(next)
    if (wsRef.current && wsRef.current.readyState === STREAM_OPEN) {
      sendAction({ type: 'auto', mode: next })
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

  const sendPrompt = (text: string, clientMsgId: string) => {
    setBusy(true)
    setStatusHint('Working…')
    setError(null)
    appendLocalUser(text, clientMsgId)
    sendAction({ type: 'prompt', text, clientMsgId })
  }

  const sendCommand = (name: string, args: string) => {
    setBusy(true)
    setStatusHint('Working…')
    setError(null)
    appendLocalUser(`/${name}${args ? ` ${args}` : ''}`)
    sendAction({ type: 'command', name, args })
  }

  // Route one composer payload: commands keep their own frames; text goes out
  // as steer, as queue, or as a plain turn when idle. Offline text lands in
  // the outbox and is delivered on reconnect.
  const dispatchOutgoing = (out: SendPayload, mode: 'steer' | 'queue') => {
    const open = wsRef.current && wsRef.current.readyState === STREAM_OPEN
    if (out.kind === 'command') {
      // Commands are never queued offline: /clear acts on the live runner,
      // which only makes sense against an open stream.
      if (open) {
        sendCommand(out.name, out.args)
      } else {
        setError('连接断开，命令未发送：请等待重连后再试')
      }
      return
    }
    const clientMsgId = newClientMsgId()
    if (!open) {
      // WeChat-style: show the bubble immediately; deliver when the stream is ready.
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
        sendAction({ type: 'steer', text: out.text, clientMsgId })
        return
      }
      sendAction({ type: 'prompt', text: out.text, clientMsgId })
      setStatusHint(mode === 'steer' ? '该 agent 不支持插话，已排队' : '已排队')
      return
    }
    sendPrompt(out.text, clientMsgId)
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
    if (!pending || sentPending.has(id) || !histReady || busy) {
      return
    }
    sentPending.add(id)
    clearPendingPrompt(id)
    if (wsRef.current && wsRef.current.readyState === STREAM_OPEN) {
      sendPrompt(pending, newClientMsgId())
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
    sendAction({
      type: 'permission',
      requestId: perm.requestId,
      optionId,
    })
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
        leading={
          <>
            <Button
              theme="borderless"
              type="tertiary"
              size="small"
              icon={<IconMenu />}
              aria-label="打开菜单"
              className="rp-shell-menu-mobile"
              onClick={openAppMenu}
            />
            <Button
              theme="borderless"
              type="tertiary"
              size="small"
              icon={<IconUser />}
              aria-label="打开助手列表"
              className="rp-assistant-menu-mobile"
              onClick={openAssistantList}
            />
            {chatAssistantId &&
              (projectCtx ? (
                <Link
                  to={`/projects/${projectCtx.project.key}`}
                  className="rp-chat-assistant-name is-project"
                  title={projectCtx.title}
                  aria-label={t('projects.openFromChat')}
                >
                  {projectCtx.label}
                </Link>
              ) : (
                <button
                  type="button"
                  className="rp-chat-assistant-name"
                  title={
                    assistants.find((a) => a.id === chatAssistantId)?.name
                  }
                  aria-label="切换助手"
                  onClick={openAssistantList}
                >
                  {assistants.find((a) => a.id === chatAssistantId)?.name ||
                    '助手'}
                </button>
              ))}
          </>
        }
        trailing={
          <div className="rp-session-tabs-tools" ref={moreRef}>
            {projectCtx && id ? (
              <CoordinatorWorkersSheet
                project={projectCtx.project}
                sessionId={id}
                isWorkerSession={Boolean(projectCtx.worker)}
                onActiveChange={onActiveWorkerChange}
              />
            ) : null}
            <Button
              theme="borderless"
              type="tertiary"
              size="small"
              icon={<IconMore />}
              aria-label={t('chat.perm.label')}
              aria-expanded={moreOpen}
              onClick={() => setMoreOpen((v) => !v)}
            />
            {moreOpen && (
              <div className="rp-more-menu" role="menu">
                <Typography.Text
                  type="tertiary"
                  size="small"
                  className="rp-more-caption"
                >
                  {t('chat.perm.label')}
                </Typography.Text>
                {(['edit', 'decide', 'all'] as const).map((mode) => (
                  <button
                    key={mode}
                    type="button"
                    role="menuitemradio"
                    aria-checked={permMode === mode}
                    className={
                      permMode === mode
                        ? 'rp-more-item is-active'
                        : 'rp-more-item'
                    }
                    onClick={() => {
                      choosePermMode(mode)
                      setMoreOpen(false)
                    }}
                  >
                    <span>
                      {mode === 'edit'
                        ? t('chat.perm.edit')
                        : mode === 'all'
                          ? t('chat.perm.all')
                          : t('chat.perm.decide')}
                    </span>
                    <Typography.Text type="tertiary" size="small">
                      {mode === 'edit'
                        ? t('chat.perm.editHint')
                        : mode === 'all'
                          ? t('chat.perm.allHint')
                          : t('chat.perm.decideHint')}
                    </Typography.Text>
                  </button>
                ))}
                {session?.assistantId && (
                  <Link
                    to={`/a/${session.assistantId}`}
                    className="rp-more-item"
                    onClick={() => setMoreOpen(false)}
                  >
                    助手详情
                  </Link>
                )}
                <Typography.Text
                  type={wsStatus === 'open' ? 'tertiary' : 'danger'}
                  size="small"
                  className="rp-more-caption"
                  title={wsDetail ?? undefined}
                >
                  {session ? (busy ? '工作中' : session.status) : ''}
                  {session ? ' · ' : ''}
                  {wsStatusLabel(wsStatus, wsDetail)}
                </Typography.Text>
                {browserSeen && !showBrowser && (
                  <button
                    type="button"
                    className="rp-more-item"
                    onClick={() => {
                      setShowBrowser(true)
                      setMoreOpen(false)
                    }}
                  >
                    打开画面
                  </button>
                )}
              </div>
            )}
          </div>
        }
      />
      {showLlmGuide && (
        <div role="status" style={{ padding: '0 16px 8px' }}>
          <LlmSetupChooser
            compact
            status={
              llmStatus != null
                ? {
                    ...llmStatus,
                    reason:
                      (looksLikeFreeQuotaExceeded(error) ||
                      looksLikeLLMNotConfigured(error)
                        ? error
                        : null) ||
                      llmStatus.reason ||
                      undefined,
                  }
                : {
                    ready: false,
                    mode: 'none',
                    freeAvailable: true,
                    reason:
                      (looksLikeLLMNotConfigured(error) ||
                      looksLikeFreeQuotaExceeded(error)
                        ? error
                        : null) || '尚未配置模型',
                  }
            }
            isAdmin={isAdmin}
            onReady={(res) => {
              setLlmStatus(res)
              // Clearing only helps cold-start misconfig; FREE quota needs BYOK.
              if (res.ready && looksLikeLLMNotConfigured(error)) {
                setError(null)
              }
            }}
          />
        </div>
      )}

      {error &&
        !looksLikeLLMNotConfigured(error) &&
        !looksLikeFreeQuotaExceeded(error) && (
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
                  mode="userBubble"
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
              {chats.length === 0 && !composerHasText && !generating && (
                <RoutineSuggestions
                  assistant={chatAssistant}
                  onPick={fillSuggestion}
                />
              )}
              <DeliveryActionBar
                need={deliveryNeed}
                disabled={!wsCanSendProp(wsStatus)}
                onSend={(text) => {
                  const clientMsgId = `${Date.now()}-delivery`
                  if (busy) {
                    appendLocalUser(text, clientMsgId)
                    sendAction({ type: 'prompt', text, clientMsgId })
                    setStatusHint('已排队')
                    return
                  }
                  sendPrompt(text, clientMsgId)
                }}
              />
              <AIChatInput
                ref={composerRef}
                keepSkillAfterSend={false}
                generating={generating}
                canSend={wsCanSendProp(wsStatus) && composerHasText}
                showUploadButton={false}
                showUploadFile={false}
                showReference={false}
                round
                extensions={composerExtensions}
                clearContentOnGenerating={false}
                placeholder={
                  workerBusy && activeWorker
                    ? t('projects.workers.workingHint', {
                        key: activeWorker.key,
                      })
                    : wsInputPlaceholder(wsStatus)
                }
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
                  // the shell to that viewport and keep the thread on the bottom
                  // so the composer sits on the keyboard.
                  const pin = () => {
                    syncVisualViewportInsets()
                    window.scrollTo(0, 0)
                    const el = scrollRef.current
                    if (el) el.scrollTop = el.scrollHeight
                  }
                  requestAnimationFrame(pin)
                  window.setTimeout(pin, 50)
                  window.setTimeout(pin, 300)
                  window.setTimeout(pin, 600)
                }}
                onBlur={() => setComposerFocused(false)}
                onContentChange={(contents) => {
                  composerContentsRef.current = contents as unknown as SendContent[]
                  setComposerHasText(contentsHaveSendableText(contents))
                }}
                onMessageSend={handleMessageSend}
                onStopGenerate={handleStopGenerate}
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
