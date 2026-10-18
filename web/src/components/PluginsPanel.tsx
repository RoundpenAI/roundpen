import { useCallback, useEffect, useState, type CSSProperties } from 'react'
import { Banner, Button, Switch, Typography } from '@douyinfe/semi-ui-19'
import { plugins, type PluginStatus } from '../api'
import { useT } from '../i18n'
import type { MessageKey } from '../i18n/translate'

const sectionGap: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 16,
}

const cardStyle: CSSProperties = {
  display: 'flex',
  flexWrap: 'wrap',
  alignItems: 'flex-start',
  justifyContent: 'space-between',
  gap: 12,
  border: '1px solid var(--semi-color-border)',
  borderRadius: 8,
  padding: 12,
}

const NAME_KEYS: Record<string, MessageKey> = {
  'self-hosted-git': 'plugins.selfHostedGit.name',
}

const DESC_KEYS: Record<string, MessageKey> = {
  'self-hosted-git': 'plugins.selfHostedGit.desc',
}

export function PluginsPanel() {
  const t = useT()
  const [list, setList] = useState<PluginStatus[]>([])
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [loaded, setLoaded] = useState(false)

  const load = useCallback(async () => {
    setError(null)
    try {
      const res = await plugins.list()
      setList(res.plugins ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : t('plugins.loadFailed'))
    } finally {
      setLoaded(true)
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  async function toggle(p: PluginStatus, enabled: boolean) {
    setBusy(p.id)
    setError(null)
    try {
      await plugins.setEnabled(p.id, enabled)
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('plugins.saveFailed'))
    } finally {
      setBusy(null)
    }
  }

  return (
    <section style={sectionGap}>
      <Typography.Title heading={5} style={{ margin: 0 }}>
        {t('plugins.title')}
      </Typography.Title>
      <Typography.Text type="tertiary" size="small">
        {t('plugins.desc')}
      </Typography.Text>
      {error ? (
        <div role="alert">
          <Banner fullMode={false} type="danger" description={error} closeIcon={null} />
        </div>
      ) : null}
      {list.length > 0 ? (
        <ul style={{ listStyle: 'none', margin: 0, padding: 0, display: 'flex', flexDirection: 'column', gap: 8 }}>
          {list.map((p) => {
            const nameKey = NAME_KEYS[p.id]
            const descKey = DESC_KEYS[p.id]
            return (
              <li key={p.id} style={cardStyle}>
                <div style={{ minWidth: 0, flex: 1 }}>
                  <Typography.Text strong>
                    {nameKey ? t(nameKey) : p.name}
                  </Typography.Text>
                  <Typography.Text
                    type="tertiary"
                    size="small"
                    style={{ display: 'block', marginTop: 4 }}
                  >
                    {descKey ? t(descKey) : p.description}
                  </Typography.Text>
                </div>
                <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <Switch
                    checked={p.enabled}
                    disabled={busy === p.id}
                    onChange={(v: boolean) => void toggle(p, v)}
                    aria-label={nameKey ? t(nameKey) : p.name}
                  />
                  <Button
                    size="small"
                    loading={busy === p.id}
                    onClick={() => void toggle(p, !p.enabled)}
                  >
                    {p.enabled ? t('plugins.disable') : t('plugins.enable')}
                  </Button>
                </div>
              </li>
            )
          })}
        </ul>
      ) : loaded && !error ? (
        <Typography.Text type="tertiary" size="small">
          {t('plugins.empty')}
        </Typography.Text>
      ) : null}
    </section>
  )
}
