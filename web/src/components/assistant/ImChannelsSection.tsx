import { useCallback, useEffect, useMemo, useState, type CSSProperties } from 'react'
import {
  Button,
  Input,
  Modal,
  Select,
  Switch,
  Typography,
} from '@douyinfe/semi-ui-19'
import QRCode from 'qrcode'
import {
  assistantsApi,
  IM_CHANNEL_META,
  type ImChannel,
  type ImChannels,
} from '../../api'

const SECRET_MASK = '●●●●●●●●'

const fieldLabel: CSSProperties = {
  display: 'block',
  marginBottom: 4,
}

type Props = {
  assistantId: string
  channels: ImChannels
  onChange: (next: ImChannels) => Promise<void>
  saving?: boolean
}

function metaFor(type: string) {
  return IM_CHANNEL_META.find((m) => m.type === type)
}

function channelStatus(type: string, ch: ImChannel): string {
  if (!ch.enabled) return '已关闭'
  const m = metaFor(type)
  if (!m) return '未知'
  switch (m.family) {
    case 'botToken':
    case 'weixinQR':
      return ch.token ? '已配置' : '缺少 Token'
    case 'appPair':
      if (type === 'dingtalk') {
        return ch.clientId && ch.clientSecret ? '已配置' : '凭证不完整'
      }
      return ch.appId && ch.appSecret ? '已配置' : '凭证不完整'
    case 'dualToken':
      return ch.botToken && ch.appToken ? '已配置' : '凭证不完整'
    case 'wecomWS':
      return ch.botId && ch.botSecret ? '已配置' : '凭证不完整'
    default:
      return '已配置'
  }
}

export function ImChannelsSection({
  assistantId,
  channels,
  onChange,
  saving,
}: Props) {
  const [addType, setAddType] = useState<string>('')
  const [editing, setEditing] = useState<string | null>(null)
  const [draft, setDraft] = useState<ImChannel | null>(null)
  const [weixinOpen, setWeixinOpen] = useState(false)

  const bound = useMemo(
    () => IM_CHANNEL_META.filter((m) => channels[m.type]),
    [channels],
  )
  const unbound = useMemo(
    () => IM_CHANNEL_META.filter((m) => !channels[m.type]),
    [channels],
  )

  const openEdit = (type: string) => {
    const ch = channels[type] ?? { enabled: true }
    setEditing(type)
    setDraft({ ...ch })
  }

  const saveDraft = async () => {
    if (!editing || !draft) return
    const next = { ...channels, [editing]: draft }
    await onChange(next)
    setEditing(null)
    setDraft(null)
  }

  const removeChannel = async (type: string) => {
    const next = { ...channels }
    delete next[type]
    await onChange(next)
  }

  const toggleEnabled = async (type: string, enabled: boolean) => {
    const ch = channels[type]
    if (!ch) return
    await onChange({ ...channels, [type]: { ...ch, enabled } })
  }

  const addChannel = () => {
    if (!addType) return
    if (addType === 'weixin') {
      setWeixinOpen(true)
      setAddType('')
      return
    }
    const base: ImChannel = { enabled: true }
    void onChange({ ...channels, [addType]: base }).then(() => {
      openEdit(addType)
      setAddType('')
    })
  }

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <Typography.Title heading={5} style={{ margin: 0 }}>
        即时通讯渠道
      </Typography.Title>
      <Typography.Text type="tertiary" size="small">
        每个助手可绑定自己的机器人。消息会进入该助手的对话。
      </Typography.Text>

      {bound.length === 0 ? (
        <Typography.Text type="tertiary">尚未绑定渠道</Typography.Text>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          {bound.map((m) => {
            const ch = channels[m.type]
            return (
              <div
                key={m.type}
                style={{
                  display: 'flex',
                  flexWrap: 'wrap',
                  alignItems: 'center',
                  gap: 8,
                  padding: '8px 0',
                  borderBottom: '1px solid var(--semi-color-border)',
                }}
              >
                <Typography.Text strong style={{ minWidth: 88 }}>
                  {m.label}
                </Typography.Text>
                <Switch
                  checked={ch.enabled}
                  onChange={(v) => void toggleEnabled(m.type, v)}
                  disabled={saving}
                />
                <Typography.Text type="tertiary" size="small">
                  {channelStatus(m.type, ch)}
                </Typography.Text>
                <div style={{ marginLeft: 'auto', display: 'flex', gap: 8 }}>
                  {m.type === 'weixin' && (
                    <Button
                      size="small"
                      onClick={() => setWeixinOpen(true)}
                      disabled={saving}
                    >
                      扫码连接
                    </Button>
                  )}
                  <Button
                    size="small"
                    onClick={() => openEdit(m.type)}
                    disabled={saving}
                  >
                    编辑
                  </Button>
                  <Button
                    size="small"
                    type="danger"
                    onClick={() => void removeChannel(m.type)}
                    disabled={saving}
                  >
                    移除
                  </Button>
                </div>
              </div>
            )
          })}
        </div>
      )}

      {unbound.length > 0 && (
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <Select
            placeholder="添加渠道"
            value={addType || undefined}
            onChange={(v) => setAddType(String(v))}
            style={{ minWidth: 160 }}
            optionList={unbound.map((m) => ({
              value: m.type,
              label: m.label,
            }))}
          />
          <Button
            theme="solid"
            type="primary"
            disabled={!addType || saving}
            onClick={addChannel}
          >
            添加
          </Button>
        </div>
      )}

      <Modal
        title={editing ? `编辑 ${metaFor(editing)?.label ?? editing}` : ''}
        visible={Boolean(editing && draft)}
        onCancel={() => {
          setEditing(null)
          setDraft(null)
        }}
        onOk={() => void saveDraft()}
        okText="保存"
        cancelText="取消"
        confirmLoading={saving}
      >
        {editing && draft && (
          <ChannelForm
            type={editing}
            value={draft}
            onChange={setDraft}
          />
        )}
      </Modal>

      <WeixinQRModal
        assistantId={assistantId}
        visible={weixinOpen}
        onClose={() => setWeixinOpen(false)}
        onConnected={(im) => {
          void onChange(im)
          setWeixinOpen(false)
        }}
      />
    </section>
  )
}

