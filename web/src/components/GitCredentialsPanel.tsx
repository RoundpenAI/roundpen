import { useCallback, useEffect, useState, type CSSProperties } from 'react'
import {
  Banner,
  Button,
  Input,
  Select,
  Typography,
} from '@douyinfe/semi-ui-19'
import { gitCredentials, type GitCredential } from '../api'

const PROVIDERS = [
  { value: 'gitea', label: 'Gitea' },
  { value: 'github', label: 'GitHub' },
  { value: 'gitlab', label: 'GitLab' },
  { value: 'generic', label: 'Other git host' },
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

const codeStyle: CSSProperties = {
  fontFamily: 'var(--semi-font-family-code)',
  fontSize: '0.75rem',
}

export function GitCredentialsPanel() {
  const [list, setList] = useState<GitCredential[]>([])
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [host, setHost] = useState('git.eaxi.com')
  const [provider, setProvider] = useState('gitea')
  const [username, setUsername] = useState('')
  const [token, setToken] = useState('')

  const load = useCallback(async () => {
    setError(null)
    try {
      const res = await gitCredentials.list()
      setList(res.credentials ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load git credentials')
    }
  }, [])

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
      setError(e instanceof Error ? e.message : 'save failed')
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
      setError(e instanceof Error ? e.message : 'delete failed')
    }
  }

  return (
    <section style={sectionGap}>
      <Typography.Title heading={5} style={{ margin: 0 }}>
        Git personal tokens
      </Typography.Title>
      <Typography.Text type="tertiary" size="small">
        One personal token per git host. Roundpen stores it for your account and
        injects it into the Cloud Agent workspace for <code style={codeStyle}>git</code>,
        and later <code style={codeStyle}>tea</code> /{' '}
        <code style={codeStyle}>gh</code> /{' '}
        <code style={codeStyle}>glab</code> (issues, PRs). Tokens never go into
        the image, and we do not copy SSH keys from this machine.
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
                  {c.hasToken ? ' · personal token saved' : ''}
                </Typography.Text>
              </div>
              <Button type="tertiary" size="small" onClick={() => void onDelete(c.id)}>
                Remove
              </Button>
            </li>
          ))}
        </ul>
      ) : (
        <Typography.Text type="tertiary" size="small">
          No personal tokens yet. Add a host below.
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
            Provider
          </Typography.Text>
          <Select
            value={provider}
            onChange={(v) => setProvider(String(v))}
            optionList={[...PROVIDERS]}
            style={{ width: '100%' }}
          />
        </div>
        <div>
          <Typography.Text size="small" type="tertiary" style={fieldLabel}>
            Host
          </Typography.Text>
          <Input
            value={host}
            onChange={setHost}
            placeholder="git.eaxi.com"
          />
        </div>
        <div>
          <Typography.Text size="small" type="tertiary" style={fieldLabel}>
            Username (optional)
          </Typography.Text>
          <Input
            value={username}
            onChange={setUsername}
            placeholder="git"
          />
        </div>
        <div>
          <Typography.Text size="small" type="tertiary" style={fieldLabel}>
            Personal token
          </Typography.Text>
          <Input
            mode="password"
            autoComplete="new-password"
            value={token}
            onChange={setToken}
            placeholder="PAT with repo + issues/PR scope"
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
          Save personal token
        </Button>
      </div>
    </section>
  )
}
