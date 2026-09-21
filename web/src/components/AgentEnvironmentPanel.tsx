import { useCallback, useEffect, useState, type CSSProperties } from 'react'
import { Banner, Button, Modal, Radio, Select, Typography } from '@douyinfe/semi-ui-19'
import {
  environments,
  modelSource,
  slotChoices,
  type EnvironmentView,
  type ModelSource,
  type SlotChoice,
} from '../api'
import { useT, type MessageKey } from '../i18n'

const sectionGap: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 16,
}

const STATUS_KEYS: Record<string, MessageKey> = {
  up_to_date: 'agentEnv.upToDate',
  upgraded: 'agentEnv.upgraded',
  restarted: 'agentEnv.restarted',
  created: 'agentEnv.created',
}

export function AgentEnvironmentPanel() {
  const t = useT()
  const [view, setView] = useState<EnvironmentView | null>(null)
  const [source, setSource] = useState<ModelSource>('gateway')
  const [proxyOptions, setProxyOptions] = useState<SlotChoice[]>([])
  const [agentProxy, setAgentProxy] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      const [envRes, srcRes, proxyChoices, proxyCurrent] = await Promise.all([
        environments.list(),
        modelSource.get(),
        slotChoices.options('proxy'),
        slotChoices.current('proxy.agent'),
      ])
      setView((envRes.environments ?? []).find((v) => v.slot === 'agent') ?? null)
      setSource(srcRes.modelSource ?? 'gateway')
      setProxyOptions(proxyChoices)
      setAgentProxy(proxyCurrent)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('agentEnv.loadFailed'))
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  const changeSource = useCallback(
    (next: ModelSource) => {
      if (next === source) {
        return
      }
      Modal.confirm({
        title: t('agentEnv.modelSource'),
        content: t('agentEnv.modelSource.confirm'),
        onOk: async () => {
          setBusy(true)
          setNotice(null)
          setError(null)
          try {
            const res = await modelSource.set(next)
            setSource(res.modelSource)
            if (res.rebuildError) {
              setError(`${t('agentEnv.modelSource.rebuildFailed')} ${res.rebuildError}`)
            } else if (res.status && res.status !== 'absent') {
              setNotice(t('agentEnv.modelSource.rebuilt'))
            } else {
              setNotice(t('agentEnv.modelSource.absent'))
            }
            await load()
          } catch (e) {
            setError(e instanceof Error ? e.message : t('agentEnv.modelSource.failed'))
            await load()
          } finally {
            setBusy(false)
          }
        },
      })
    },
    [source, t, load],
  )

  const upgrade = useCallback(
    async (force: boolean) => {
      setBusy(true)
      setNotice(null)
      setError(null)
      try {
        const res = await environments.upgradeAgent(force)
        setNotice(t(STATUS_KEYS[res.status] ?? 'agentEnv.done'))
        await load()
      } catch (e) {
        setError(e instanceof Error ? e.message : t('agentEnv.failed'))
        await load()
      } finally {
        setBusy(false)
      }
    },
    [t, load],
  )

  const changeProxy = useCallback(
    (itemId: string) => {
      if (itemId === agentProxy) {
        return
      }
      Modal.confirm({
        title: t('agentEnv.proxy'),
        content: t('agentEnv.proxy.confirm'),
        onOk: async () => {
          setBusy(true)
          setNotice(null)
          setError(null)
          try {
            const res = await slotChoices.select('proxy.agent', itemId)
            setAgentProxy(res.itemId)
            if (res.rebuildError) {
              setError(`${t('agentEnv.proxy.rebuildFailed')} ${res.rebuildError}`)
            } else if (res.status && res.status !== 'absent') {
              setNotice(t('agentEnv.proxy.rebuilt'))
            } else {
              setNotice(t('agentEnv.proxy.absent'))
            }
            await load()
          } catch (e) {
            setError(e instanceof Error ? e.message : t('agentEnv.proxy.failed'))
            await load()
          } finally {
            setBusy(false)
          }
        },
      })
    },
    [agentProxy, t, load],
  )

  const confirmUpgrade = useCallback(
    (force: boolean) => {
      Modal.confirm({
        title: t('agentEnv.title'),
        content: force ? t('agentEnv.confirmForce') : t('agentEnv.confirmUpgrade'),
        okButtonProps: force ? { type: 'danger' as const } : undefined,
        onOk: () => upgrade(force),
      })
    },
    [t, upgrade],
  )

  return (
    <div style={sectionGap}>
      <Typography.Title heading={5}>{t('agentEnv.title')}</Typography.Title>
      <Typography.Text type="tertiary">{t('agentEnv.hint')}</Typography.Text>
      {error && (
        <div role="alert">
          <Banner type="danger" description={error} closeIcon={null} />
        </div>
      )}
      {notice && (
        <div role="status">
          <Banner type="success" description={notice} closeIcon={null} />
        </div>
      )}
      <div style={{ display: 'flex', gap: 24, flexWrap: 'wrap' }}>
        <span>
          <Typography.Text strong>{t('agentEnv.status')}: </Typography.Text>
          <Typography.Text>{view?.status ?? 'absent'}</Typography.Text>
        </span>
        <span>
          <Typography.Text strong>{t('agentEnv.image')}: </Typography.Text>
          <Typography.Text>{view?.image ?? '—'}</Typography.Text>
        </span>
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <Typography.Text strong>{t('agentEnv.modelSource')}</Typography.Text>
        <Typography.Text type="tertiary">{t('agentEnv.modelSource.hint')}</Typography.Text>
        <Radio.Group
          value={source}
          disabled={busy}
          onChange={(e) => changeSource(e.target.value as ModelSource)}
        >
          <Radio value="gateway">{t('agentEnv.modelSource.gateway')}</Radio>
          <Radio value="own">{t('agentEnv.modelSource.own')}</Radio>
        </Radio.Group>
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        <Typography.Text strong>{t('agentEnv.proxy')}</Typography.Text>
        <Typography.Text type="tertiary">{t('agentEnv.proxy.hint')}</Typography.Text>
        <Select
          style={{ maxWidth: 320 }}
          value={agentProxy}
          disabled={busy || proxyOptions.length === 0}
          onChange={(v) => changeProxy(String(v))}
          optionList={[
            { value: '', label: t('agentEnv.proxy.direct') },
            ...proxyOptions.map((p) => ({
              value: p.id,
              label: p.description ? `${p.name} — ${p.description}` : p.name,
            })),
          ]}
        />
      </div>
      <div style={{ display: 'flex', gap: 12 }}>
        <Button theme="solid" loading={busy} onClick={() => confirmUpgrade(false)}>
          {t('agentEnv.upgrade')}
        </Button>
        <Button loading={busy} onClick={() => confirmUpgrade(true)}>
          {t('agentEnv.force')}
        </Button>
      </div>
    </div>
  )
}
