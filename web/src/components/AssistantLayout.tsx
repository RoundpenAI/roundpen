import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react'
import {
  Navigate,
  Outlet,
  useLocation,
  useNavigate,
  useParams,
} from 'react-router-dom'
import {
  Badge,
  Banner,
  Button,
  Layout,
  List,
  SideSheet,
  Spin,
  Typography,
} from '@douyinfe/semi-ui-19'
import { IconPlus, IconMenu } from '@douyinfe/semi-icons'
import {
  assistantsApi,
  ApiError,
  type Assistant,
  type AssistTicket,
} from '../api'
import { pickHomeAssistant } from '../lib/assistants'

const { Sider, Header, Content } = Layout

type AssistantLayoutValue = {
  assistants: Assistant[]
  refresh: () => Promise<void>
}

const AssistantLayoutContext = createContext<AssistantLayoutValue | null>(null)

export function useAssistantLayout(): AssistantLayoutValue {
  const ctx = useContext(AssistantLayoutContext)
  if (!ctx) {
    throw new Error('useAssistantLayout must be used inside AssistantLayout')
  }
  return ctx
}

function bioLine(a: Assistant): string {
  const bio = a.bio.trim()
  if (bio) return bio.length > 40 ? `${bio.slice(0, 37)}…` : bio
  return '补充简介以便派活'
}

