import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type CSSProperties,
  type MouseEvent,
  type ReactNode,
} from 'react'
import { Banner, Button, Input, Modal, Select, Typography } from '@douyinfe/semi-ui-19'
import {
  IconApps,
  IconCalendar,
  IconCloud,
  IconGithubLogo,
  IconGitlabLogo,
  IconKey,
} from '@douyinfe/semi-icons'
import {
  appPath,
  connectors,
  gitCredentials,
  identities,
  mcp,
  oauthProviders,
  type ConnectorStatus,
  type GitCredential,
  type Identity,
  type McpCatalogItem,
  type McpConnection,
  type OAuthProviderOption,
} from '../api'
import { useT } from '../i18n'
import type { MessageKey } from '../i18n/translate'

const APPS_PATH = appPath('/settings/apps')
const GIT_NEW_KEY = 'git:new'

/** First-class PAT apps — host is user-supplied (not a fixed admin OAuth host). */
const GIT_PAT_PRESETS = [
  {
    provider: 'gitea',
    nameKey: 'git.provider.gitea' as MessageKey,
    helpKey: 'apps.giteaHelp' as MessageKey,
    guideKey: 'apps.giteaPatGuide' as MessageKey,
  },
  {
    provider: 'gitlab',
    nameKey: 'git.provider.gitlab' as MessageKey,
    helpKey: 'apps.gitlabHelp' as MessageKey,
    guideKey: 'apps.gitlabPatGuide' as MessageKey,
  },
] as const

const GIT_PROVIDER_KEYS = [
  { value: 'gitea', labelKey: 'git.provider.gitea' as MessageKey },
  { value: 'github', labelKey: 'git.provider.github' as MessageKey },
  { value: 'gitlab', labelKey: 'git.provider.gitlab' as MessageKey },
  { value: 'generic', labelKey: 'git.provider.generic' as MessageKey },
] as const

function patSettingsPath(provider: string, host: string): string | null {
  const h = host.trim().replace(/^https?:\/\//i, '').replace(/\/+$/, '')
  if (!h) return null
  if (provider === 'gitea') return `https://${h}/user/settings/applications`
  if (provider === 'gitlab') return `https://${h}/-/user_settings/personal_access_tokens`
  return null
}

const sectionGap: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 16,
}

const listStyle: CSSProperties = {
  listStyle: 'none',
  margin: 0,
  padding: 0,
  display: 'flex',
  flexDirection: 'column',
}

const rowStyle: CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  gap: 12,
  padding: '12px 4px',
  borderBottom: '1px solid var(--semi-color-border)',
  cursor: 'pointer',
}

const iconWrap: CSSProperties = {
  width: 40,
  height: 40,
  borderRadius: 10,
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  flexShrink: 0,
  background: 'var(--semi-color-fill-0)',
  color: 'var(--semi-color-text-0)',
  fontSize: 22,
}

const rowBody: CSSProperties = {
  flex: 1,
  minWidth: 0,
  display: 'flex',
  flexDirection: 'column',
  gap: 2,
}

const detailGap: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 12,
}

const detailActions: CSSProperties = {
  display: 'flex',
  flexWrap: 'wrap',
  gap: 8,
  marginTop: 8,
}

const CALLBACK_ERR_KEYS: Record<string, MessageKey> = {
  state: 'mcp.errState',
  denied: 'mcp.errDenied',
  token: 'mcp.errToken',
  dcr: 'mcp.errDcr',
  unsupported: 'mcp.errUnsupported',
}

type AppEntry = {
  key: string
  kind: 'oauth' | 'google' | 'mcp' | 'git'
  id: string
  name: string
  description: string
  installed: boolean
  accountLabel?: string
  scopes?: string
  identity?: Identity
  provider?: OAuthProviderOption
  mcpItem?: McpCatalogItem
  mcpConn?: McpConnection
  gitCred?: GitCredential
  /** Fixed provider when adding via Gitea / GitLab PAT tile. */
  gitPreset?: 'gitea' | 'gitlab'
  icon: ReactNode
  testId: string
}

function oauthIcon(kind: string, host: string): ReactNode {
  const k = kind.toLowerCase()
  if (k === 'github' || host.includes('github')) return <IconGithubLogo />
  if (k === 'gitlab' || host.includes('gitlab')) return <IconGitlabLogo />
  if (k === 'gitea' || host.includes('gitea')) return <IconGitlabLogo />
  return <IconCloud />
}

