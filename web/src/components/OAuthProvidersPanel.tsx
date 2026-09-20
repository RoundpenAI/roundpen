import { useCallback, useEffect, useMemo, useState, type CSSProperties } from 'react'
import {
  Banner,
  Button,
  Input,
  Modal,
  Select,
  Switch,
  Typography,
} from '@douyinfe/semi-ui-19'
import { oauthAdmin, type OAuthProvider } from '../api'
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

const rowStyle: CSSProperties = {
  display: 'flex',
  flexWrap: 'wrap',
  alignItems: 'center',
  justifyContent: 'space-between',
  gap: 8,
  border: '1px solid var(--semi-color-border)',
  borderRadius: 8,
  padding: '8px 12px',
}

type FormState = {
  id: string
  kind: string
  scheme: string
  host: string
  label: string
  clientId: string
  clientSecret: string
  scopes: string
  authUrl: string
  tokenUrl: string
  apiUrl: string
  enabled: boolean
}

const blank: FormState = {
  id: '',
  kind: 'gitea',
  scheme: 'https',
  host: '',
  label: '',
  clientId: '',
  clientSecret: '',
  scopes: '',
  authUrl: '',
  tokenUrl: '',
  apiUrl: '',
  enabled: true,
}

export function OAuthProvidersPanel() {
  const t = useT()
  const [list, setList] = useState<OAuthProvider[]>([])
  const [form, setForm] = useState<FormState>(blank)
  const [editing, setEditing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const kindOptions = useMemo(
    () => [
      { value: 'github', label: t('git.provider.github') },
      { value: 'gitea', label: t('git.provider.gitea') },
    ],
    [t],
  )

  const load = useCallback(async () => {
    setError(null)
    try {
      const res = await oauthAdmin.list()
      setList(res.providers ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : t('oauthAdmin.loadFailed'))
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  function patch(part: Partial<FormState>) {
    setForm((prev) => ({ ...prev, ...part }))
  }

  function startEdit(p: OAuthProvider) {
    setEditing(true)
    setNotice(null)
    setError(null)
    setForm({
      id: p.id,
      kind: p.kind,
      scheme: p.scheme,
      host: p.host,
      label: p.label,
      clientId: p.clientId,
      clientSecret: p.clientSecret,
      scopes: p.scopes,
      authUrl: p.authUrl,
      tokenUrl: p.tokenUrl,
      apiUrl: p.apiUrl,
      enabled: p.enabled,
    })
  }

  async function save() {
    setSaving(true)
    setError(null)
    setNotice(null)
    try {
      await oauthAdmin.save({
        id: form.id.trim(),
        kind: form.kind,
        scheme: form.scheme,
        host: form.host.trim(),
        label: form.label.trim(),
        clientId: form.clientId.trim(),
        clientSecret: form.clientSecret,
        scopes: form.scopes.trim(),
        authUrl: form.authUrl.trim(),
        tokenUrl: form.tokenUrl.trim(),
        apiUrl: form.apiUrl.trim(),
        enabled: form.enabled,
      })
      setNotice(t('oauthAdmin.saved'))
      setForm(blank)
      setEditing(false)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('oauthAdmin.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  function remove(p: OAuthProvider) {
    Modal.confirm({
      title: t('oauthAdmin.remove'),
      content: t('oauthAdmin.confirmDelete'),
      onOk: async () => {
        setError(null)
        try {
          await oauthAdmin.remove(p.id)
          setNotice(t('oauthAdmin.removed'))
          if (form.id === p.id) {
            setForm(blank)
            setEditing(false)
          }
          await load()
        } catch (e) {
          setError(e instanceof Error ? e.message : t('oauthAdmin.deleteFailed'))
        }
      },
    })
  }

  const canSave =
    form.host.trim() !== '' &&
    form.clientId.trim() !== '' &&
    (editing || form.clientSecret.trim() !== '')

  return (
    <section style={sectionGap}>
      <Typography.Title heading={5} style={{ margin: 0 }}>
        {t('oauthAdmin.title')}
      </Typography.Title>
      <Typography.Text type="tertiary" size="small">
        {t('oauthAdmin.desc')}
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
      {notice ? (
        <div role="status">
          <Banner
            fullMode={false}
            type="success"
            description={notice}
            closeIcon={null}
          />
        </div>
      ) : null}
      {list.length > 0 ? (
        <ul
          style={{
            listStyle: 'none',
            margin: 0,
            padding: 0,
            display: 'flex',
            flexDirection: 'column',
            gap: 8,
          }}
        >
          {list.map((p) => (
            <li key={p.id} style={rowStyle}>
              <div style={{ minWidth: 0 }}>
                <Typography.Text
                  style={{
                    fontFamily: 'var(--semi-font-family-code)',
                    fontSize: 12,
                  }}
                >
                  {p.displayLabel} · {p.host}
                </Typography.Text>
                <Typography.Text
                  type="tertiary"
                  size="small"
                  style={{ display: 'block' }}
                >
                  {`${p.enabled ? t('oauthAdmin.enabled') : t('oauthAdmin.disabled')} · ${p.clientId}`}
                </Typography.Text>
                <Typography.Text type="tertiary" size="small">
                  {`${t('oauthAdmin.callback')}: ${p.callbackUrl}`}
                </Typography.Text>
              </div>
              <div style={{ display: 'flex', gap: 8 }}>
                <Button
                  type="tertiary"
                  size="small"
                  onClick={() => void navigator.clipboard?.writeText(p.callbackUrl)}
                >
                  {t('oauthAdmin.copy')}
                </Button>
                <Button
                  type="tertiary"
                  size="small"
                  onClick={() => startEdit(p)}
                >
                  {t('oauthAdmin.edit')}
                </Button>
                <Button type="danger" size="small" onClick={() => remove(p)}>
                  {t('oauthAdmin.remove')}
                </Button>
              </div>
            </li>
          ))}
        </ul>
      ) : (
        <Typography.Text type="tertiary" size="small">
          {t('oauthAdmin.empty')}
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
            {t('oauthAdmin.kind')}
          </Typography.Text>
          <Select
            value={form.kind}
            onChange={(v) => patch({ kind: String(v) })}
            optionList={kindOptions}
            style={{ width: '100%' }}
          />
        </div>
        <div>
          <Typography.Text size="small" type="tertiary" style={fieldLabel}>
            {t('oauthAdmin.scheme')}
          </Typography.Text>
          <Select
            value={form.scheme}
            onChange={(v) => patch({ scheme: String(v) })}
            optionList={[
              { value: 'https', label: 'https' },
              { value: 'http', label: 'http' },
            ]}
            style={{ width: '100%' }}
          />
        </div>
        <div>
          <Typography.Text size="small" type="tertiary" style={fieldLabel}>
            {t('oauthAdmin.host')}
          </Typography.Text>
          <Input
            aria-label={t('oauthAdmin.host')}
            value={form.host}
            onChange={(v) => patch({ host: v })}
            placeholder="git.eaxi.com"
          />
        </div>
        <div>
          <Typography.Text size="small" type="tertiary" style={fieldLabel}>
            {t('oauthAdmin.label')}
          </Typography.Text>
          <Input
            aria-label={t('oauthAdmin.label')}
            value={form.label}
            onChange={(v) => patch({ label: v })}
            placeholder="Gitea"
          />
        </div>
        <div>
          <Typography.Text size="small" type="tertiary" style={fieldLabel}>
            {t('oauthAdmin.clientId')}
          </Typography.Text>
          <Input
            aria-label={t('oauthAdmin.clientId')}
            value={form.clientId}
            onChange={(v) => patch({ clientId: v })}
          />
        </div>
        <div>
          <Typography.Text size="small" type="tertiary" style={fieldLabel}>
            {t('oauthAdmin.clientSecret')}
          </Typography.Text>
          <Input
            aria-label={t('oauthAdmin.clientSecret')}
            mode="password"
            autoComplete="new-password"
            value={form.clientSecret}
            onChange={(v) => patch({ clientSecret: v })}
            placeholder={editing ? t('oauthAdmin.secretHint') : ''}
          />
        </div>
        <div>
          <Typography.Text size="small" type="tertiary" style={fieldLabel}>
            {t('oauthAdmin.scopes')}
          </Typography.Text>
          <Input
            aria-label={t('oauthAdmin.scopes')}
            value={form.scopes}
            onChange={(v) => patch({ scopes: v })}
            placeholder="read:user user:email repo"
          />
        </div>
      </div>

      <details>
        <summary style={{ cursor: 'pointer' }}>
          <Typography.Text type="tertiary" size="small">
            {t('oauthAdmin.advanced')}
          </Typography.Text>
        </summary>
        <div
          style={{
            display: 'grid',
            gap: 12,
            marginTop: 12,
            gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))',
          }}
        >
          <div>
            <Typography.Text size="small" type="tertiary" style={fieldLabel}>
              {t('oauthAdmin.authUrl')}
            </Typography.Text>
            <Input
              value={form.authUrl}
              onChange={(v) => patch({ authUrl: v })}
            />
          </div>
          <div>
            <Typography.Text size="small" type="tertiary" style={fieldLabel}>
              {t('oauthAdmin.tokenUrl')}
            </Typography.Text>
            <Input
              value={form.tokenUrl}
              onChange={(v) => patch({ tokenUrl: v })}
            />
          </div>
          <div>
            <Typography.Text size="small" type="tertiary" style={fieldLabel}>
              {t('oauthAdmin.apiUrl')}
            </Typography.Text>
            <Input value={form.apiUrl} onChange={(v) => patch({ apiUrl: v })} />
          </div>
        </div>
      </details>

      <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
        <Button
          theme="solid"
          type="primary"
          size="small"
          loading={saving}
          disabled={!canSave}
          onClick={() => void save()}
        >
          {editing ? t('oauthAdmin.saveEdit') : t('oauthAdmin.new')}
        </Button>
        {editing ? (
          <Button
            size="small"
            onClick={() => {
              setForm(blank)
              setEditing(false)
            }}
          >
            {t('oauthAdmin.cancel')}
          </Button>
        ) : null}
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <Switch
            checked={form.enabled}
            onChange={(v: boolean) => patch({ enabled: v })}
          />
          <Typography.Text size="small">{t('oauthAdmin.enabled')}</Typography.Text>
        </div>
      </div>
    </section>
  )
}
