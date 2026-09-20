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
import { SessionTabs } from '../components/SessionTabs'
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
} from '../lib/slashCommand'
import {
  clearPendingPrompt,
  clearStreaming,
  DIALOGUE_RENDER,
  isStreamingMessage,
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
import { connectChatSocket } from './chat/sessionSocket'

export function ChatSessionPage() {
  const { id = '', assistantId: assistantIdParam } = useParams<{
    id?: string
    assistantId?: string
  }>()
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
  const [commands, setCommands] = useState<AgentCommand[]>([])
  const busyRef = useRef(false)
  useEffect(() => {
    busyRef.current = busy
  }, [busy])
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
        // If a turn is already streaming, don't clobber it with persisted
        // history; the runner snapshot and done/error refetch reconcile later.
        if (!busyRef.current) {
          setMessages(hist.messages ?? [])
        }
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
        refreshCommands,
      }),
    [id, refreshCommands],
  )

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

  const sendCommand = (ws: WebSocket, name: string, args: string) => {
    setBusy(true)
    setStatusHint('Working…')
    setError(null)
    appendLocalUser(`/${name}${args ? ` ${args}` : ''}`)
    ws.send(JSON.stringify({ type: 'command', name, args }))
  }

  const handleMessageSend = (payload: MessageContent) => {
    const out = buildSendPayload(payload.inputContents, commands)
    if (!out) return
    setComposerHasText(false)
    setComposerFocused(false)
    const ws = wsRef.current
    const open = ws && ws.readyState === WebSocket.OPEN
    if (out.kind === 'command') {
      // Commands are never queued offline: the runner cancels the current turn
      // and resets the runtime, which only makes sense against a live session.
      if (open) {
        sendCommand(ws, out.name, out.args)
      } else {
        setError('连接断开，命令未发送：请等待重连后再试')
      }
      return
    }
    if (open) {
      sendPrompt(ws, out.text)
      return
    }
    // WeChat-style: show the bubble immediately; deliver when WS is ready.
    appendLocalUser(out.text)
    outboxRef.current = enqueueOutbox(outboxRef.current, out.text)
    setStatusHint(outboxWaitingHint(outboxRef.current.length))
    setError(null)
  }

  useEffect(() => {
    const pending = readPendingPrompt(id).trim()
    const ws = wsRef.current
    if (!pending || sentPending.has(id) || !histReady || busy) {
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
  }, [histReady, wsOpen, id, busy])

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
                  打个招呼，开启对话…
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
