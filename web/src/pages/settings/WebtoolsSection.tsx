import { useCallback, useEffect, useRef, useState } from 'react'
import { Banner, Button, Typography } from '@douyinfe/semi-ui-19'
import { Link } from 'react-router-dom'
import {
  llmgwVendors,
  mcp,
  settingItems,
  type LlmgwVendor,
  type SettingItem,
} from '../../api'
import { Loading } from '../../components/Loading'
import { ItemListSection } from './items/ItemListSection'
import { sectionGap } from './constants'
import type { SectionBase } from './helpers'

type SearchSource =
  | { kind: 'vendor'; name: string }
  | { kind: 'search'; name: string }
  | { kind: 'mcp'; name: string }
  | null

function hasSearchKey(item: SettingItem): boolean {
  const key = item.config.apiKey
  // Masked secrets still count as configured.
  return typeof key === 'string' && key.trim() !== ''
}

function resolveVendorSearch(
  items: SettingItem[],
  defaultId: string,
  vendors: LlmgwVendor[],
): SearchSource {
  const enabled = items.filter((i) => i.enabled)
  const target =
    (defaultId && enabled.find((i) => i.id === defaultId)) ||
    (enabled.length === 1 ? enabled[0] : undefined)
  if (!target) return null
  if (target.id === 'roundpen-free' || target.config.authMode === 'install_identity') {
    return null
  }
  const vendorId = typeof target.config.vendor === 'string' ? target.config.vendor : ''
  const vendor = vendors.find((v) => v.id === vendorId)
  if (!vendor?.search) return null
  // Masked or plaintext both mean a key is stored; missing/empty means none.
  const key = target.config.apiKey
  if (typeof key !== 'string' || key.trim() === '') return null
  return { kind: 'vendor', name: vendor.name || vendor.id }
}

function sameSource(a: SearchSource, b: SearchSource): boolean {
  if (a === b) return true
  if (!a || !b) return false
  return a.kind === b.kind && a.name === b.name
}

// Search backends are setting items (kind "search"); each saves on its own.
// When model config or a connector already supplies search, surface that and
// only suggest Tavily / Exa when nothing is detected.
export function WebtoolsSection({ isAdmin, loading, t }: SectionBase) {
  const [source, setSource] = useState<SearchSource>(null)
  const [detecting, setDetecting] = useState(true)
  const initialDetect = useRef(true)

  const detect = useCallback(async () => {
    if (initialDetect.current) setDetecting(true)
    try {
      const [searchListed, llmListed, vendors, bindings, catalog] = await Promise.all([
        settingItems.list('search'),
        settingItems.list('llm'),
        llmgwVendors().catch(() => [] as LlmgwVendor[]),
        settingItems.bindings().catch(() => ({ bindings: [] as { slot: string; itemId?: string }[] })),
        mcp.catalog().catch(() => ({ servers: [] })),
      ])

      let next: SearchSource = null
      const searchItems = (searchListed.items ?? []).filter((i) => i.enabled)
      const withKey = searchItems.find((i) => hasSearchKey(i) || String(i.config.endpoint ?? '').trim())
      if (withKey) {
        next = { kind: 'search', name: withKey.name || withKey.id }
      } else {
        const defaultId =
          bindings.bindings?.find((b) => b.slot === 'llm.default')?.itemId ||
          bindings.bindings?.find((b) => b.slot === 'llm.sysagent')?.itemId ||
          ''
        const fromVendor = resolveVendorSearch(llmListed.items ?? [], defaultId, vendors ?? [])
        if (fromVendor) {
          next = fromVendor
        } else {
          const exa = (catalog.servers ?? []).find((s) => s.id === 'exa' && s.connected)
          if (exa) next = { kind: 'mcp', name: exa.name || 'Exa' }
        }
      }

      setSource((prev) => (sameSource(prev, next) ? prev : next))
    } catch {
      setSource((prev) => (prev === null ? prev : null))
    } finally {
      initialDetect.current = false
      setDetecting(false)
    }
  }, [])

  useEffect(() => {
    if (!isAdmin || loading) return
    void detect()
  }, [isAdmin, loading, detect])

  // Stable identity so ItemListSection's load() does not re-run every render.
  const onItemsChange = useCallback(() => {
    void detect()
  }, [detect])

  if (!isAdmin) return null
  if (loading) return <Loading tip={t('settings.loadingSystem')} />

  return (
    <div style={{ ...sectionGap, paddingTop: 16 }}>
      {detecting ? (
        <Typography.Text type="tertiary" size="small">
          {t('settings.webtools.detecting')}
        </Typography.Text>
      ) : source ? (
        <Banner
          fullMode={false}
          type="success"
          closeIcon={null}
          description={
            source.kind === 'vendor'
              ? t('settings.webtools.detectedVendor', { name: source.name })
              : source.kind === 'mcp'
                ? t('settings.webtools.detectedMcp', { name: source.name })
                : t('settings.webtools.detectedSearch', { name: source.name })
          }
        />
      ) : (
        <div
          style={{
            ...sectionGap,
            padding: 12,
            background: 'var(--semi-color-fill-0)',
            borderRadius: 8,
          }}
        >
          <Typography.Text strong size="small">
            {t('settings.webtools.suggestTitle')}
          </Typography.Text>
          <Typography.Text type="tertiary" size="small">
            {t('settings.webtools.suggestHint')}
          </Typography.Text>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
            <Typography.Text>
              <strong>Tavily</strong> — {t('settings.webtools.suggestTavily')}
            </Typography.Text>
          </div>
          <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, alignItems: 'center' }}>
            <Typography.Text>
              <strong>Exa</strong> — {t('settings.webtools.suggestExa')}
            </Typography.Text>
            <Link to="/settings/apps">
              <Button size="small" theme="light">
                {t('settings.webtools.openApps')}
              </Button>
            </Link>
          </div>
        </div>
      )}

      <ItemListSection
        kind="search"
        introKey="settings.webtools.intro"
        noteKey="settings.webtools.note"
        t={t}
        defaultSlot="search.default"
        onItemsChange={onItemsChange}
      />
    </div>
  )
}
