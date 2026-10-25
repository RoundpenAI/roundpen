import { useEffect, useState, type FormEvent } from 'react'
import { Navigate, useLocation, useNavigate } from 'react-router-dom'
import { Banner, Button, Input, Typography } from '@douyinfe/semi-ui-19'
import { auth as authApi } from '../api'
import { isFnOSApp } from '../api/client'
import { doLogin, useAuth } from '../auth'
import { useT, type MessageKey } from '../i18n'
import { LocaleToggle } from '../components/LocaleToggle'
import { ThemeToggle } from '../components/ThemeToggle'

// The OAuth callback bounces back here with ?oauth_error=<code>.
const OAUTH_ERROR_KEYS: Record<string, MessageKey> = {
  denied: 'login.oauth.denied',
  state: 'login.oauth.state',
  session: 'login.oauth.session',
  registration_disabled: 'login.oauth.registrationDisabled',
  already_linked: 'login.oauth.alreadyLinked',
  provider: 'login.oauth.provider',
  failed: 'login.oauth.failed',
}

export function LoginPage() {
  const t = useT()
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
  const [errorKey, setErrorKey] = useState<MessageKey | null>(() => {
    const code = new URLSearchParams(location.search).get('oauth_error')
    if (!code) return null
    return OAUTH_ERROR_KEYS[code] ?? OAUTH_ERROR_KEYS.failed
  })
  const [errorRaw, setErrorRaw] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [defaultCreds, setDefaultCreds] = useState<{
    username: string
    password: string
  } | null>(null)
  const [fnosGateway, setFnosGateway] = useState(isFnOSApp())

  useEffect(() => {
    let alive = true
    authApi
      .bootstrapHint()
      .then((hint) => {
        if (!alive) return
        if (hint.authMode === 'fnos-gateway') setFnosGateway(true)
        if (!hint.username || !hint.password) return
        setDefaultCreds({ username: hint.username, password: hint.password })
        setUser(hint.username)
        setPassword(hint.password)
      })
      .catch(() => {
        /* no packaged default is fine */
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
    setErrorKey(null)
    setErrorRaw(null)
    try {
      await doLogin(user, password)
      navigate(from, { replace: true })
    } catch (err) {
      setErrorRaw(err instanceof Error ? err.message : t('login.failed'))
    } finally {
      setBusy(false)
    }
  }

  const errorText = errorKey ? t(errorKey) : errorRaw

  return (
    <div
      style={{
        minHeight: '100%',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding:
          'max(24px, env(safe-area-inset-top, 0px)) max(24px, env(safe-area-inset-right, 0px)) max(24px, env(safe-area-inset-bottom, 0px)) max(24px, env(safe-area-inset-left, 0px))',
        background: 'var(--semi-color-bg-0)',
        position: 'relative',
      }}
    >
      <div
        style={{
          position: 'absolute',
          top: 'max(16px, env(safe-area-inset-top, 0px))',
          right: 'max(16px, env(safe-area-inset-right, 0px))',
          display: 'flex',
          alignItems: 'center',
          gap: 8,
        }}
      >
        <LocaleToggle />
        <ThemeToggle />
      </div>
      <div style={{ width: '100%', maxWidth: 360 }}>
        <Typography.Title heading={2} style={{ textAlign: 'center', margin: 0 }}>
          Roundpen
        </Typography.Title>
        <Typography.Text
          type="tertiary"
          style={{ display: 'block', textAlign: 'center', marginBottom: 24 }}
        >
          {t('login.subtitle')}
        </Typography.Text>
        {defaultCreds && (
          <Banner
            fullMode={false}
            type="info"
            closeIcon={null}
            style={{ marginBottom: 16 }}
            description={t(
              fnosGateway ? 'login.defaultCredsFnos' : 'login.defaultCreds',
              {
                user: defaultCreds.username,
                password: defaultCreds.password,
              },
            )}
          />
        )}
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
              {t('login.userLabel')}
            </Typography.Text>
            <Input
              value={user}
              onChange={setUser}
              autoComplete="username"
              autoCapitalize="none"
              autoCorrect="off"
              aria-label={t('login.userLabel')}
              required
            />
          </div>
          <div>
            <Typography.Text
              type="tertiary"
              size="small"
              style={{ display: 'block', marginBottom: 4 }}
            >
              {t('login.passwordLabel')}
            </Typography.Text>
            <Input
              mode="password"
              value={password}
              onChange={setPassword}
              autoComplete="current-password"
              aria-label={t('login.passwordLabel')}
              required
            />
          </div>
          {errorText && (
            <div role="alert">
              <Banner
                fullMode={false}
                type="danger"
                description={errorText}
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
            {t('login.submit')}
          </Button>
        </form>
      </div>
    </div>
  )
}