function ChannelForm({
  type,
  value,
  onChange,
}: {
  type: string
  value: ImChannel
  onChange: (v: ImChannel) => void
}) {
  const m = metaFor(type)
  const set = (patch: Partial<ImChannel>) => onChange({ ...value, ...patch })

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
        }}
      >
        <Typography.Text>启用</Typography.Text>
        <Switch
          checked={value.enabled}
          onChange={(v) => set({ enabled: v })}
        />
      </div>
      <div>
        <Typography.Text type="tertiary" size="small" style={fieldLabel}>
          Allow from（可选，逗号分隔）
        </Typography.Text>
        <Input
          value={value.allowFrom ?? ''}
          onChange={(v) => set({ allowFrom: v })}
          placeholder="空 = 不限制"
        />
      </div>

      {m?.family === 'botToken' && (
        <>
          <SecretField
            label="Bot Token"
            value={value.token}
            onChange={(token) => set({ token })}
          />
          {type === 'telegram' && (
            <>
              <div>
                <Typography.Text
                  type="tertiary"
                  size="small"
                  style={fieldLabel}
                >
                  Proxy（可选）
                </Typography.Text>
                <Input
                  value={value.proxy ?? ''}
                  onChange={(v) => set({ proxy: v })}
                />
              </div>
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                }}
              >
                <Typography.Text>群内回复全部消息</Typography.Text>
                <Switch
                  checked={Boolean(value.groupReplyAll)}
                  onChange={(v) => set({ groupReplyAll: v })}
                />
              </div>
            </>
          )}
        </>
      )}

      {m?.family === 'appPair' && type === 'dingtalk' && (
        <>
          <div>
            <Typography.Text type="tertiary" size="small" style={fieldLabel}>
              Client ID
            </Typography.Text>
            <Input
              value={value.clientId ?? ''}
              onChange={(v) => set({ clientId: v })}
            />
          </div>
          <SecretField
            label="Client Secret"
            value={value.clientSecret}
            onChange={(clientSecret) => set({ clientSecret })}
          />
        </>
      )}

      {m?.family === 'appPair' && type !== 'dingtalk' && (
        <>
          <div>
            <Typography.Text type="tertiary" size="small" style={fieldLabel}>
              App ID
            </Typography.Text>
            <Input
              value={value.appId ?? ''}
              onChange={(v) => set({ appId: v })}
            />
          </div>
          <SecretField
            label="App Secret"
            value={value.appSecret}
            onChange={(appSecret) => set({ appSecret })}
          />
        </>
      )}

      {m?.family === 'dualToken' && (
        <>
          <SecretField
            label="Bot Token (xoxb-…)"
            value={value.botToken}
            onChange={(botToken) => set({ botToken })}
          />
          <SecretField
            label="App Token (xapp-…)"
            value={value.appToken}
            onChange={(appToken) => set({ appToken })}
          />
        </>
      )}

      {m?.family === 'wecomWS' && (
        <>
          <Typography.Text type="tertiary" size="small">
            使用 WebSocket 模式，无需配置公网回调 URL。
          </Typography.Text>
          <div>
            <Typography.Text type="tertiary" size="small" style={fieldLabel}>
              Bot ID
            </Typography.Text>
            <Input
              value={value.botId ?? ''}
              onChange={(v) => set({ botId: v })}
            />
          </div>
          <SecretField
            label="Bot Secret"
            value={value.botSecret}
            onChange={(botSecret) => set({ botSecret })}
          />
        </>
      )}

      {m?.family === 'weixinQR' && (
        <>
          <Typography.Text type="tertiary" size="small">
            推荐使用「扫码连接」。也可粘贴已有 ilink Bearer Token。
          </Typography.Text>
          <SecretField
            label="Token"
            value={value.token}
            onChange={(token) => set({ token })}
          />
          <div>
            <Typography.Text type="tertiary" size="small" style={fieldLabel}>
              Base URL（可选）
            </Typography.Text>
            <Input
              value={value.baseUrl ?? ''}
              onChange={(v) => set({ baseUrl: v })}
              placeholder="https://ilinkai.weixin.qq.com"
            />
          </div>
        </>
      )}
    </div>
  )
}

