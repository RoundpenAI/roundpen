import { useCallback, useEffect, useState, type CSSProperties } from 'react'
import { Link, useNavigate, useParams } from 'react-router-dom'
import {
  Banner,
  Button,
  Input,
  List,
  Modal,
  Radio,
  RadioGroup,
  Select,
  Spin,
  Switch,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui-19'
import {
  assistantsApi,
  ApiError,
  type ActivityItem,
  type AssistTicket,
  type Assistant,
  type AssistantCapabilities,
  type AssistantDirectoryGrant,
} from '../api'
import { useAssistantLayout } from '../components/AssistantLayout'
import { isSystemAssistant } from '../lib/assistants'

const fieldLabel: CSSProperties = {
  display: 'block',
  marginBottom: 4,
}

const sectionGap: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 12,
}

export function AssistantDetailPage() {
  const { assistantId = '' } = useParams()
  const navigate = useNavigate()
  const { refresh } = useAssistantLayout()
  const [a, setA] = useState<Assistant | null>(null)
  const [activity, setActivity] = useState<ActivityItem[]>([])
  const [busyNow, setBusyNow] = useState(false)
  const [tickets, setTickets] = useState<AssistTicket[]>([])
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [name, setName] = useState('')
  const [bio, setBio] = useState('')
  const [newPath, setNewPath] = useState('')
  const [newMode, setNewMode] = useState<'read' | 'readwrite'>('read')
  const [allowText, setAllowText] = useState('')

  const load = useCallback(async () => {
    try {
      const [got, act, tix] = await Promise.all([
        assistantsApi.get(assistantId),
        assistantsApi.activity(assistantId).catch(() => ({
          activity: [] as ActivityItem[],
          busy: false,
        })),
        assistantsApi.listTickets(assistantId, true).catch(() => ({
          tickets: [] as AssistTicket[],
        })),
      ])
      setA(got)
      setName(got.name)
      setBio(got.bio)
      setAllowText((got.networkAllowlist ?? []).join('\n'))
      setActivity(act.activity ?? [])
      setBusyNow(Boolean(act.busy))
      setTickets(tix.tickets ?? [])
      setError(null)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    }
  }, [assistantId])

  useEffect(() => {
    void load()
  }, [load])

  const patch = async (body: Parameters<typeof assistantsApi.update>[1]) => {
    setSaving(true)
    setError(null)
    try {
      const updated = await assistantsApi.update(assistantId, body)
      setA(updated)
      setName(updated.name)
      setBio(updated.bio)
      setAllowText((updated.networkAllowlist ?? []).join('\n'))
      await refresh()
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  if (!a && !error) {
    return (
      <div
        className="chat-pane"
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <Spin tip="加载中…" />
      </div>
    )
  }
  if (!a) {
    return (
      <div className="chat-pane" style={{ padding: 24 }} role="alert">
        <Banner fullMode={false} type="danger" description={error} closeIcon={null} />
      </div>
    )
  }

  const caps = a.capabilities

  const setCap = (key: keyof AssistantCapabilities, value: boolean) => {
    const next = { ...caps, [key]: value, mobile: false, desktop: false }
    void patch({ capabilities: next })
  }

  const addGrant = () => {
    const path = newPath.trim()
    if (!path) return
    const grants: AssistantDirectoryGrant[] = [
      ...(a.directoryGrants ?? []),
      { path, mode: newMode, createdAt: new Date().toISOString() },
    ]
    setNewPath('')
    void patch({ directoryGrants: grants })
  }

  const removeGrant = (path: string) => {
    void patch({
      directoryGrants: (a.directoryGrants ?? []).filter((g) => g.path !== path),
    })
  }

  const changeIdentity = (mode: 'proxy_user' | 'independent') => {
    if (mode === a.identityMode) return
    if (
      !window.confirm(
        '切换身份模式会使已绑定渠道失效或需要重连。确定继续？',
      )
    ) {
      return
    }
    void patch({ identityMode: mode, confirmIdentityChange: true })
  }

  const confirmDelete = () => {
    Modal.confirm({
      title: `删除助手「${a.name}」？`,
      content: (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          <Typography.Text>
            删除后将立即从侧边栏消失，无法再打开与它的对话。
          </Typography.Text>
          <Typography.Text type="tertiary" size="small">
            其主会话与历史消息会停止用于新任务；已写入工作区的文件不会自动清除。此操作不可从列表撤销。
          </Typography.Text>
        </div>
      ),
      okText: '删除助手',
      okType: 'danger',
      cancelText: '取消',
      onOk: async () => {
        setDeleting(true)
        setError(null)
        try {
          await assistantsApi.update(assistantId, { status: 'disabled' })
          await refresh()
          navigate('/a', { replace: true })
        } catch (e) {
          setError(e instanceof ApiError ? e.message : String(e))
          throw e
        } finally {
          setDeleting(false)
        }
      },
    })
  }

  const sortedActivity = [...activity]
    .sort((x, y) => Date.parse(y.at) - Date.parse(x.at))
    .slice(0, 40)

  return (
    <div className="chat-pane">
      <div
        className="chat-pane-scroll"
        style={{
          maxWidth: 672,
          margin: '0 auto',
          padding: '16px 12px',
          width: '100%',
          boxSizing: 'border-box',
          display: 'flex',
          flexDirection: 'column',
          gap: 32,
        }}
      >
        <div
          style={{
            display: 'flex',
            flexWrap: 'wrap',
            alignItems: 'center',
            justifyContent: 'space-between',
            gap: 12,
          }}
        >
          <div style={{ minWidth: 0 }}>
            <Typography.Title
              heading={3}
              ellipsis={{ showTooltip: true }}
              style={{ margin: 0 }}
            >
              {a.name}
            </Typography.Title>
            <Typography.Text type="tertiary" size="small">
              状态：{a.status === 'active' ? '可用' : '已停用'}
              {a.primarySessionId ? ' · 有对话' : ''}
            </Typography.Text>
          </div>
          <Link to={`/a/${a.id}/chat`} style={{ flexShrink: 0 }}>
            <Button theme="solid" type="primary" size="small">
              打开对话
            </Button>
          </Link>
        </div>

        {error && (
          <div role="alert">
            <Banner
              fullMode={false}
              type="danger"
              description={error}
              closeIcon={null}
            />
          </div>
        )}

        <section style={sectionGap}>
          <Typography.Title heading={5} style={{ margin: 0 }}>
            基本信息
          </Typography.Title>
          <div>
            <Typography.Text type="tertiary" size="small" style={fieldLabel}>
              名称
            </Typography.Text>
            <Input
              value={name}
              onChange={setName}
              onBlur={() => {
                if (name.trim() && name.trim() !== a.name) {
                  void patch({ name: name.trim() })
                }
              }}
            />
          </div>
          <div>
            <Typography.Text type="tertiary" size="small" style={fieldLabel}>
              简介
            </Typography.Text>
            <TextArea
              value={bio}
              onChange={setBio}
              rows={4}
              onBlur={() => {
                if (bio !== a.bio) void patch({ bio })
              }}
            />
            <Typography.Text
              type="tertiary"
              size="small"
              style={{ display: 'block', marginTop: 4 }}
            >
              简介用于派活时匹配最合适的助手。
            </Typography.Text>
          </div>
        </section>

        <section style={sectionGap}>
          <Typography.Title heading={5} style={{ margin: 0 }}>
            此刻
          </Typography.Title>
          <Typography.Text type="tertiary" size="small">
            {busyNow ? '正在工作中…' : '当前空闲'}
            {a.primarySessionId ? ' · 有主对话' : ''}
          </Typography.Text>
          {tickets.length > 0 && (
            <Banner
              fullMode={false}
              type="warning"
              closeIcon={null}
              description={
                <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
                  <Typography.Text strong>待处理协助单</Typography.Text>
                  {tickets.map((t) => (
                    <div key={t.id}>
                      <Typography.Text>{t.title}</Typography.Text>
                      <Typography.Text
                        type="tertiary"
                        size="small"
                        style={{ display: 'block' }}
                      >
                        {t.askHuman || t.reason}
                      </Typography.Text>
                      <div
                        style={{
                          marginTop: 8,
                          display: 'flex',
                          flexWrap: 'wrap',
                          gap: 8,
                        }}
                      >
                        {t.kind === 'policy_apply' ? (
                          <>
                            <Button
                              size="small"
                              onClick={() =>
                                void assistantsApi
                                  .resolveTicket(t.id, { resolution: 'allow_once' })
                                  .then(() => load())
                              }
                            >
                              允许一次
                            </Button>
                            <Button
                              size="small"
                              theme="solid"
                              type="primary"
                              onClick={() =>
                                void assistantsApi
                                  .resolveTicket(t.id, { resolution: 'permanent' })
                                  .then(() => {
                                    void refresh()
                                    return load()
                                  })
                              }
                            >
                              写入档案并继续
                            </Button>
                            <Button
                              size="small"
                              type="tertiary"
                              onClick={() =>
                                void assistantsApi
                                  .resolveTicket(t.id, { resolution: 'reject' })
                                  .then(() => load())
                              }
                            >
                              拒绝
                            </Button>
                          </>
                        ) : null}
                        <Link to={`/a/${a.id}/chat`}>
                          <Button size="small" type="tertiary">
                            在对话中处理
                          </Button>
                        </Link>
                      </div>
                    </div>
                  ))}
                </div>
              }
            />
          )}
          <div
            style={{
              maxHeight: 256,
              overflowY: 'auto',
              border: '1px solid var(--semi-color-border)',
              borderRadius: 8,
              padding: 12,
              background: 'var(--semi-color-bg-1)',
            }}
          >
            {sortedActivity.length === 0 ? (
              <Typography.Text type="tertiary" size="small">
                暂无活动记录
              </Typography.Text>
            ) : (
              <List
                size="small"
                dataSource={sortedActivity}
                renderItem={(it) => (
                  <List.Item
                    style={{ padding: '4px 0' }}
                    main={
                      <Typography.Text size="small">
                        <Typography.Text type="tertiary" size="small">
                          {new Date(it.at).toLocaleString()}
                        </Typography.Text>{' '}
                        <Tag size="small" color="grey">
                          {it.kind}
                        </Tag>{' '}
                        {it.title}
                        {it.detail ? (
                          <Typography.Text type="tertiary" size="small">
                            {' '}
                            — {it.detail}
                          </Typography.Text>
                        ) : null}
                      </Typography.Text>
                    }
                  />
                )}
              />
            )}
          </div>
        </section>

        <section style={sectionGap}>
          <Typography.Title heading={5} style={{ margin: 0 }}>
            可见范围
          </Typography.Title>
          <Typography.Text type="tertiary" size="small">
            默认有一个仅它可见的工作区。需要访问本机目录时在此授权。
          </Typography.Text>
          <List
            size="small"
            dataSource={a.directoryGrants ?? []}
            emptyContent={null}
            renderItem={(g) => (
              <List.Item
                style={{
                  border: '1px solid var(--semi-color-border)',
                  borderRadius: 8,
                  marginBottom: 8,
                  padding: '8px 12px',
                }}
                main={
                  <Typography.Text ellipsis={{ showTooltip: true }}>
                    {g.path}{' '}
                    <Typography.Text type="tertiary" size="small">
                      ({g.mode === 'readwrite' ? '读写' : '只读'})
                    </Typography.Text>
                  </Typography.Text>
                }
                extra={
                  <Button
                    size="small"
                    type="tertiary"
                    onClick={() => removeGrant(g.path)}
                  >
                    移除
                  </Button>
                }
              />
            )}
          />
          <div
            style={{
              display: 'flex',
              flexWrap: 'wrap',
              gap: 8,
              alignItems: 'center',
            }}
          >
            <Input
              style={{ flex: '1 1 180px', minWidth: 0 }}
              placeholder="/path/to/folder"
              value={newPath}
              onChange={setNewPath}
            />
            <Select
              value={newMode}
              onChange={(v) => setNewMode(v as 'read' | 'readwrite')}
              style={{ width: 100 }}
            >
              <Select.Option value="read">只读</Select.Option>
              <Select.Option value="readwrite">读写</Select.Option>
            </Select>
            <Button onClick={addGrant}>添加授权</Button>
          </div>
        </section>

        <section style={sectionGap}>
          <Typography.Title heading={5} style={{ margin: 0 }}>
            能力清单
          </Typography.Title>
          {(
            [
              ['shell', '终端', true],
              ['browser', '浏览器', true],
              ['mobile', '手机', false],
              ['desktop', '桌面', false],
            ] as const
          ).map(([key, label, ready]) => (
            <div
              key={key}
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: 12,
                border: '1px solid var(--semi-color-border)',
                borderRadius: 8,
                padding: '8px 12px',
              }}
            >
              <span>
                {label}
                {!ready && (
                  <Tag size="small" color="grey" style={{ marginLeft: 8 }}>
                    即将推出
                  </Tag>
                )}
              </span>
              <Switch
                checked={Boolean(caps[key])}
                disabled={!ready || saving}
                onChange={(checked) => setCap(key, checked)}
              />
            </div>
          ))}
        </section>

        <section style={sectionGap}>
          <Typography.Title heading={5} style={{ margin: 0 }}>
            网络
          </Typography.Title>
          <RadioGroup
            direction="vertical"
            value={a.networkTier}
            disabled={saving}
            onChange={(e) => {
              const tier = e.target.value as 'none' | 'dev_sites' | 'all'
              if (tier === 'all' && !window.confirm('允许全部出站风险最高，确定？')) {
                return
              }
              void patch({ networkTier: tier })
            }}
          >
            <Radio value="none">禁止上网</Radio>
            <Radio value="dev_sites">常用开发站</Radio>
            <Radio value="all">允许全部</Radio>
          </RadioGroup>
          <div>
            <Typography.Text type="tertiary" size="small" style={fieldLabel}>
              高级白名单（每行一个域名）
            </Typography.Text>
            <TextArea
              value={allowText}
              onChange={setAllowText}
              rows={3}
              style={{ fontFamily: 'var(--semi-font-family-regular), monospace', fontSize: 12 }}
              onBlur={() => {
                const list = allowText
                  .split(/[\n,]+/)
                  .map((s) => s.trim())
                  .filter(Boolean)
                const prev = a.networkAllowlist ?? []
                if (JSON.stringify(list) !== JSON.stringify(prev)) {
                  void patch({ networkAllowlist: list })
                }
              }}
            />
          </div>
        </section>

        <section style={sectionGap}>
          <Typography.Title heading={5} style={{ margin: 0 }}>
            身份绑定
          </Typography.Title>
          <Typography.Text type="secondary" size="small">
            当前：{a.identityMode === 'proxy_user' ? '代理我' : '独立身份'}
          </Typography.Text>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
            <Button
              size="small"
              theme={a.identityMode === 'proxy_user' ? 'solid' : 'light'}
              type={a.identityMode === 'proxy_user' ? 'primary' : 'tertiary'}
              onClick={() => changeIdentity('proxy_user')}
            >
              代理我
            </Button>
            <Button
              size="small"
              theme={a.identityMode === 'independent' ? 'solid' : 'light'}
              type={a.identityMode === 'independent' ? 'primary' : 'tertiary'}
              onClick={() => changeIdentity('independent')}
            >
              独立身份
            </Button>
          </div>
          {a.identityMode === 'proxy_user' ? (
            <Typography.Text type="tertiary" size="small">
              使用你的账号。可在{' '}
              <Link to="/settings" style={{ color: 'var(--semi-color-link)' }}>
                设置
              </Link>{' '}
              中管理 Git 等连接。
            </Typography.Text>
          ) : (
            <Typography.Text type="tertiary" size="small">
              助手专用账号将在后续版本连接。
            </Typography.Text>
          )}
        </section>

        {!isSystemAssistant(a) && (
        <section style={sectionGap}>
          <Typography.Title heading={5} style={{ margin: 0 }}>
            删除助手
          </Typography.Title>
          <Typography.Text type="tertiary" size="small">
            删除后助手会从列表中移除，相关对话入口关闭；工作区里已有文件不会自动清理。请确认后再操作。
          </Typography.Text>
          <div>
            <Button
              type="danger"
              theme="solid"
              loading={deleting}
              disabled={saving}
              onClick={confirmDelete}
            >
              删除此助手
            </Button>
          </div>
        </section>
        )}

        {saving && (
          <Typography.Text type="tertiary" size="small">
            保存中…
          </Typography.Text>
        )}
      </div>
    </div>
  )
}
