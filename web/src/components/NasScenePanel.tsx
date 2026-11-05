import { useCallback, useEffect, useState } from 'react'
import { Banner, Button, Input, Switch, Typography } from '@douyinfe/semi-ui-19'
import {
  downloadBackends,
  nasScene,
  type DownloadBackend,
  type NasSceneStatus,
} from '../api'
import { useT, type MessageKey } from '../i18n'
import { Loading } from './Loading'

export function NasScenePanel() {
  const t = useT()
  const [status, setStatus] = useState<NasSceneStatus | null>(null)
  const [backends, setBackends] = useState<DownloadBackend[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [name, setName] = useState('qBittorrent')
  const [baseUrl, setBaseUrl] = useState('http://127.0.0.1:8080')
  const [username, setUsername] = useState('admin')
  const [password, setPassword] = useState('')
  const [savePath, setSavePath] = useState('')

  const load = useCallback(async () => {
    setError(null)
    try {
      const [scene, dl] = await Promise.all([
        nasScene.get(),
        downloadBackends.list(),
      ])
      setStatus(scene)
      setBackends(dl.backends ?? [])
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

  async function addBackend() {
    setBusy(true)
    setError(null)
    try {
      await downloadBackends.create({
        name: name.trim() || 'qBittorrent',
        type: 'qbittorrent',
        baseUrl: baseUrl.trim(),
        username: username.trim(),
        password: password || undefined,
        defaultSavePath: savePath.trim() || undefined,
      })
      setPassword('')
      const dl = await downloadBackends.list()
      setBackends(dl.backends ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : t('nasScene.dlSaveFailed'))
    } finally {
      setBusy(false)
    }
  }

  async function removeBackend(id: string) {
    setBusy(true)
    setError(null)
    try {
      await downloadBackends.remove(id)
      setBackends((prev) => prev.filter((b) => b.id !== id))
    } catch (e) {
      setError(e instanceof Error ? e.message : t('nasScene.dlSaveFailed'))
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

      <div style={{ borderTop: '1px solid var(--semi-color-border)', paddingTop: 16 }}>
        <Typography.Title heading={6} style={{ margin: '0 0 8px' }}>
          {t('nasScene.dlTitle')}
        </Typography.Title>
        <Typography.Text type="tertiary" size="small">
          {t('nasScene.dlBlurb')}
        </Typography.Text>

        {backends.length > 0 && (
          <ul style={{ margin: '12px 0', paddingLeft: 18 }}>
            {backends.map((b) => (
              <li key={b.id} style={{ marginBottom: 6 }}>
                <Typography.Text>
                  {b.name} ({b.type}) — {b.baseUrl}
                </Typography.Text>{' '}
                <Button
                  size="small"
                  type="danger"
                  theme="borderless"
                  disabled={busy}
                  onClick={() => void removeBackend(b.id)}
                >
                  {t('nasScene.dlRemove')}
                </Button>
              </li>
            ))}
          </ul>
        )}

        <div style={{ display: 'grid', gap: 8, marginTop: 12, maxWidth: 480 }}>
          <Input
            placeholder={t('nasScene.dlName')}
            value={name}
            onChange={setName}
          />
          <Input
            placeholder={t('nasScene.dlBaseUrl')}
            value={baseUrl}
            onChange={setBaseUrl}
          />
          <Input
            placeholder={t('nasScene.dlUser')}
            value={username}
            onChange={setUsername}
          />
          <Input
            mode="password"
            placeholder={t('nasScene.dlPassword')}
            value={password}
            onChange={setPassword}
          />
          <Input
            placeholder={t('nasScene.dlSavePath')}
            value={savePath}
            onChange={setSavePath}
          />
          <Button
            theme="solid"
            disabled={busy || !baseUrl.trim()}
            onClick={() => void addBackend()}
          >
            {t('nasScene.dlAdd')}
          </Button>
        </div>
      </div>
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
