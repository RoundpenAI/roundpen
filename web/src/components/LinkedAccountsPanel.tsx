import { useCallback, useEffect, useState, type CSSProperties } from 'react'
import { useNavigate } from 'react-router-dom'
import { Banner, Button, Modal, Typography } from '@douyinfe/semi-ui-19'
import {
  identities,
  oauthProviders,
  type Identity,
  type OAuthProviderOption,
} from '../api'
import { useAuth } from '../auth'
import { useT } from '../i18n'

const sectionGap: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 16,
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

function expiryText(iso: string | undefined, noExpiry: string): string {
  if (!iso) return noExpiry
  const at = new Date(iso)
  if (Number.isNaN(at.getTime())) return noExpiry
  return at.toLocaleString()
}

type AccountRow = {
  key: string
  label: string
  host: string
  identity?: Identity
  provider?: OAuthProviderOption
}

export function LinkedAccountsPanel() {
  const t = useT()
  const auth = useAuth()
  const navigate = useNavigate()
  const isAdmin = auth.status === 'ok' && auth.user.role === 'admin'
  const [list, setList] = useState<Identity[]>([])
  const [providers, setProviders] = useState<OAuthProviderOption[]>([])
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [loaded, setLoaded] = useState(false)

  const load = useCallback(async () => {
    setError(null)
    try {
      const [linked, available] = await Promise.all([
        identities.list(),
        oauthProviders.list(),
      ])
      setList(linked.identities ?? [])
      setProviders(available.providers ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : t('accounts.loadFailed'))
    } finally {
      setLoaded(true)
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  // The OAuth callback lands back here with a result in the query string.
  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const linked = params.get('oauth_linked')
    const failed = params.get('oauth_error')
    if (linked) setNotice(t('accounts.linked'))
    if (failed) {
      setError(
        failed === 'already_linked'
          ? t('accounts.errLinked')
          : failed === 'session'
            ? t('accounts.errSession')
            : t('accounts.errFailed'),
      )
    }
    if (linked || failed) {
      window.history.replaceState({}, '', '/settings/accounts')
    }
  }, [t])

  async function connect(providerId: string) {
    setBusy(providerId)
    setError(null)
    setNotice(null)
    try {
      const res = await identities.linkUrl(providerId)
      window.location.assign(res.authorizeUrl)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('accounts.connectFailed'))
      setBusy(null)
    }
  }

  function unlink(id: string) {
    Modal.confirm({
      title: t('accounts.remove'),
      content: t('accounts.confirmUnlink'),
      onOk: async () => {
        setError(null)
        try {
          await identities.remove(id)
          setNotice(t('accounts.unlinked'))
          await load()
        } catch (e) {
          setError(e instanceof Error ? e.message : t('accounts.unlinkFailed'))
        }
      },
    })
  }

  // One row per provider: the bound account, or an entry to bind one. A
  // disabled provider keeps its row (and its unbind entry) while it still
  // holds a binding.
  const linkedRows: AccountRow[] = list.map((identity) => ({
    key: identity.providerId,
    label: identity.providerLabel,
    host: identity.providerHost,
    identity,
    provider: providers.find((p) => p.id === identity.providerId),
  }))
  const unlinkedRows: AccountRow[] = providers
    .filter((p) => !list.some((i) => i.providerId === p.id))
    .map((p) => ({ key: p.id, label: p.label, host: p.host, provider: p }))
  const rows = [...linkedRows, ...unlinkedRows]

  return (
    <section style={sectionGap}>
      <Typography.Title heading={5} style={{ margin: 0 }}>
        {t('accounts.title')}
      </Typography.Title>
      <Typography.Text type="tertiary" size="small">
        {t('accounts.desc')}
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
      {rows.length > 0 ? (
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
          {rows.map((row) => {
            const identity = row.identity
            return (
              <li key={row.key} style={rowStyle}>
                <div style={{ minWidth: 0 }}>
                  <Typography.Text
                    style={{
                      fontFamily: 'var(--semi-font-family-code)',
                      fontSize: 12,
                    }}
                  >
                    {row.label} · {row.host}
                  </Typography.Text>
                  {identity ? (
                    <>
                      <Typography.Text
                        type="tertiary"
                        size="small"
                        style={{ display: 'block' }}
                      >
                        {identity.login}
                        {identity.email ? ` · ${identity.email}` : ''}
                      </Typography.Text>
                      <Typography.Text type="tertiary" size="small">
                        {`${t('accounts.expires')}: ${expiryText(identity.expiresAt, t('accounts.noExpiry'))}`}
                      </Typography.Text>
                    </>
                  ) : (
                    <Typography.Text
                      type="tertiary"
                      size="small"
                      style={{ display: 'block' }}
                    >
                      {t('accounts.notLinked')}
                    </Typography.Text>
                  )}
                </div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                  {identity ? (
                    <>
                      {row.provider ? (
                        <Button
                          type="tertiary"
                          size="small"
                          loading={busy === row.key}
                          onClick={() => void connect(row.key)}
                        >
                          {t('accounts.relink')}
                        </Button>
                      ) : null}
                      <Button
                        type="tertiary"
                        size="small"
                        onClick={() => unlink(identity.id)}
                      >
                        {t('accounts.remove')}
                      </Button>
                    </>
                  ) : (
                    <Button
                      size="small"
                      loading={busy === row.key}
                      onClick={() => void connect(row.key)}
                    >
                      {t('accounts.connect')}
                    </Button>
                  )}
                </div>
              </li>
            )
          })}
        </ul>
      ) : loaded && !error ? (
        <div
          style={{
            display: 'flex',
            flexDirection: 'column',
            alignItems: 'flex-start',
            gap: 8,
          }}
        >
          <Typography.Text type="tertiary" size="small">
            {t('accounts.empty')}
          </Typography.Text>
          {isAdmin ? (
            <Button size="small" onClick={() => navigate('/admin/settings/oauth')}>
              {t('accounts.configure')}
            </Button>
          ) : (
            <Typography.Text type="tertiary" size="small">
              {t('accounts.emptyHint')}
            </Typography.Text>
          )}
        </div>
      ) : null}
    </section>
  )
}
