import { useEffect, useState, type FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { Banner, Button, Input, Typography } from '@douyinfe/semi-ui-19'
import { oauthProviders, type OAuthProviderOption } from '../api'
import { doLogin, useAuth } from '../auth'

// The OAuth callback bounces back here with ?oauth_error=<code>.
const OAUTH_ERRORS: Record<string, string> = {
  denied: 'Authorization was cancelled.',
  state: 'This sign-in link expired or was already used. Please try again.',
  session: 'This authorization was started in a different browser session.',
  registration_disabled:
    'No Roundpen account matches this identity. Sign in with your password, then link it under Settings → Linked accounts.',
  already_linked: 'That account is already linked to another Roundpen user.',
  provider: 'This sign-in provider is not available.',
  failed: 'Sign-in failed. Please try again.',
}

export function LoginPage() {
  const auth = useAuth()
  const navigate = useNavigate()
  const location = useLocation()
  const from =
    (location.state as { from?: string } | null)?.from &&
    (location.state as { from: string }).from !== '/login'
      ? (location.state as { from: string }).from
      : '/'

  const [user, setUser] = useState('admin')
  const [password, setPassword] = useState('')
  const [error, setError] = useState<string | null>(() => {
    const code = new URLSearchParams(location.search).get('oauth_error')
    return code ? (OAUTH_ERRORS[code] ?? OAUTH_ERRORS.failed) : null
  })
  const [busy, setBusy] = useState(false)
  const [providers, setProviders] = useState<OAuthProviderOption[]>([])

  useEffect(() => {
    let alive = true
    oauthProviders
      .list()
      .then((res) => {
        if (alive) setProviders(res.providers ?? [])
      })
      .catch(() => {
        /* no federated providers is a valid state */
      })
    return () => {
      alive = false
    }
  }, [])

  if (auth.status === 'ok') {
    return <Navigate to={from} replace />
  }

  async function onSubmit(e: FormEvent) {
    e.preventDefault()
    setBusy(true)
    setError(null)
    try {
      await doLogin(user, password)
      navigate(from, { replace: true })
    } catch (err) {
      setError(err instanceof Error ? err.message : 'login failed')
    } finally {
      setBusy(false)
    }
  }

  return (
    <div
      style={{
        minHeight: '100%',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: 24,
        background: 'var(--semi-color-bg-0)',
      }}
    >
      <div style={{ width: '100%', maxWidth: 360 }}>
        <Typography.Title heading={2} style={{ textAlign: 'center', margin: 0 }}>
          Roundpen
        </Typography.Title>
        <Typography.Text
          type="tertiary"
          style={{ display: 'block', textAlign: 'center', marginBottom: 24 }}
        >
          Sign in to your agent sandbox
        </Typography.Text>
        <form
          onSubmit={(e) => void onSubmit(e)}
          style={{ display: 'flex', flexDirection: 'column', gap: 16 }}
        >
          <div>
            <Typography.Text
              type="tertiary"
              size="small"
              style={{ display: 'block', marginBottom: 4 }}
            >
              Username or email
            </Typography.Text>
            <Input
              value={user}
              onChange={setUser}
              autoComplete="username"
              autoCapitalize="none"
              autoCorrect="off"
              aria-label="Username or email"
              required
            />
          </div>
          <div>
            <Typography.Text
              type="tertiary"
              size="small"
              style={{ display: 'block', marginBottom: 4 }}
            >
              Password
            </Typography.Text>
            <Input
              mode="password"
              value={password}
              onChange={setPassword}
              autoComplete="current-password"
              aria-label="Password"
              required
            />
          </div>
          {error && (
            <div role="alert">
              <Banner
                fullMode={false}
                type="danger"
                description={error}
                closeIcon={null}
              />
            </div>
          )}
          <Button
            htmlType="submit"
            theme="solid"
            type="primary"
            block
            loading={busy}
          >
            Sign in
          </Button>
        </form>
        {providers.length > 0 && (
          <div
            style={{
              display: 'flex',
              flexDirection: 'column',
              gap: 12,
              marginTop: 24,
            }}
          >
            <Typography.Text
              type="tertiary"
              size="small"
              style={{ textAlign: 'center' }}
            >
              or continue with
            </Typography.Text>
            {providers.map((p) => (
              <Button
                key={p.id}
                block
                onClick={() =>
                  window.location.assign(oauthProviders.startUrl(p.id, from))
                }
              >
                {`Sign in with ${p.label}`}
              </Button>
            ))}
          </div>
        )}
      </div>
    </div>
  )
}
