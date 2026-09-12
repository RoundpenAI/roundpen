import { useState, type FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { Banner, Button, Input, Typography } from '@douyinfe/semi-ui-19'
import { doLogin, useAuth } from '../auth'

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
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

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
      </div>
    </div>
  )
}
