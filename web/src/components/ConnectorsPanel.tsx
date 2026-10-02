import { useCallback, useEffect, useState, type CSSProperties } from 'react'
import { Banner, Button, Input, Typography } from '@douyinfe/semi-ui-19'
import { connectors, type ConnectorStatus, type GoogleClientConfig } from '../api'
import { useT } from '../i18n'

const sectionGap: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 16,
}

const fieldLabel: CSSProperties = {
  display: 'block',
  marginBottom: 4,
}

const monoBox: CSSProperties = {
  fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, monospace',
  fontSize: 13,
  padding: '8px 10px',
  border: '1px solid var(--semi-color-border)',
  borderRadius: 8,
  wordBreak: 'break-all',
  background: 'var(--semi-color-fill-0)',
}

export function ConnectorsPanel() {
  const t = useT()
  const [status, setStatus] = useState<ConnectorStatus | null>(null)
  const [client, setClient] = useState<GoogleClientConfig | null>(null)
  const [clientId, setClientId] = useState('')
  const [clientSecret, setClientSecret] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [loaded, setLoaded] = useState(false)

  const load = useCallback(async () => {
    setError(null)
    try {
      const [list, cfg] = await Promise.all([
        connectors.list(),
        connectors.getGoogleClient(),
      ])
      const google =
        (list.connectors ?? []).find((c) => c.kind === 'google') ?? null
      setStatus(google)
      setClient(cfg)
      setClientId(cfg.clientId ?? '')
      setClientSecret(cfg.clientSecret ?? '')
    } catch (e) {
      setError(e instanceof Error ? e.message : t('connectors.loadFailed'))
    } finally {
      setLoaded(true)
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const google = params.get('google')
    if (!google) return
    if (google === 'linked') {
      setNotice(t('connectors.linked'))
    } else {
      setError(t('connectors.linkFailed'))
    }
    params.delete('google')
    const next = `${window.location.pathname}${params.toString() ? `?${params}` : ''}`
    window.history.replaceState({}, '', next)
    void load()
  }, [load, t])

  async function onSave() {
    setBusy('save')
    setError(null)
    setNotice(null)
    try {
      const cfg = await connectors.saveGoogleClient({
        clientId: clientId.trim(),
        clientSecret: clientSecret.trim(),
      })
      setClient(cfg)
      setClientId(cfg.clientId)
      setClientSecret(cfg.clientSecret)
      setNotice(t('connectors.saved'))
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('connectors.saveFailed'))
    } finally {
      setBusy(null)
    }
  }

  async function onConnect() {
    setBusy('connect')
    setError(null)
    try {
      const res = await connectors.connectGoogle()
      window.location.href = res.authorizeUrl
    } catch (e) {
      setError(e instanceof Error ? e.message : t('connectors.connectFailed'))
      setBusy(null)
    }
  }

  async function onDisconnect() {
    setBusy('disconnect')
    setError(null)
    try {
      await connectors.disconnectGoogle()
      setNotice(t('connectors.disconnected'))
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('connectors.disconnectFailed'))
    } finally {
      setBusy(null)
    }
  }

  async function onClearClient() {
    setBusy('clear')
    setError(null)
    try {
      await connectors.deleteGoogleClient()
      setClientId('')
      setClientSecret('')
      setNotice(t('connectors.clientCleared'))
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('connectors.clearFailed'))
    } finally {
      setBusy(null)
    }
  }

  if (!loaded) {
    return <Typography.Text type="tertiary">{t('connectors.loading')}</Typography.Text>
  }

  const hasClient = Boolean(status?.hasClient || client?.clientId)
  const connected = Boolean(status?.connected)

  return (
    <div style={sectionGap}>
      <div>
        <Typography.Title heading={4} style={{ margin: 0 }}>
          {t('connectors.title')}
        </Typography.Title>
        <Typography.Paragraph type="tertiary" style={{ margin: '8px 0 0' }}>
          {t('connectors.desc')}
        </Typography.Paragraph>
      </div>

      {error && (
        <Banner type="danger" description={error} closeIcon={null} />
      )}
      {notice && (
        <Banner type="success" description={notice} closeIcon={null} />
      )}

      <div style={sectionGap}>
        <Typography.Title heading={5} style={{ margin: 0 }}>
          {t('connectors.google')}
        </Typography.Title>
        <Typography.Paragraph type="tertiary" style={{ margin: 0 }}>
          {t('connectors.googleHelp')}
        </Typography.Paragraph>

        <div>
          <Typography.Text strong style={fieldLabel}>
            {t('connectors.callbackUrl')}
          </Typography.Text>
          <div style={monoBox}>{client?.callbackUrl ?? '—'}</div>
          <Typography.Text type="tertiary" size="small">
            {t('connectors.callbackHint')}
          </Typography.Text>
        </div>

        <div>
          <Typography.Text strong style={fieldLabel}>
            {t('connectors.clientId')}
          </Typography.Text>
          <Input
            value={clientId}
            onChange={setClientId}
            placeholder="xxxx.apps.googleusercontent.com"
          />
        </div>
        <div>
          <Typography.Text strong style={fieldLabel}>
            {t('connectors.clientSecret')}
          </Typography.Text>
          <Input
            mode="password"
            value={clientSecret}
            onChange={setClientSecret}
            placeholder={t('connectors.secretPlaceholder')}
          />
        </div>

        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
          <Button
            theme="solid"
            type="primary"
            loading={busy === 'save'}
            disabled={!!busy}
            onClick={() => void onSave()}
          >
            {t('connectors.saveClient')}
          </Button>
          {hasClient && (
            <Button
              type="danger"
              loading={busy === 'clear'}
              disabled={!!busy}
              onClick={() => void onClearClient()}
            >
              {t('connectors.clearClient')}
            </Button>
          )}
        </div>
      </div>

      <div
        style={{
          ...sectionGap,
          border: '1px solid var(--semi-color-border)',
          borderRadius: 8,
          padding: 12,
        }}
      >
        <Typography.Text strong>
          {connected
            ? t('connectors.statusConnected', { email: status?.email || '—' })
            : t('connectors.statusDisconnected')}
        </Typography.Text>
        {connected && status?.scopes ? (
          <Typography.Text type="tertiary" size="small">
            {t('connectors.scopes')}: {status.scopes}
          </Typography.Text>
        ) : null}
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
          <Button
            theme="solid"
            disabled={!hasClient || !!busy}
            loading={busy === 'connect'}
            onClick={() => void onConnect()}
          >
            {connected ? t('connectors.reconnect') : t('connectors.connect')}
          </Button>
          {connected && (
            <Button
              disabled={!!busy}
              loading={busy === 'disconnect'}
              onClick={() => void onDisconnect()}
            >
              {t('connectors.disconnect')}
            </Button>
          )}
        </div>
      </div>
    </div>
  )
}
