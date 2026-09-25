import { useCallback, useEffect, useState, type CSSProperties } from 'react'
import { Banner, Button, Modal, Typography } from '@douyinfe/semi-ui-19'
import {
  identities,
  oauthProviders,
  type Identity,
  type OAuthProviderOption,
} from '../api'
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

export function LinkedAccountsPanel() {
  const t = useT()
  const [list, setList] = useState<Identity[]>([])
  const [providers, setProviders] = useState<OAuthProviderOption[]>([])
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)

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

  const unlinked = providers.filter(
    (p) => !list.some((i) => i.providerId === p.id),
  )

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
          {list.map((identity) => (
            <li key={identity.id} style={rowStyle}>
              <div style={{ minWidth: 0 }}>
                <Typography.Text
                  style={{
                    fontFamily: 'var(--semi-font-family-code)',
                    fontSize: 12,
                  }}
                >
                  {identity.providerLabel} · {identity.providerHost}
                </Typography.Text>
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
              </div>
              <Button
                type="tertiary"
                size="small"
                onClick={() => unlink(identity.id)}
              >
                {t('accounts.remove')}
              </Button>
            </li>
          ))}
        </ul>
      ) : (
        <Typography.Text type="tertiary" size="small">
          {t('accounts.empty')}
        </Typography.Text>
      )}
      {unlinked.length > 0 ? (
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
          {unlinked.map((p) => (
            <Button
              key={p.id}
              loading={busy === p.id}
              onClick={() => void connect(p.id)}
            >
              {`${t('accounts.connect')} ${p.label}`}
            </Button>
          ))}
        </div>
      ) : null}
    </section>
  )
}
