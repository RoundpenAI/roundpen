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
import { useNavigate } from 'react-router-dom'
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
  kind: 'github',
  scheme: 'https',
  host: 'github.com',
  label: '',
  clientId: '',
  clientSecret: '',
  scopes: '',
  authUrl: '',
  tokenUrl: '',
  apiUrl: '',
  enabled: true,
}

const GITHUB_APP_ERR: Record<string, 'oauthAdmin.githubDenied' | 'oauthAdmin.githubState' | 'oauthAdmin.githubSession' | 'oauthAdmin.githubFailed'> = {
  denied: 'oauthAdmin.githubDenied',
  state: 'oauthAdmin.githubState',
  session: 'oauthAdmin.githubSession',
  failed: 'oauthAdmin.githubFailed',
}

function isGitHubDotCom(kind: string, host: string): boolean {
  return kind === 'github' && (host.trim() === '' || host.trim().toLowerCase() === 'github.com')
}

function githubCallbackURL(): string {
  return `${window.location.origin}/v1/auth/oauth/github/callback`
}

export function OAuthProvidersPanel() {
  const t = useT()
  const navigate = useNavigate()
  const [list, setList] = useState<OAuthProvider[]>([])
  const [selfHostedGit, setSelfHostedGit] = useState(false)
  const [form, setForm] = useState<FormState>(blank)
  const [editing, setEditing] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [creatingApp, setCreatingApp] = useState(false)

  const kindOptions = useMemo(() => {
    const out = [{ value: 'github', label: t('git.provider.github') }]
    if (selfHostedGit) {
      out.push(
        { value: 'gitea', label: t('git.provider.gitea') },
        { value: 'gitlab', label: t('git.provider.gitlab') },
      )
    }
    return out
  }, [selfHostedGit, t])

  const load = useCallback(async () => {
    setError(null)
    try {
      const res = await oauthAdmin.list()
      setList(res.providers ?? [])
      setSelfHostedGit(Boolean(res.features?.selfHostedGit))
    } catch (e) {
      setError(e instanceof Error ? e.message : t('oauthAdmin.loadFailed'))
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    if (params.get('github_app') === '1') {
      setNotice(t('oauthAdmin.githubCreated'))
      void load()
    }
    const appErr = params.get('github_app_error')
    if (appErr) {
      const key = GITHUB_APP_ERR[appErr]
      setError(t(key ?? 'oauthAdmin.githubFailed'))
    }
    if (params.get('github_app') || appErr) {
      params.delete('github_app')
      params.delete('github_app_error')
      const next = `${window.location.pathname}${params.toString() ? `?${params}` : ''}`
      window.history.replaceState({}, '', next)
    }
  }, [load, t])

  function patch(part: Partial<FormState>) {
    setForm((prev) => ({ ...prev, ...part }))
  }

  function onKindChange(kind: string) {
    if (kind === 'github') {
      patch({
        kind,
        scheme: 'https',
        host: 'github.com',
        scopes: '',
        label: form.label.trim() === '' || form.label === 'Gitea' || form.label === 'GitLab' ? 'GitHub' : form.label,
      })
      return
    }
    patch({
      kind,
      host: form.host === 'github.com' ? '' : form.host,
      scopes: '',
      label: kind === 'gitlab' ? 'GitLab' : kind === 'gitea' ? 'Gitea' : form.label,
    })
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
        host: form.kind === 'github' ? form.host.trim() || 'github.com' : form.host.trim(),
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

  async function startGitHubApp() {
    setCreatingApp(true)
    setError(null)
    setNotice(null)
    try {
      const res = await oauthAdmin.githubManifest()
      const formEl = document.createElement('form')
      formEl.method = 'POST'
      formEl.action = res.actionUrl
      const input = document.createElement('input')
      input.type = 'hidden'
      input.name = 'manifest'
      input.value = res.manifest
      formEl.appendChild(input)
      document.body.appendChild(formEl)
      formEl.submit()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('oauthAdmin.githubOneClickFailed'))
      setCreatingApp(false)
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

  const githubDotCom = isGitHubDotCom(form.kind, form.host)
  const canSave =
    form.clientId.trim() !== '' &&
    (editing || form.clientSecret.trim() !== '') &&
    (form.kind === 'github' || form.host.trim() !== '')

  return (
    <section style={sectionGap}>
      <Typography.Title heading={5} style={{ margin: 0 }}>
        {t('oauthAdmin.title')}
      </Typography.Title>
      <Typography.Text type="tertiary" size="small">
        {t('oauthAdmin.desc')}
      </Typography.Text>
      {!selfHostedGit ? (
        <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
          <Typography.Text type="tertiary" size="small">
            {t('oauthAdmin.selfHostedHint')}
          </Typography.Text>
          <Button size="small" onClick={() => navigate('/admin/settings/plugins')}>
            {t('oauthAdmin.openPlugins')}
          </Button>
        </div>
      ) : null}
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

      <div
        style={{
          display: 'flex',
          flexDirection: 'column',
          gap: 8,
          border: '1px solid var(--semi-color-border)',
          borderRadius: 8,
          padding: 12,
        }}
      >
        <Typography.Text size="small">{t('oauthAdmin.githubOneClickHint')}</Typography.Text>
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
          <Button
            theme="solid"
            type="primary"
            size="small"
            loading={creatingApp}
            onClick={() => void startGitHubApp()}
          >
            {t('oauthAdmin.githubOneClick')}
          </Button>
        </div>
      </div>

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

      <Typography.Text type="tertiary" size="small">
        {t('oauthAdmin.githubManual')}
      </Typography.Text>

      <div
        style={{
          display: 'grid',
          gap: 12,
          gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))',
        }}
      >
        {selfHostedGit ? (
        <div>
          <Typography.Text size="small" type="tertiary" style={fieldLabel}>
            {t('oauthAdmin.kind')}
          </Typography.Text>
          <Select
            aria-label={t('oauthAdmin.kind')}
            value={form.kind}
            onChange={(v) => onKindChange(String(v))}
            optionList={kindOptions}
            style={{ width: '100%' }}
          />
        </div>
        ) : null}
        {selfHostedGit && form.kind !== 'github' ? (
          <>
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
                placeholder="git.example.com"
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
          </>
        ) : null}
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
        {selfHostedGit && form.kind !== 'github' ? (
          <div>
            <Typography.Text size="small" type="tertiary" style={fieldLabel}>
              {t('oauthAdmin.scopes')}
            </Typography.Text>
            <Input
              aria-label={t('oauthAdmin.scopes')}
              value={form.scopes}
              onChange={(v) => patch({ scopes: v })}
              placeholder={form.kind === 'gitlab' ? 'read_user api' : 'read:user user:email repo'}
            />
          </div>
        ) : null}
      </div>

      {githubDotCom ? (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
          <Typography.Text type="tertiary" size="small">
            {t('oauthAdmin.githubCallbackHint')}
          </Typography.Text>
          <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
            <Typography.Text
              style={{
                fontFamily: 'var(--semi-font-family-code)',
                fontSize: 12,
                wordBreak: 'break-all',
              }}
            >
              {githubCallbackURL()}
            </Typography.Text>
            <Button
              type="tertiary"
              size="small"
              onClick={() => void navigator.clipboard?.writeText(githubCallbackURL())}
            >
              {t('oauthAdmin.copy')}
            </Button>
            <Button
              type="tertiary"
              size="small"
              onClick={() =>
                window.open('https://github.com/settings/applications/new', '_blank', 'noopener')
              }
            >
              {t('oauthAdmin.githubCreateOauthApp')}
            </Button>
          </div>
        </div>
      ) : null}

      {selfHostedGit && form.kind === 'github' ? (
        <details>
          <summary style={{ cursor: 'pointer' }}>
            <Typography.Text type="tertiary" size="small">
              {t('oauthAdmin.enterprise')}
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
                placeholder="github.com"
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
                placeholder="GitHub"
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
        </details>
      ) : null}

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
