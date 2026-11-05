import { useCallback, useEffect, useState } from 'react'
import { Banner, Button, Switch, Typography } from '@douyinfe/semi-ui-19'
import { nasScene, type NasSceneStatus } from '../api'
import { useT, type MessageKey } from '../i18n'
import { Loading } from './Loading'

export function NasScenePanel() {
  const t = useT()
  const [status, setStatus] = useState<NasSceneStatus | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      setStatus(await nasScene.get())
    } catch (e) {
      setError(e instanceof Error ? e.message : t('nasScene.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  async function toggle(enabled: boolean) {
    setBusy(true)
    setError(null)
    try {
      setStatus(await nasScene.setEnabled(enabled))
    } catch (e) {
      setError(e instanceof Error ? e.message : t('nasScene.saveFailed'))
    } finally {
      setBusy(false)
    }
  }

  async function restore() {
    setBusy(true)
    setError(null)
    try {
      setStatus(await nasScene.restore())
    } catch (e) {
      setError(e instanceof Error ? e.message : t('nasScene.restoreFailed'))
    } finally {
      setBusy(false)
    }
  }

  if (loading) return <Loading />

  const missing =
    status?.enabled &&
    status.members.some((m) => m.status === 'missing' || !m.assistantId)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <div>
        <Typography.Title heading={5} style={{ margin: 0 }}>
          {t('nasScene.title')}
        </Typography.Title>
        <Typography.Text type="tertiary">{t('nasScene.blurb')}</Typography.Text>
      </div>

      {error && (
        <Banner type="danger" description={error} closeIcon={null} />
      )}

      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 12,
        }}
      >
        <div>
          <Typography.Text strong>{t('nasScene.enable')}</Typography.Text>
          <div>
            <Typography.Text type="tertiary" size="small">
              {t('nasScene.enableHint')}
            </Typography.Text>
          </div>
        </div>
        <Switch
          checked={Boolean(status?.enabled)}
          disabled={busy}
          onChange={(v) => void toggle(v)}
        />
      </div>

      {status && (
        <ul style={{ margin: 0, paddingLeft: 18, lineHeight: 1.7 }}>
          {status.members.map((m) => (
            <li key={m.key}>
              <Typography.Text>
                {m.name}{' '}
                <Typography.Text type="tertiary" size="small">
                  ({t(statusKey(m.status))})
                </Typography.Text>
              </Typography.Text>
            </li>
          ))}
        </ul>
      )}

      {missing ? (
        <Button disabled={busy} onClick={() => void restore()}>
          {t('nasScene.restore')}
        </Button>
      ) : null}
    </div>
  )
}

function statusKey(status: string): MessageKey {
  switch (status) {
    case 'active':
      return 'nasScene.status.active'
    case 'disabled':
      return 'nasScene.status.disabled'
    default:
      return 'nasScene.status.missing'
  }
}
