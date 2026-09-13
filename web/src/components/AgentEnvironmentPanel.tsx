import { useCallback, useEffect, useState, type CSSProperties } from 'react'
import { Banner, Button, Modal, Typography } from '@douyinfe/semi-ui-19'
import { environments, type EnvironmentView } from '../api'
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
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  const load = useCallback(async () => {
    try {
      const res = await environments.list()
      setView((res.environments ?? []).find((v) => v.slot === 'agent') ?? null)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('agentEnv.loadFailed'))
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

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