export function AssistantLayout() {
  const navigate = useNavigate()
  const location = useLocation()
  const { assistantId } = useParams()

  const [assistants, setAssistants] = useState<Assistant[]>([])
  const [pending, setPending] = useState<AssistTicket[]>([])
  const [pendingOpen, setPendingOpen] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [mobileOpen, setMobileOpen] = useState(false)

  const refresh = useCallback(async () => {
    try {
      const [res, pend] = await Promise.all([
        assistantsApi.list(),
        assistantsApi.pendingTickets().catch(() => ({ count: 0, tickets: [] })),
      ])
      setAssistants(res.assistants ?? [])
      setPending(pend.tickets ?? [])
      setError(null)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    }
  }, [])

  useEffect(() => {
    void refresh()
  }, [refresh])

  useEffect(() => {
    setMobileOpen(false)
  }, [location.pathname])

  const openAssistant = async (id: string) => {
    try {
      const { sessionId } = await assistantsApi.ensureSession(id)
      navigate(`/a/${id}/s/${sessionId}`)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    }
  }

  const value = useMemo(
    () => ({ assistants, refresh }),
    [assistants, refresh],
  )

  const title =
    assistants.find((a) => a.id === assistantId)?.name ||
    (location.pathname.includes('/new') ? '新建助手' : '助手')

  const sidebarBody = (opts: { onNavigate?: () => void }) => (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        height: '100%',
        background: 'var(--semi-color-bg-1)',
      }}
    >
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          padding: '12px 10px',
          borderBottom: '1px solid var(--semi-color-border)',
        }}
      >
        <Typography.Text
          strong
          style={{
            flex: 1,
            minWidth: 0,
            overflow: 'hidden',
            textOverflow: 'ellipsis',
            whiteSpace: 'nowrap',
          }}
        >
          助手
        </Typography.Text>
        <Button
          theme="borderless"
          type="tertiary"
          icon={<IconPlus />}
          aria-label="新建助手"
          onClick={() => {
            opts.onNavigate?.()
            navigate('/a/new')
          }}
        />
        <Badge count={pending.length} overflowCount={99} type="warning">
          <Button
            theme="borderless"
            type="tertiary"
            aria-label="待处理"
            onClick={() => setPendingOpen((v) => !v)}
          >
            待办
          </Button>
        </Badge>
      </div>

      {pendingOpen && (
        <div
          style={{
            padding: 12,
            borderBottom: '1px solid var(--semi-color-border)',
          }}
        >
          <Typography.Text type="tertiary" size="small">
            待处理
          </Typography.Text>
          {pending.length === 0 ? (
            <Typography.Text
              type="tertiary"
              size="small"
              style={{ display: 'block' }}
            >
              没有待处理协助单
            </Typography.Text>
          ) : (
            <List
              size="small"
              dataSource={pending}
              renderItem={(t) => (
                <List.Item
                  style={{ cursor: 'pointer', padding: '6px 0' }}
                  onClick={() => {
                    setPendingOpen(false)
                    opts.onNavigate?.()
                    void openAssistant(t.assistantId)
                  }}
                >
                  {t.title}
                </List.Item>
              )}
            />
          )}
        </div>
      )}

      <div style={{ padding: '8px 10px' }}>
        <Button
          block
          theme="solid"
          type="primary"
          icon={<IconPlus />}
          onClick={() => {
            opts.onNavigate?.()
            navigate('/a/new')
          }}
        >
          新建助手
        </Button>
      </div>

      <div style={{ flex: 1, overflow: 'auto', minHeight: 0 }}>
        {assistants.length === 0 && (
          <Typography.Text
            type="tertiary"
            size="small"
            style={{ display: 'block', padding: 16 }}
          >
            还没有助手
          </Typography.Text>
        )}
        {assistants.map((a) => {
          const active = a.id === assistantId
          return (
            <div
              key={a.id}
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 4,
                padding: '4px 8px',
                background: active
                  ? 'var(--semi-color-fill-0)'
                  : 'transparent',
                borderRadius: 6,
                margin: '2px 6px',
              }}
            >
              <button
                type="button"
                style={{
                  flex: 1,
                  minWidth: 0,
                  textAlign: 'left',
                  border: 'none',
                  background: 'transparent',
                  color: 'var(--semi-color-text-0)',
                  cursor: 'pointer',
                  padding: '6px 4px',
                }}
                title={a.name}
                onClick={() => {
                  opts.onNavigate?.()
                  void openAssistant(a.id)
                }}
              >
                <Typography.Text
                  strong
                  ellipsis={{ showTooltip: true }}
                  style={{ display: 'block' }}
                >
                  {a.name}
                  {a.kind === 'system' ? (
                    <Typography.Text
                      type="tertiary"
                      size="small"
                      style={{ marginLeft: 6, fontWeight: 400 }}
                    >
                      系统
                    </Typography.Text>
                  ) : null}
                </Typography.Text>
                <Typography.Text
                  type="tertiary"
                  size="small"
                  ellipsis
                  style={{ display: 'block' }}
                >
                  {bioLine(a)}
                </Typography.Text>
              </button>
            </div>
          )
        })}
      </div>
    </div>
  )

  return (
    <AssistantLayoutContext.Provider value={value}>
      <Layout style={{ height: '100%', background: 'var(--semi-color-bg-0)' }}>
        <Sider
          style={{
            width: 260,
            maxWidth: 260,
            minWidth: 260,
            background: 'var(--semi-color-bg-1)',
            borderRight: '1px solid var(--semi-color-border)',
            display: 'none',
          }}
          className="rp-assistant-sider-desktop"
        >
          {sidebarBody({})}
        </Sider>

        <SideSheet
          title="助手"
          visible={mobileOpen}
          onCancel={() => setMobileOpen(false)}
          placement="left"
          width={280}
          bodyStyle={{ padding: 0 }}
        >
          {sidebarBody({
            onNavigate: () => setMobileOpen(false),
          })}
        </SideSheet>

        <Layout>
          <Header
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 8,
              padding: '0 12px',
              height: 48,
              background: 'var(--semi-color-bg-1)',
              borderBottom: '1px solid var(--semi-color-border)',
            }}
          >
            <Button
              theme="borderless"
              type="tertiary"
              icon={<IconMenu />}
              aria-label="打开助手列表"
              className="rp-assistant-menu-mobile"
              onClick={() => setMobileOpen(true)}
            />
            <Typography.Text
              strong
              ellipsis={{ showTooltip: true }}
              style={{ flex: 1, minWidth: 0 }}
            >
              {title}
            </Typography.Text>
            {assistantId && (
              <Button
                theme="borderless"
                type="tertiary"
                size="small"
                onClick={() => navigate(`/a/${assistantId}`)}
              >
                详情
              </Button>
            )}
          </Header>
          <Content
            style={{
              minHeight: 0,
              minWidth: 0,
              overflow: 'hidden',
              display: 'flex',
              flexDirection: 'column',
              flex: 1,
            }}
          >
            {error && (
              <div role="alert" style={{ padding: '8px 16px' }}>
                <Banner
                  fullMode={false}
                  type="danger"
                  description={error}
                  closeIcon={null}
                />
              </div>
            )}
            <Outlet />
          </Content>
        </Layout>
      </Layout>
      <style>{`
        @media (min-width: 768px) {
          .rp-assistant-sider-desktop { display: block !important; }
          .rp-assistant-menu-mobile { display: none !important; }
        }
      `}</style>
    </AssistantLayoutContext.Provider>
  )
}

export function AssistantsIndexRedirect() {
  const { assistants, refresh } = useAssistantLayout()
  const [ready, setReady] = useState(false)

  useEffect(() => {
    void refresh().finally(() => setReady(true))
  }, [refresh])

  if (!ready && assistants.length === 0) {
    return (
      <div
        style={{
          flex: 1,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <Spin tip="加载中…" />
      </div>
    )
  }
  const home = pickHomeAssistant(assistants)
  if (!home) {
    return (
      <div
        style={{
          flex: 1,
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <Spin tip="加载中…" />
      </div>
    )
  }
  return <Navigate to={`/a/${home.id}/chat`} replace />
}
