import { useCallback, useEffect, useMemo, useState, type CSSProperties } from 'react'
import { Banner, Button, Input, Tag, Typography } from '@douyinfe/semi-ui-19'
import { mcp, type McpCatalogItem, type McpConnection } from '../api'
import { useT } from '../i18n'
import type { MessageKey } from '../i18n/translate'

const sectionGap: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 16,
}

const cardStyle: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 8,
  border: '1px solid var(--semi-color-border)',
  borderRadius: 8,
  padding: 12,
}

const rowStyle: CSSProperties = {
  display: 'flex',
  alignItems: 'center',
  flexWrap: 'wrap',
  gap: 8,
}

const GROUP_KEYS: Record<string, MessageKey> = {
  knowledge: 'mcp.group.knowledge',
  tasks: 'mcp.group.tasks',
  collab: 'mcp.group.collab',
  dev: 'mcp.group.dev',
  business: 'mcp.group.business',
  design: 'mcp.group.design',
  search: 'mcp.group.search',
}

const AUTH_KEYS: Record<string, MessageKey> = {
  oauth: 'mcp.authOauth',
  api_key: 'mcp.authApiKey',
  both: 'mcp.authBoth',
}

const STATUS_COLORS: Record<string, 'green' | 'orange' | 'red'> = {
  connected: 'green',
  needs_reauth: 'orange',
  error: 'red',
}

const STATUS_KEYS: Record<string, MessageKey> = {
  connected: 'mcp.statusConnected',
  needs_reauth: 'mcp.statusNeedsReauth',
  error: 'mcp.statusError',
}

const CALLBACK_ERR_KEYS: Record<string, MessageKey> = {
  state: 'mcp.errState',
  denied: 'mcp.errDenied',
  token: 'mcp.errToken',
  dcr: 'mcp.errDcr',
  unsupported: 'mcp.errUnsupported',
}

