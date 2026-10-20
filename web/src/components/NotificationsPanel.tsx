import { useCallback, useEffect, useState } from 'react'
import { Banner, Button, Spin, Typography } from '@douyinfe/semi-ui-19'
import { useT } from '../i18n'
import {
  disablePushNotifications,
  enablePushNotifications,
  fetchPushStatus,
  type PushStatus,
} from '../lib/pushNotifications'
import { isPushSupported } from '../lib/pushSupport'

export function NotificationsPanel() {
  const t = useT()
  const supported = isPushSupported()
  const [status, setStatus] = useState<PushStatus | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const refresh = useCallback(async () => {
    if (!supported) return
    try {
      const s = await fetchPushStatus()
      setStatus(s)
      setError(null)
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    }
  }, [supported])

  useEffect(() => {
    if (!supported) return
    void refresh()
  }, [refresh, supported])

  const onEnable = async () => {
    setBusy(true)
    setError(null)
    try {
      await enablePushNotifications()
      await refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const onDisable = async () => {
    setBusy(true)
    setError(null)
    try {
      await disablePushNotifications()
      await refresh()
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  if (!supported) {
    return null
  }

  if (!status) {
    return <Spin />
  }

  if (!status.configured) {
    return (
      <Banner
        type="warning"
        description={t('settings.notifications.serverOff')}
        closeIcon={null}
      />
    )
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <Typography.Paragraph type="tertiary" style={{ margin: 0 }}>
        {t('settings.notifications.hint')}
      </Typography.Paragraph>
      {error && (
        <Banner type="danger" description={error} onClose={() => setError(null)} />
      )}
      <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
        <Typography.Text>
          {status.subscribed
            ? t('settings.notifications.on')
            : t('settings.notifications.off')}
        </Typography.Text>
        {status.subscribed ? (
          <Button type="tertiary" loading={busy} onClick={() => void onDisable()}>
            {t('settings.notifications.disable')}
          </Button>
        ) : (
          <Button type="primary" loading={busy} onClick={() => void onEnable()}>
            {t('settings.notifications.enable')}
          </Button>
        )}
      </div>
    </div>
  )
}