function mcpIcon(item: McpCatalogItem): ReactNode {
  const id = item.id.toLowerCase()
  if (id === 'github' || item.name.toLowerCase().includes('github')) {
    return <IconGithubLogo />
  }
  if (id === 'gitlab' || item.name.toLowerCase().includes('gitlab')) {
    return <IconGitlabLogo />
  }
  return item.auth === 'api_key' ? <IconKey /> : <IconApps />
}

function oauthHostKey(kind: string, host: string): string {
  return `${kind.toLowerCase()}|${host.toLowerCase()}`
}

function normHost(host: string): string {
  return host.trim().toLowerCase().replace(/\/+$/, '')
}

export function ConnectedAppsPanel() {
  const t = useT()
  const [identitiesList, setIdentitiesList] = useState<Identity[]>([])
  const [providers, setProviders] = useState<OAuthProviderOption[]>([])
  const [google, setGoogle] = useState<ConnectorStatus | null>(null)
  const [mcpCatalog, setMcpCatalog] = useState<McpCatalogItem[]>([])
  const [mcpConnections, setMcpConnections] = useState<McpConnection[]>([])
  const [gitCreds, setGitCreds] = useState<GitCredential[]>([])
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [loaded, setLoaded] = useState(false)
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const [keyValue, setKeyValue] = useState('')
  const [preferKey, setPreferKey] = useState(false)
  const [gitProvider, setGitProvider] = useState('gitea')
  const [gitHost, setGitHost] = useState('')
  const [gitUsername, setGitUsername] = useState('')
  const [gitToken, setGitToken] = useState('')

  const gitProviderOptions = useMemo(
    () =>
      GIT_PROVIDER_KEYS.map((p) => ({
        value: p.value,
        label: t(p.labelKey),
      })),
    [t],
  )

  const load = useCallback(async () => {
    setError(null)
    try {
      const [linked, available, connList, cat, mcpConns, gitList] =
        await Promise.all([
          identities.list(),
          oauthProviders.listLinkable(),
          connectors.list(),
          mcp.catalog(),
          mcp.listConnections(),
          gitCredentials.list(),
        ])
      setIdentitiesList(linked.identities ?? [])
      setProviders(available.providers ?? [])
      const g =
        (connList.connectors ?? []).find((c) => c.kind === 'google') ?? null
      setGoogle(g)
      setMcpCatalog(cat.servers ?? [])
      setMcpConnections(mcpConns.connections ?? [])
      setGitCreds(gitList.credentials ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : t('apps.loadFailed'))
    } finally {
      setLoaded(true)
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    let changed = false

    const linked = params.get('oauth_linked')
    const oauthFailed = params.get('oauth_error')
    if (linked) {
      setNotice(t('apps.linked'))
      params.delete('oauth_linked')
      changed = true
    }
    if (oauthFailed) {
      setError(
        oauthFailed === 'already_linked'
          ? t('accounts.errLinked')
          : oauthFailed === 'session'
            ? t('accounts.errSession')
            : t('accounts.errFailed'),
      )
      params.delete('oauth_error')
      changed = true
    }

    const googleResult = params.get('google')
    if (googleResult) {
      if (googleResult === 'linked') setNotice(t('apps.linked'))
      else setError(t('connectors.linkFailed'))
      params.delete('google')
      changed = true
    }

    const mcpResult = params.get('mcpResult')
    if (mcpResult) {
      if (mcpResult === 'linked') {
        setNotice(t('apps.linked'))
      } else {
        const code = params.get('mcpError') ?? ''
        const key = CALLBACK_ERR_KEYS[code]
        setError(key ? t(key) : t('mcp.linkFailed'))
      }
      params.delete('mcpResult')
      params.delete('mcpError')
      params.delete('mcp')
      changed = true
    }

    if (changed) {
      const next = `${APPS_PATH}${params.toString() ? `?${params}` : ''}`
      window.history.replaceState({}, '', next)
      void load()
    }
  }, [load, t])

  const mcpByServer = useMemo(() => {
    const map = new Map<string, McpConnection>()
    for (const c of mcpConnections) map.set(c.serverId, c)
    return map
  }, [mcpConnections])

  const catalog = useMemo((): AppEntry[] => {
    const out: AppEntry[] = []
    const seenOauth = new Set<string>()
    // Same host (e.g. github.com via relay vs admin App) must not list twice.
    const seenOauthHost = new Set<string>()

    for (const identity of identitiesList) {
      seenOauth.add(identity.providerId)
      seenOauthHost.add(oauthHostKey(identity.providerKind, identity.providerHost))
      const provider = providers.find((p) => p.id === identity.providerId)
      out.push({
        key: `oauth:${identity.providerId}`,
        kind: 'oauth',
        id: identity.providerId,
        name: identity.providerLabel || identity.providerKind || identity.providerId,
        description: t('apps.oauthHelp'),
        installed: true,
        accountLabel: [identity.login, identity.email].filter(Boolean).join(' · '),
        scopes: identity.scopes,
        identity,
        provider,
        icon: oauthIcon(identity.providerKind, identity.providerHost),
        testId: `app-installed-oauth-${identity.providerId}`,
      })
    }
    for (const p of providers) {
      // Gitea / GitLab agent access uses host + PAT tiles below — do not push
      // users onto an admin-pinned OAuth host (e.g. a single corporate Gitea).
      if (p.kind === 'gitea' || p.kind === 'gitlab') continue
      if (seenOauth.has(p.id)) continue
      const hostKey = oauthHostKey(p.kind, p.host)
      if (seenOauthHost.has(hostKey)) continue
      seenOauthHost.add(hostKey)
      out.push({
        key: `oauth:${p.id}`,
        kind: 'oauth',
        id: p.id,
        name: p.label,
        description: t('apps.oauthHelp'),
        installed: false,
        provider: p,
        icon: oauthIcon(p.kind, p.host),
        testId: `app-available-oauth-${p.id}`,
      })
    }

    if (google?.official || google?.connected) {
      out.push({
        key: 'google',
        kind: 'google',
        id: 'google',
        name: t('connectors.google'),
        description: t('connectors.officialHelp'),
        installed: Boolean(google?.connected),
        accountLabel: google?.email || google?.subject || undefined,
        scopes: google?.scopes,
        icon: <IconCalendar />,
        testId: google?.connected ? 'app-installed-google' : 'app-google',
      })
    }

    for (const item of mcpCatalog) {
      const conn = mcpByServer.get(item.id)
      out.push({
        key: `mcp:${item.id}`,
        kind: 'mcp',
        id: item.id,
        name: item.name,
        description: item.description,
        installed: Boolean(conn),
        accountLabel: conn
          ? conn.lastError
            ? conn.lastError
            : conn.accountLabel ||
              t('mcp.toolsCount', { count: String(conn.toolCount) })
          : undefined,
        scopes: conn?.scopes,
        mcpItem: item,
        mcpConn: conn,
        icon: mcpIcon(item),
        testId: conn ? `mcp-conn-${item.id}` : `mcp-server-${item.id}`,
      })
    }

    // PAT for a host already covered by OAuth stays in the DB (overrides) but
    // is not listed twice — manage via Gitea/GitLab / generic token add.
    const oauthHosts = new Set(
      identitiesList.map((i) => normHost(i.providerHost)).filter(Boolean),
    )
    for (const c of gitCreds) {
      if (oauthHosts.has(normHost(c.host))) continue
      out.push({
        key: `git:${c.id}`,
        kind: 'git',
        id: c.id,
        name: c.host,
        description: t('apps.gitHelp'),
        installed: true,
        accountLabel: [c.provider, c.username].filter(Boolean).join(' · '),
        gitCred: c,
        icon: oauthIcon(c.provider, c.host),
        testId: `app-installed-git-${c.id}`,
      })
    }
    for (const preset of GIT_PAT_PRESETS) {
      out.push({
        key: `git:new:${preset.provider}`,
        kind: 'git',
        id: `new-${preset.provider}`,
        name: t(preset.nameKey),
        description: t(preset.helpKey),
        installed: false,
        gitPreset: preset.provider,
        icon: oauthIcon(preset.provider, ''),
        testId: `app-available-git-${preset.provider}`,
      })
    }
    out.push({
      key: GIT_NEW_KEY,
      kind: 'git',
      id: 'new',
      name: t('apps.gitHost'),
      description: t('apps.gitHelp'),
      installed: false,
      icon: <IconKey />,
      testId: 'app-available-git-host',
    })

    out.sort((a, b) => {
      if (a.installed !== b.installed) return a.installed ? -1 : 1
      // Keep PAT / generic git add tiles after other available apps.
      const aGitNew = a.key.startsWith('git:new')
      const bGitNew = b.key.startsWith('git:new')
      if (aGitNew !== bGitNew) return aGitNew ? 1 : -1
      return a.name.localeCompare(b.name)
    })
    return out
  }, [identitiesList, providers, google, mcpCatalog, mcpByServer, gitCreds, t])

  const installedCount = catalog.filter((a) => a.installed).length
  const selected = catalog.find((a) => a.key === selectedKey) ?? null

  useEffect(() => {
    if (!selected) {
      setKeyValue('')
      setPreferKey(false)
      setGitToken('')
      return
    }
    if (selected.kind === 'mcp' && selected.mcpItem?.auth === 'api_key') {
      setPreferKey(true)
    } else {
      setPreferKey(false)
    }
    setKeyValue('')
    if (selected.kind === 'git' && selected.gitCred) {
      setGitProvider(selected.gitCred.provider || 'gitea')
      setGitHost(selected.gitCred.host)
      setGitUsername(selected.gitCred.username || '')
      setGitToken('')
    } else if (selected.kind === 'git' && selected.gitPreset) {
      setGitProvider(selected.gitPreset)
      setGitHost('')
      setGitUsername('')
      setGitToken('')
    } else if (selected.key === GIT_NEW_KEY) {
      setGitProvider('generic')
      setGitHost('')
      setGitUsername('')
      setGitToken('')
    }
  }, [selected])

  async function connectOauth(providerId: string) {
    setBusy(`oauth:${providerId}`)
    setError(null)
    setNotice(null)
    try {
      const res = await identities.linkUrl(providerId, undefined, APPS_PATH)
      window.location.assign(res.authorizeUrl)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('accounts.connectFailed'))
      setBusy(null)
    }
  }

  function uninstallOauth(id: string) {
    setSelectedKey(null)
    Modal.confirm({
      title: t('apps.uninstall'),
      content: t('accounts.confirmUnlink'),
      onOk: async () => {
        setError(null)
        try {
          await identities.remove(id)
          setNotice(t('apps.disconnected'))
          await load()
        } catch (e) {
          setError(e instanceof Error ? e.message : t('accounts.unlinkFailed'))
        }
      },
    })
  }

  async function connectGoogle() {
    setBusy('google:connect')
    setError(null)
    setNotice(null)
    try {
      const res = await connectors.connectGoogle()
      window.location.href = res.authorizeUrl
    } catch (e) {
      setError(e instanceof Error ? e.message : t('connectors.connectFailed'))
      setBusy(null)
    }
  }

  function uninstallGoogle() {
    setSelectedKey(null)
    Modal.confirm({
      title: t('apps.uninstall'),
      content: t('apps.confirmUninstall'),
      onOk: async () => {
        setBusy('google:off')
        setError(null)
        try {
          await connectors.disconnectGoogle()
          setNotice(t('apps.disconnected'))
          await load()
        } catch (e) {
          setError(
            e instanceof Error ? e.message : t('connectors.disconnectFailed'),
          )
        } finally {
          setBusy(null)
        }
      },
    })
  }

  async function connectMcpOAuth(serverId: string) {
    setBusy(`mcp-oauth:${serverId}`)
    setError(null)
    setNotice(null)
    try {
      const res = await mcp.startOAuth(serverId, APPS_PATH)
      window.location.href = res.authorizeUrl
    } catch (e) {
      setError(e instanceof Error ? e.message : t('mcp.startFailed'))
      setBusy(null)
    }
  }

  async function saveMcpKey(serverId: string) {
    setBusy(`mcp-key:${serverId}`)
    setError(null)
    setNotice(null)
    try {
      await mcp.connectApiKey(serverId, keyValue.trim())
      setKeyValue('')
      setSelectedKey(null)
      setNotice(t('apps.linked'))
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('mcp.connectFailed'))
    } finally {
      setBusy(null)
    }
  }

  function uninstallMcp(serverId: string) {
    setSelectedKey(null)
    Modal.confirm({
      title: t('apps.uninstall'),
      content: t('apps.confirmUninstall'),
      onOk: async () => {
        setBusy(`mcp-off:${serverId}`)
        setError(null)
        try {
          await mcp.disconnect(serverId)
          setNotice(t('apps.disconnected'))
          await load()
        } catch (e) {
          setError(e instanceof Error ? e.message : t('mcp.disconnectFailed'))
        } finally {
          setBusy(null)
        }
      },
    })
  }

  async function saveGit(existingId?: string) {
    setBusy(existingId ? `git-save:${existingId}` : 'git-save:new')
    setError(null)
    setNotice(null)
    try {
      await gitCredentials.upsert({
        id: existingId,
        provider: gitProvider,
        host: gitHost.trim(),
        username: gitUsername.trim(),
        token: gitToken.trim(),
      })
      setGitToken('')
      setNotice(t('apps.gitSaved'))
      setSelectedKey(null)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('git.saveFailed'))
    } finally {
      setBusy(null)
    }
  }

  function uninstallGit(id: string) {
    Modal.confirm({
      title: t('apps.uninstall'),
      content: t('apps.confirmUninstall'),
      okType: 'danger',
      onOk: async () => {
        setBusy(`git-off:${id}`)
        setError(null)
        try {
          await gitCredentials.remove(id)
          setNotice(t('apps.disconnected'))
          setSelectedKey(null)
          await load()
        } catch (e) {
          setError(e instanceof Error ? e.message : t('git.deleteFailed'))
        } finally {
          setBusy(null)
        }
      },
    })
  }

  function onAddClick(e: MouseEvent, app: AppEntry) {
    e.stopPropagation()
    if (app.installed || busy) return
    if (app.kind === 'oauth') {
      void connectOauth(app.id)
      return
    }
    if (app.kind === 'google') {
      void connectGoogle()
      return
    }
    if (app.kind === 'git') {
      setSelectedKey(app.key)
      return
    }
    // Pure OAuth MCP can start immediately; API key / dual-auth need the sheet.
    if (app.mcpItem?.auth === 'oauth') {
      void connectMcpOAuth(app.id)
      return
    }
    setSelectedKey(app.key)
  }

  function canReauth(app: AppEntry): boolean {
    if (!app.installed) return false
    // Secondary recovery when the grant expires, scopes change, or MCP asks
    // for reauth — not needed for day-to-day use.
    if (app.kind === 'oauth') return Boolean(app.provider)
    if (app.kind === 'google') return true
    if (app.kind === 'mcp') {
      const st = app.mcpConn?.status
      return st === 'needs_reauth' || st === 'error'
    }
    if (app.kind === 'git') return true
    return false
  }

  function reauth(app: AppEntry) {
    if (app.kind === 'oauth') void connectOauth(app.id)
    else if (app.kind === 'google') void connectGoogle()
    else if (app.kind === 'git') setSelectedKey(app.key)
    else if (app.kind === 'mcp') {
      if (app.mcpConn?.authKind === 'api_key') {
        setPreferKey(true)
        setKeyValue('')
      } else {
        void connectMcpOAuth(app.id)
      }
    }
  }

  function uninstall(app: AppEntry) {
    if (app.kind === 'oauth' && app.identity) uninstallOauth(app.identity.id)
    else if (app.kind === 'google') uninstallGoogle()
    else if (app.kind === 'mcp') uninstallMcp(app.id)
    else if (app.kind === 'git' && app.gitCred) uninstallGit(app.gitCred.id)
  }

  if (!loaded) {
    return <Typography.Text type="tertiary">{t('apps.loading')}</Typography.Text>
  }

  const mcpUsesKey =
    selected?.kind === 'mcp' &&
    (selected.mcpItem?.auth === 'api_key' ||
      preferKey ||
      selected.mcpConn?.authKind === 'api_key')

  return (
    <div style={sectionGap}>
      <div>
        <Typography.Title heading={4} style={{ margin: 0 }}>
          {t('apps.title')}
        </Typography.Title>
        <Typography.Paragraph type="tertiary" style={{ margin: '8px 0 0' }}>
          {t('apps.desc')}
        </Typography.Paragraph>
      </div>

      {error && <Banner type="danger" description={error} closeIcon={null} />}
      {notice && <Banner type="success" description={notice} closeIcon={null} />}

      <Typography.Text strong>
        {t('apps.installedCount', { count: String(installedCount) })}
      </Typography.Text>

      {catalog.length === 0 ? (
        <Typography.Text type="tertiary" size="small">
          {t('apps.catalogEmpty')}
        </Typography.Text>
      ) : (
        <ul style={listStyle}>
          {catalog.map((app) => (
            <li
              key={app.key}
              style={rowStyle}
              data-testid={app.testId}
              onClick={() => setSelectedKey(app.key)}
            >
              <div style={iconWrap} aria-hidden>
                {app.icon}
              </div>
              <div style={rowBody}>
                <Typography.Text
                  strong
                  ellipsis={{ showTooltip: true }}
                  style={{ display: 'block' }}
                >
                  {app.name}
                </Typography.Text>
                <Typography.Text
                  type="tertiary"
                  size="small"
                  ellipsis={{ showTooltip: true }}
                  style={{ display: 'block' }}
                >
                  {app.installed && app.accountLabel
                    ? app.accountLabel
                    : app.description}
                </Typography.Text>
              </div>
              <Button
                size="small"
                theme={app.installed ? 'borderless' : 'solid'}
                type={app.installed ? 'tertiary' : 'primary'}
                disabled={app.installed || !!busy}
                loading={
                  !app.installed &&
                  (busy === `oauth:${app.id}` ||
                    busy === 'google:connect' ||
                    busy === `mcp-oauth:${app.id}` ||
                    busy === 'git-save:new')
                }
                onClick={(e) => onAddClick(e, app)}
              >
                {app.installed ? t('apps.added') : t('apps.add')}
              </Button>
            </li>
          ))}
        </ul>
      )}

      <Modal
        title={selected?.name}
        visible={Boolean(selected)}
        onCancel={() => setSelectedKey(null)}
        footer={null}
        width={440}
        keepDOM={false}
      >
        {selected ? (
          <div style={detailGap}>
            <div style={{ display: 'flex', gap: 12, alignItems: 'center' }}>
              <div style={iconWrap}>{selected.icon}</div>
              <div style={{ minWidth: 0 }}>
                <Typography.Text strong>{selected.name}</Typography.Text>
                <Typography.Paragraph
                  type="tertiary"
                  size="small"
                  style={{ margin: '4px 0 0' }}
                >
                  {selected.description}
                </Typography.Paragraph>
              </div>
            </div>

            {selected.installed ? (
              <>
                {selected.accountLabel ? (
                  <div>
                    <Typography.Text type="tertiary" size="small">
                      {t('apps.detailAccount')}
                    </Typography.Text>
                    <Typography.Text style={{ display: 'block' }}>
                      {selected.accountLabel}
                    </Typography.Text>
                  </div>
                ) : null}
                {selected.scopes ? (
                  <div>
                    <Typography.Text type="tertiary" size="small">
                      {t('connectors.scopes')}
                    </Typography.Text>
                    <Typography.Text
                      size="small"
                      style={{ display: 'block', wordBreak: 'break-all' }}
                    >
                      {selected.scopes}
                    </Typography.Text>
                  </div>
                ) : null}
                {selected.kind === 'mcp' && selected.mcpConn?.lastError ? (
                  <Banner
                    type="warning"
                    description={selected.mcpConn.lastError}
                    closeIcon={null}
                  />
                ) : null}
                {selected.kind === 'mcp' && selected.mcpConn && !selected.mcpConn.lastError ? (
                  <Typography.Text type="tertiary" size="small">
                    {t('mcp.toolsCount', {
                      count: String(selected.mcpConn.toolCount),
                    })}
                  </Typography.Text>
                ) : null}
                {selected.kind === 'mcp' &&
                  selected.mcpConn?.status === 'needs_reauth' &&
                  mcpUsesKey && (
                    <div style={detailGap}>
                      <Input
                        mode="password"
                        value={keyValue}
                        onChange={setKeyValue}
                        placeholder={t('mcp.apiKeyPlaceholder')}
                      />
                      <Button
                        theme="solid"
                        loading={busy === `mcp-key:${selected.id}`}
                        disabled={!!busy || !keyValue.trim()}
                        onClick={() => void saveMcpKey(selected.id)}
                      >
                        {t('mcp.saveKey')}
                      </Button>
                    </div>
                  )}
                {selected.mcpItem?.docsUrl ? (
                  <a
                    href={selected.mcpItem.docsUrl}
                    target="_blank"
                    rel="noreferrer"
                  >
                    {t('mcp.docs')}
                  </a>
                ) : null}
                {selected.kind === 'git' && selected.gitCred ? (
                  <div style={detailGap}>
                    {(gitProvider === 'gitea' || gitProvider === 'gitlab') && (
                      <Banner
                        type="info"
                        closeIcon={null}
                        description={
                          gitProvider === 'gitea'
                            ? t('apps.giteaPatGuide')
                            : t('apps.gitlabPatGuide')
                        }
                      />
                    )}
                    <div>
                      <Typography.Text type="tertiary" size="small">
                        {t('git.provider')}
                      </Typography.Text>
                      <Select
                        value={gitProvider}
                        onChange={(v) => setGitProvider(String(v))}
                        optionList={gitProviderOptions}
                        style={{ width: '100%' }}
                      />
                    </div>
                    <div>
                      <Typography.Text type="tertiary" size="small">
                        {t('git.host')}
                      </Typography.Text>
                      <Input
                        value={gitHost}
                        onChange={setGitHost}
                        placeholder={t('apps.gitHostPlaceholder')}
                      />
                    </div>
                    <div>
                      <Typography.Text type="tertiary" size="small">
                        {t('git.username')}
                      </Typography.Text>
                      <Input
                        value={gitUsername}
                        onChange={setGitUsername}
                        placeholder={
                          gitProvider === 'gitlab'
                            ? 'oauth2'
                            : gitProvider === 'github'
                              ? 'x-access-token'
                              : 'git'
                        }
                      />
                    </div>
                    <div>
                      <Typography.Text type="tertiary" size="small">
                        {t('git.token')}
                      </Typography.Text>
                      <Input
                        mode="password"
                        autoComplete="new-password"
                        value={gitToken}
                        onChange={setGitToken}
                        placeholder={t('git.tokenPlaceholder')}
                      />
                    </div>
                    {(() => {
                      const href = patSettingsPath(gitProvider, gitHost)
                      return href ? (
                        <a href={href} target="_blank" rel="noreferrer">
                          {t('apps.openPatSettings')}
                        </a>
                      ) : null
                    })()}
                    <Button
                      theme="solid"
                      type="primary"
                      loading={busy === `git-save:${selected.gitCred.id}`}
                      disabled={
                        !!busy || !gitHost.trim() || !gitToken.trim()
                      }
                      onClick={() => void saveGit(selected.gitCred?.id)}
                    >
                      {t('git.save')}
                    </Button>
                  </div>
                ) : null}
                <div style={detailActions}>
                  {canReauth(selected) && selected.kind !== 'git' ? (
                    <Button
                      loading={
                        busy === `oauth:${selected.id}` ||
                        busy === 'google:connect' ||
                        busy === `mcp-oauth:${selected.id}`
                      }
                      disabled={!!busy}
                      onClick={() => reauth(selected)}
                    >
                      {t('apps.reauth')}
                    </Button>
                  ) : null}
                  <Button
                    type="danger"
                    theme="solid"
                    disabled={!!busy}
                    loading={
                      busy === 'google:off' ||
                      busy === `mcp-off:${selected.id}` ||
                      busy === `git-off:${selected.id}`
                    }
                    onClick={() => uninstall(selected)}
                  >
                    {t('apps.uninstall')}
                  </Button>
                </div>
              </>
            ) : (
              <>
                {selected.mcpItem?.docsUrl ? (
                  <a
                    href={selected.mcpItem.docsUrl}
                    target="_blank"
                    rel="noreferrer"
                  >
                    {t('mcp.docs')}
                  </a>
                ) : null}

                {selected.kind === 'oauth' && (
                  <div style={detailActions}>
                    <Button
                      theme="solid"
                      type="primary"
                      loading={busy === `oauth:${selected.id}`}
                      disabled={!!busy}
                      onClick={() => void connectOauth(selected.id)}
                    >
                      {t('apps.add')}
                    </Button>
                  </div>
                )}

                {selected.kind === 'google' && (
                  <div style={detailActions}>
                    <Button
                      theme="solid"
                      type="primary"
                      loading={busy === 'google:connect'}
                      disabled={!!busy}
                      onClick={() => void connectGoogle()}
                    >
                      {t('apps.add')}
                    </Button>
                  </div>
                )}

                {selected.kind === 'git' && (
                  <div style={detailGap}>
                    {(selected.gitPreset === 'gitea' ||
                      selected.gitPreset === 'gitlab' ||
                      gitProvider === 'gitea' ||
                      gitProvider === 'gitlab') && (
                      <Banner
                        type="info"
                        closeIcon={null}
                        description={
                          (selected.gitPreset || gitProvider) === 'gitlab'
                            ? t('apps.gitlabPatGuide')
                            : t('apps.giteaPatGuide')
                        }
                      />
                    )}
                    {!selected.gitPreset ? (
                      <div>
                        <Typography.Text type="tertiary" size="small">
                          {t('git.provider')}
                        </Typography.Text>
                        <Select
                          value={gitProvider}
                          onChange={(v) => setGitProvider(String(v))}
                          optionList={gitProviderOptions}
                          style={{ width: '100%' }}
                        />
                      </div>
                    ) : null}
                    <div>
                      <Typography.Text type="tertiary" size="small">
                        {t('git.host')}
                      </Typography.Text>
                      <Input
                        value={gitHost}
                        onChange={setGitHost}
                        placeholder={t('apps.gitHostPlaceholder')}
                      />
                    </div>
                    {!selected.gitPreset ? (
                      <div>
                        <Typography.Text type="tertiary" size="small">
                          {t('git.username')}
                        </Typography.Text>
                        <Input
                          value={gitUsername}
                          onChange={setGitUsername}
                          placeholder="git"
                        />
                      </div>
                    ) : null}
                    <div>
                      <Typography.Text type="tertiary" size="small">
                        {t('git.token')}
                      </Typography.Text>
                      <Input
                        mode="password"
                        autoComplete="new-password"
                        value={gitToken}
                        onChange={setGitToken}
                        placeholder={t('git.tokenPlaceholder')}
                      />
                    </div>
                    {(() => {
                      const href = patSettingsPath(
                        selected.gitPreset || gitProvider,
                        gitHost,
                      )
                      return href ? (
                        <a href={href} target="_blank" rel="noreferrer">
                          {t('apps.openPatSettings')}
                        </a>
                      ) : null
                    })()}
                    <Button
                      theme="solid"
                      type="primary"
                      loading={busy === 'git-save:new'}
                      disabled={
                        !!busy || !gitHost.trim() || !gitToken.trim()
                      }
                      onClick={() => void saveGit()}
                    >
                      {t('apps.add')}
                    </Button>
                  </div>
                )}

                {selected.kind === 'mcp' && selected.mcpItem && (
                  <div style={detailGap}>
                    {!mcpUsesKey && (
                      <Button
                        theme="solid"
                        type="primary"
                        loading={busy === `mcp-oauth:${selected.id}`}
                        disabled={!!busy}
                        onClick={() => void connectMcpOAuth(selected.id)}
                      >
                        {t('mcp.connectOauth')}
                      </Button>
                    )}
                    {selected.mcpItem.auth === 'both' && (
                      <Button
                        disabled={!!busy}
                        onClick={() => setPreferKey((v) => !v)}
                      >
                        {mcpUsesKey ? t('mcp.useOauth') : t('mcp.useApiKey')}
                      </Button>
                    )}
                    {mcpUsesKey && (
                      <>
                        <Input
                          mode="password"
                          value={keyValue}
                          onChange={setKeyValue}
                          placeholder={t('mcp.apiKeyPlaceholder')}
                        />
                        <Button
                          theme="solid"
                          type="primary"
                          loading={busy === `mcp-key:${selected.id}`}
                          disabled={!!busy || !keyValue.trim()}
                          onClick={() => void saveMcpKey(selected.id)}
                        >
                          {t('mcp.saveKey')}
                        </Button>
                      </>
                    )}
                  </div>
                )}
              </>
            )}
          </div>
        ) : null}
      </Modal>
    </div>
  )
}