function SecretField({
  label,
  value,
  onChange,
}: {
  label: string
  value?: string
  onChange: (v: string) => void
}) {
  const masked = Boolean(value && value === SECRET_MASK)
  return (
    <div>
      <Typography.Text type="tertiary" size="small" style={fieldLabel}>
        {label}
        {masked ? '（已保存，改则重填）' : ''}
      </Typography.Text>
      <Input
        mode="password"
        value={value ?? ''}
        onChange={onChange}
        placeholder={masked ? '留空或保持掩码则不改' : ''}
      />
    </div>
  )
}

function WeixinQRModal({
  assistantId,
  visible,
  onClose,
  onConnected,
}: {
  assistantId: string
  visible: boolean
  onClose: () => void
  onConnected: (im: ImChannels) => void
}) {
  const [qrKey, setQrKey] = useState('')
  const [qrDataUrl, setQrDataUrl] = useState('')
  const [status, setStatus] = useState('idle')
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    setError(null)
    setStatus('loading')
    try {
      const begin = await assistantsApi.weixinBegin(assistantId)
      setQrKey(begin.qrKey)
      const url = await QRCode.toDataURL(begin.qrUrl, {
        width: 220,
        margin: 2,
      })
      setQrDataUrl(url)
      setStatus('wait')
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
      setStatus('error')
    }
  }, [assistantId])

  useEffect(() => {
    if (!visible) {
      setQrKey('')
      setQrDataUrl('')
      setStatus('idle')
      setError(null)
      return
    }
    void refresh()
  }, [visible, refresh])

  useEffect(() => {
    if (!visible || !qrKey || status === 'confirmed' || status === 'error') {
      return
    }
    let cancelled = false
    const tick = async () => {
      try {
        const res = await assistantsApi.weixinPoll(assistantId, qrKey)
        if (cancelled) return
        setStatus(res.status || 'wait')
        if (res.status === 'confirmed' && res.imChannels) {
          onConnected(res.imChannels)
          return
        }
        if (res.status === 'expired') {
          void refresh()
        }
      } catch (e) {
        if (!cancelled) {
          setError(e instanceof Error ? e.message : String(e))
        }
      }
    }
    const id = window.setInterval(() => void tick(), 2000)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
  }, [visible, qrKey, status, assistantId, onConnected, refresh])

  const statusText =
    status === 'loading'
      ? '正在获取二维码…'
      : status === 'wait'
        ? '请使用微信扫码'
        : status === 'scaned'
          ? '已扫码，请在手机上确认'
          : status === 'confirmed'
            ? '已连接'
            : status === 'error'
              ? '出错了'
              : status

  return (
    <Modal
      title="微信扫码连接"
      visible={visible}
      onCancel={onClose}
      footer={
        <Button onClick={() => void refresh()} disabled={status === 'loading'}>
          刷新二维码
        </Button>
      }
    >
      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          alignItems: 'center',
          gap: 12,
        }}
      >
        {qrDataUrl ? (
          <img src={qrDataUrl} alt="微信登录二维码" width={220} height={220} />
        ) : (
          <Typography.Text type="tertiary">加载中…</Typography.Text>
        )}
        <Typography.Text>{statusText}</Typography.Text>
        {error && (
          <Typography.Text type="danger" size="small">
            {error}
          </Typography.Text>
        )}
        <Typography.Text type="tertiary" size="small">
          扫码后首次请从微信给机器人发一条消息以完成会话关联。
        </Typography.Text>
      </div>
    </Modal>
  )
}