export function McpPanel() {
  const t = useT()
  const [catalog, setCatalog] = useState<McpCatalogItem[]>([])
  const [connections, setConnections] = useState<McpConnection[]>([])
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [loaded, setLoaded] = useState(false)
  // Which server's inline API key form is open, and the OAuth/API-key
  // preference for "both" servers.
  const [keyFor, setKeyFor] = useState<string | null>(null)
  const [keyValue, setKeyValue] = useState('')
  const [preferKey, setPreferKey] = useState<Record<string, boolean>>({})

  const load = useCallback(async () => {
    setError(null)
    try {
      const [cat, conns] = await Promise.all([
        mcp.catalog(),
        mcp.listConnections(),
      ])
      setCatalog(cat.servers ?? [])
      setConnections(conns.connections ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : t('mcp.loadFailed'))
    } finally {
      setLoaded(true)
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    const params = new URLSearchParams(window.location.search)
    const result = params.get('mcpResult')
    if (!result) return
    if (result === 'linked') {
      setNotice(t('mcp.linked'))
    } else {
      const code = params.get('mcpError') ?? ''
      const key = CALLBACK_ERR_KEYS[code]
      setError(key ? t(key) : t('mcp.linkFailed'))
    }
    params.delete('mcpResult')
    params.delete('mcpError')
    params.delete('mcp')
    const next = `${window.location.pathname}${params.toString() ? `?${params}` : ''}`
    window.history.replaceState({}, '', next)
    void load()
  }, [load, t])

  const byServer = useMemo(() => {
    const map = new Map<string, McpConnection>()
    for (const c of connections) map.set(c.serverId, c)
    return map
  }, [connections])

  const grouped = useMemo(() => {
    const map = new Map<string, McpCatalogItem[]>()
    for (const item of catalog) {
      const list = map.get(item.group) ?? []
      list.push(item)
      map.set(item.group, list)
    }
    return [...map.entries()]
  }, [catalog])

  async function onConnectOAuth(serverId: string) {
    setBusy(`oauth:${serverId}`)
    setError(null)
    setNotice(null)
    try {
      const res = await mcp.startOAuth(serverId, '/settings/mcp')
      window.location.href = res.authorizeUrl
    } catch (e) {
      setError(e instanceof Error ? e.message : t('mcp.startFailed'))
      setBusy(null)
    }
  }

  async function onSaveKey(serverId: string) {
    setBusy(`key:${serverId}`)
    setError(null)
    setNotice(null)
    try {
      await mcp.connectApiKey(serverId, keyValue.trim())
      setKeyFor(null)
      setKeyValue('')
      setNotice(t('mcp.keySaved'))
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('mcp.connectFailed'))
    } finally {
      setBusy(null)
    }
  }

  async function onDisconnect(serverId: string) {
    setBusy(`off:${serverId}`)
    setError(null)
    setNotice(null)
    try {
      await mcp.disconnect(serverId)
      setNotice(t('mcp.disconnected'))
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('mcp.disconnectFailed'))
    } finally {
      setBusy(null)
    }
  }

  async function onRefresh(serverId: string) {
    setBusy(`refresh:${serverId}`)
    setError(null)
    setNotice(null)
    try {
      const res = await mcp.refreshTools(serverId)
      setNotice(t('mcp.refreshed', { count: String(res.connection.toolCount) }))
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('mcp.refreshFailed'))
    } finally {
      setBusy(null)
    }
  }

  if (!loaded) {
    return <Typography.Text type="tertiary">{t('mcp.loading')}</Typography.Text>
  }

  function statusTag(conn: McpConnection) {
    const labelKey = STATUS_KEYS[conn.status] ?? 'mcp.statusError'
    return <Tag color={STATUS_COLORS[conn.status] ?? 'red'}>{t(labelKey)}</Tag>
  }

  // usesKey: API-key form is the active method (key-only servers, or the user
  // switched a dual-auth server away from OAuth).
  function usesKey(item: McpCatalogItem) {
    return item.auth === 'api_key' || preferKey[item.id] === true
  }

  function keyForm(serverId: string) {
    return (
      <div style={rowStyle}>
        <Input
          mode="password"
          value={keyValue}
          onChange={setKeyValue}
          placeholder={t('mcp.apiKeyPlaceholder')}
          style={{ maxWidth: 320 }}
        />
        <Button
          theme="solid"
          type="primary"
          loading={busy === `key:${serverId}`}
          disabled={!!busy || !keyValue.trim()}
          onClick={() => void onSaveKey(serverId)}
        >
          {t('mcp.saveKey')}
        </Button>
        <Button
          disabled={!!busy}
          onClick={() => {
            setKeyFor(null)
            setKeyValue('')
          }}
        >
          {t('mcp.cancel')}
        </Button>
      </div>
    )
  }

  return (
    <div style={sectionGap}>
      <div>
        <Typography.Title heading={4} style={{ margin: 0 }}>
          {t('mcp.title')}
        </Typography.Title>
        <Typography.Paragraph type="tertiary" style={{ margin: '8px 0 0' }}>
          {t('mcp.desc')}
        </Typography.Paragraph>
      </div>

      {error && <Banner type="danger" description={error} closeIcon={null} />}
      {notice && <Banner type="success" description={notice} closeIcon={null} />}

      {connections.length > 0 && (
        <div style={sectionGap}>
          <Typography.Title heading={5} style={{ margin: 0 }}>
            {t('mcp.connectedSection')}
          </Typography.Title>
          {connections.map((conn) => (
            <div key={conn.serverId} style={cardStyle} data-testid={`mcp-conn-${conn.serverId}`}>
              <div style={rowStyle}>
                <Typography.Text strong>{conn.serverName}</Typography.Text>
                {statusTag(conn)}
                <Tag>{t(AUTH_KEYS[conn.authKind] ?? 'mcp.authApiKey')}</Tag>
                <Typography.Text type="tertiary" size="small">
                  {t('mcp.toolsCount', { count: String(conn.toolCount) })}
                </Typography.Text>
              </div>
              {conn.lastError ? (
                <Typography.Text type="danger" size="small">
                  {t('mcp.lastError')}: {conn.lastError}
                </Typography.Text>
              ) : null}
              {conn.status === 'needs_reauth' && keyFor !== conn.serverId && (
                <div style={rowStyle}>
                  {conn.authKind === 'oauth' ? (
                    <Button
                      theme="solid"
                      type="primary"
                      loading={busy === `oauth:${conn.serverId}`}
                      disabled={!!busy}
                      onClick={() => void onConnectOAuth(conn.serverId)}
                    >
                      {t('mcp.reconnect')}
                    </Button>
                  ) : (
                    <Button
                      theme="solid"
                      type="primary"
                      disabled={!!busy}
                      onClick={() => {
                        setKeyFor(conn.serverId)
                        setKeyValue('')
                      }}
                    >
                      {t('mcp.reconnect')}
                    </Button>
                  )}
                </div>
              )}
              {conn.status === 'needs_reauth' && keyFor === conn.serverId && keyForm(conn.serverId)}
              <div style={rowStyle}>
                <Button
                  disabled={!!busy}
                  loading={busy === `refresh:${conn.serverId}`}
                  onClick={() => void onRefresh(conn.serverId)}
                >
                  {t('mcp.refreshTools')}
                </Button>
                <Button
                  type="danger"
                  disabled={!!busy}
                  loading={busy === `off:${conn.serverId}`}
                  onClick={() => void onDisconnect(conn.serverId)}
                >
                  {t('mcp.disconnect')}
                </Button>
              </div>
            </div>
          ))}
        </div>
      )}

      <div style={sectionGap}>
        <Typography.Title heading={5} style={{ margin: 0 }}>
          {t('mcp.catalogSection')}
        </Typography.Title>
        {grouped.map(([group, items]) => (
          <div key={group} style={sectionGap}>
            <Typography.Text strong>
              {GROUP_KEYS[group] ? t(GROUP_KEYS[group]) : group}
            </Typography.Text>
            {items.map((item) => {
              const conn = byServer.get(item.id)
              return (
                <div key={item.id} style={cardStyle} data-testid={`mcp-server-${item.id}`}>
                  <div style={rowStyle}>
                    <Typography.Text strong>{item.name}</Typography.Text>
                    <Tag>{t(AUTH_KEYS[item.auth] ?? 'mcp.authOauth')}</Tag>
                    {conn && statusTag(conn)}
                    <a href={item.docsUrl} target="_blank" rel="noreferrer">
                      {t('mcp.docs')}
                    </a>
                  </div>
                  <Typography.Text type="tertiary" size="small">
                    {item.description}
                  </Typography.Text>
                  {!conn && (
                    <div style={rowStyle}>
                      {!usesKey(item) && (
                        <Button
                          theme="solid"
                          type="primary"
                          loading={busy === `oauth:${item.id}`}
                          disabled={!!busy}
                          onClick={() => void onConnectOAuth(item.id)}
                        >
                          {t('mcp.connectOauth')}
                        </Button>
                      )}
                      {item.auth === 'both' && (
                        <Button
                          disabled={!!busy}
                          onClick={() =>
                            setPreferKey((prev) => ({
                              ...prev,
                              [item.id]: !usesKey(item),
                            }))
                          }
                        >
                          {usesKey(item) ? t('mcp.useOauth') : t('mcp.useApiKey')}
                        </Button>
                      )}
                    </div>
                  )}
                  {!conn && usesKey(item) && keyForm(item.id)}
                </div>
              )
            })}
          </div>
        ))}
      </div>
    </div>
  )
}
