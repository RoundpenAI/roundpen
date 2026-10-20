import { useEffect, useMemo, useState } from 'react'
import { Banner, Table, Typography } from '@douyinfe/semi-ui-19'
import { adminSettings, type EnvVar } from '../../api'
import { useUiPreference } from '../../components/SemiAppProvider'
import { Loading } from '../../components/Loading'
import type { MessageKey } from '../../i18n'
import { sectionGap } from './constants'
import type { Translate } from './helpers'

type Props = {
  isAdmin: boolean
  t: Translate
}

const GROUP_ORDER = ['core', 'sandbox', 'browser', 'llm', 'preview', 'push', 'advanced'] as const

const GROUP_LABEL: Record<(typeof GROUP_ORDER)[number], MessageKey> = {
  core: 'settings.env.group.core',
  sandbox: 'settings.env.group.sandbox',
  browser: 'settings.env.group.browser',
  llm: 'settings.env.group.llm',
  preview: 'settings.env.group.preview',
  push: 'settings.env.group.push',
  advanced: 'settings.env.group.advanced',
}

export function EnvSection({ isAdmin, t }: Props) {
  const { preference } = useUiPreference()
  const [vars, setVars] = useState<EnvVar[]>([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    if (!isAdmin) return
    let alive = true
    setLoading(true)
    void adminSettings
      .envCatalog()
      .then((res) => {
        if (!alive) return
        setVars(res.vars ?? [])
        setError(null)
      })
      .catch((e) => {
        if (!alive) return
        setError(e instanceof Error ? e.message : t('settings.env.loadFailed'))
      })
      .finally(() => {
        if (alive) setLoading(false)
      })
    return () => {
      alive = false
    }
  }, [isAdmin, t])

  const byGroup = useMemo(() => {
    const map = new Map<string, EnvVar[]>()
    for (const v of vars) {
      const list = map.get(v.group) ?? []
      list.push(v)
      map.set(v.group, list)
    }
    return GROUP_ORDER.filter((g) => map.has(g)).map((g) => ({
      group: g,
      rows: map.get(g) ?? [],
    }))
  }, [vars])

  if (!isAdmin) return null
  if (loading) return <Loading tip={t('settings.loadingSystem')} />

  const zh = preference.locale.startsWith('zh')

  return (
    <div style={{ ...sectionGap, paddingTop: 16 }}>
      <Banner
        fullMode={false}
        type="info"
        closeIcon={null}
        description={t('settings.env.howTo')}
      />
      {error && <Banner fullMode={false} type="danger" closeIcon={null} description={error} />}
      {byGroup.map(({ group, rows }) => (
        <div key={group} style={sectionGap}>
          <Typography.Title heading={6} style={{ margin: 0 }}>
            {t(GROUP_LABEL[group])}
          </Typography.Title>
          <Table
            size="small"
            pagination={false}
            rowKey="name"
            dataSource={rows}
            columns={[
              {
                title: t('settings.env.col.name'),
                dataIndex: 'name',
                width: 280,
                render: (name: string) => (
                  <Typography.Text
                    style={{ fontFamily: 'var(--semi-font-family-code)', wordBreak: 'break-all' }}
                  >
                    {name}
                  </Typography.Text>
                ),
              },
              {
                title: t('settings.env.col.default'),
                dataIndex: 'default',
                width: 160,
                render: (v: string) => (
                  <Typography.Text type="tertiary" style={{ wordBreak: 'break-all' }}>
                    {v || '—'}
                  </Typography.Text>
                ),
              },
              {
                title: t('settings.env.col.value'),
                dataIndex: 'value',
                width: 180,
                render: (v: string) => (
                  <Typography.Text style={{ wordBreak: 'break-all' }}>{v || '—'}</Typography.Text>
                ),
              },
              {
                title: t('settings.env.col.hint'),
                dataIndex: 'hint',
                render: (_: string, row: EnvVar) => (
                  <Typography.Text type="tertiary" size="small">
                    {zh && row.hintZh ? row.hintZh : row.hint}
                  </Typography.Text>
                ),
              },
            ]}
          />
        </div>
      ))}
    </div>
  )
}
