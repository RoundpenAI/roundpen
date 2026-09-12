import { useCallback, useEffect, useMemo, useState, type CSSProperties } from 'react'
import {
  Banner,
  Button,
  Input,
  Select,
  Typography,
} from '@douyinfe/semi-ui-19'
import { gitCredentials, type GitCredential } from '../api'
import { useT } from '../i18n'
import type { MessageKey } from '../i18n'

const PROVIDER_KEYS = [
  { value: 'gitea', labelKey: 'git.provider.gitea' as MessageKey },
  { value: 'github', labelKey: 'git.provider.github' as MessageKey },
  { value: 'gitlab', labelKey: 'git.provider.gitlab' as MessageKey },
  { value: 'generic', labelKey: 'git.provider.generic' as MessageKey },
] as const

const sectionGap: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 16,
}

const fieldLabel: CSSProperties = {
  display: 'block',
  marginBottom: 4,
}

export function GitCredentialsPanel() {
  const t = useT()
  const [list, setList] = useState<GitCredential[]>([])
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [host, setHost] = useState('git.eaxi.com')
  const [provider, setProvider] = useState('gitea')
  const [username, setUsername] = useState('')
  const [token, setToken] = useState('')

  const providerOptions = useMemo(
    () =>
      PROVIDER_KEYS.map((p) => ({
        value: p.value,
        label: t(p.labelKey),
      })),
    [t],
  )

  const load = useCallback(async () => {
    setError(null)
    try {
      const res = await gitCredentials.list()
      setList(res.credentials ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : t('git.loadFailed'))
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  async function onSave() {
    setSaving(true)
    setError(null)
    try {
      await gitCredentials.upsert({
        provider,
        host,
        username,
        token,
      })
      setToken('')
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('git.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  async function onDelete(id: string) {
    setError(null)
    try {
      await gitCredentials.remove(id)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('git.deleteFailed'))
    }
  }

  return (
    <section style={sectionGap}>
      <Typography.Title heading={5} style={{ margin: 0 }}>
        {t('git.title')}
      </Typography.Title>
      <Typography.Text type="tertiary" size="small">
        {t('git.desc')}
      </Typography.Text>
      {error ? (
        <div role="alert">
          <Banner
            fullMode={false}
            type="danger"
            description={error}
            closeIcon={null}
          />
        </div>
      ) : null}
      {list.length > 0 ? (
        <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 8 }}>
          {list.map((c) => (
            <li
              key={c.id}
              style={{
                display: 'flex',
                flexWrap: 'wrap',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: 8,
                border: '1px solid var(--semi-color-border)',
                borderRadius: 8,
                padding: '8px 12px',
              }}
            >
              <div style={{ minWidth: 0 }}>
                <Typography.Text
                  style={{
                    fontFamily: 'var(--semi-font-family-code)',
                    fontSize: 12,
                  }}
                >
                  {c.host}
                </Typography.Text>
                <Typography.Text
                  type="tertiary"
                  size="small"
                  style={{ display: 'block' }}
                >
                  {c.provider}
                  {c.username ? ` · ${c.username}` : ''}
                  {c.hasToken ? ` · ${t('git.tokenSaved')}` : ''}
                </Typography.Text>
              </div>
              <Button type="tertiary" size="small" onClick={() => void onDelete(c.id)}>
                {t('git.remove')}
              </Button>
            </li>
          ))}
        </ul>
      ) : (
        <Typography.Text type="tertiary" size="small">
          {t('git.empty')}
        </Typography.Text>
      )}
      <div
        style={{
          display: 'grid',
          gap: 12,
          gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))',
        }}
      >
        <div>
          <Typography.Text size="small" type="tertiary" style={fieldLabel}>
            {t('git.provider')}
          </Typography.Text>
          <Select
            value={provider}
            onChange={(v) => setProvider(String(v))}
            optionList={providerOptions}
            style={{ width: '100%' }}
          />
        </div>
        <div>
          <Typography.Text size="small" type="tertiary" style={fieldLabel}>
            {t('git.host')}
          </Typography.Text>
          <Input
            value={host}
            onChange={setHost}
            placeholder="git.eaxi.com"
          />
        </div>
        <div>
          <Typography.Text size="small" type="tertiary" style={fieldLabel}>
            {t('git.username')}
          </Typography.Text>
          <Input
            value={username}
            onChange={setUsername}
            placeholder="git"
          />
        </div>
        <div>
          <Typography.Text size="small" type="tertiary" style={fieldLabel}>
            {t('git.token')}
          </Typography.Text>
          <Input
            mode="password"
            autoComplete="new-password"
            value={token}
            onChange={setToken}
            placeholder={t('git.tokenPlaceholder')}
          />
        </div>
      </div>
      <div>
        <Button
          theme="solid"
          type="primary"
          size="small"
          loading={saving}
          disabled={!host.trim() || !token.trim()}
          onClick={() => void onSave()}
        >
          {t('git.save')}
        </Button>
      </div>
    </section>
  )
}
