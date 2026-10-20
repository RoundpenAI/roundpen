import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { Banner, Button, Modal, Tag, Typography } from '@douyinfe/semi-ui-19'
import {
  llmgwVendors,
  settingItems,
  type ItemKind,
  type KindDef,
  type LlmgwVendor,
  type SettingItem,
} from '../../../api'
import type { MessageKey } from '../../../i18n'
import { Loading } from '../../../components/Loading'
import { sectionGap } from '../constants'
import type { Translate } from '../helpers'
import { ItemEditorModal } from './ItemEditorModal'

type Props = {
  kind: ItemKind
  introKey: MessageKey
  noteKey?: MessageKey
  t: Translate
  /** Optional per-kind action rendered on each row (e.g. a connection test). */
  renderItemAction?: (item: SettingItem) => ReactNode
  /** Extra tags next to the item name (e.g. 「已启动」). */
  renderItemTags?: (item: SettingItem, meta: { isDefault: boolean }) => ReactNode
  /** Slot whose binding marks the default item in the list. */
  defaultSlot?: string
  /** Extra slots bound to the same item when setting the default. */
  alsoBindSlots?: string[]
  /** When binding, send the item's defaultModel as the slot model param. */
  bindModelFromItem?: boolean
  /** Replace the default "Add" button (e.g. preset picker). */
  renderAdd?: (openCreate: (seed?: Partial<SettingItem>) => void) => ReactNode
  /** Notify parent after items reload. */
  onItemsChange?: (items: SettingItem[]) => void
}

const rowStyle = {
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'space-between',
  gap: 12,
  flexWrap: 'wrap' as const,
  border: '1px solid var(--semi-color-border)',
  borderRadius: 8,
  padding: '8px 12px',
}

const rowInfoStyle = { display: 'flex', flexDirection: 'column' as const, gap: 2, minWidth: 0 }

/**
 * Admin editor for one kind's items: a summary list with add/edit in an
 * overlay so the page stays scannable. Each item saves on its own, matching
 * the per-item API (unlike the whole-document settings form this panel sits
 * in).
 */
export function ItemListSection({
  kind,
  introKey,
  noteKey,
  t,
  renderItemAction,
  renderItemTags,
  defaultSlot,
  alsoBindSlots = [],
  bindModelFromItem,
  renderAdd,
  onItemsChange,
}: Props) {
  const [def, setDef] = useState<KindDef | null>(null)
  const [items, setItems] = useState<SettingItem[]>([])
  const [refs, setRefs] = useState<Record<string, SettingItem[]>>({})
  const [vendors, setVendors] = useState<LlmgwVendor[]>([])
  const [defaultItemId, setDefaultItemId] = useState<string>('')
  const [editing, setEditing] = useState<SettingItem | null>(null)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)
  // Keep the latest callback without putting it in load()'s deps — an inline
  // parent handler would otherwise re-fetch forever.
  const onItemsChangeRef = useRef(onItemsChange)
  onItemsChangeRef.current = onItemsChange

  const load = useCallback(async () => {
    setError(null)
    try {
      const [schema, listed, vendorList, bindings] = await Promise.all([
        settingItems.schema(),
        settingItems.list(kind),
        kind === 'llm' ? llmgwVendors().catch(() => [] as LlmgwVendor[]) : Promise.resolve([]),
        defaultSlot
          ? settingItems.bindings().catch(() => ({ bindings: [], slots: [], kinds: [] }))
          : Promise.resolve({ bindings: [], slots: [], kinds: [] }),
      ])
      setVendors(vendorList ?? [])
      const kindDef = schema.kinds.find((k) => k.kind === kind) ?? null
      setDef(kindDef)
      const nextItems = listed.items ?? []
      setItems(nextItems)
      onItemsChangeRef.current?.(nextItems)
      if (defaultSlot) {
        const entry = (bindings.bindings ?? []).find((b) => b.slot === defaultSlot)
        setDefaultItemId(entry?.itemId ?? '')
      }
      const referenced = new Set(
        (kindDef?.fields ?? []).filter((f) => f.type === 'itemRef').map((f) => f.refKind),
      )
      const loadedRefs: Record<string, SettingItem[]> = {}
      for (const refKind of referenced) {
        if (!refKind) continue
        try {
          loadedRefs[refKind] = (await settingItems.list(refKind)).items ?? []
        } catch {
          loadedRefs[refKind] = []
        }
      }
      setRefs(loadedRefs)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('settings.items.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [kind, t, defaultSlot])

  useEffect(() => {
    void load()
  }, [load])

  function blank(seed?: Partial<SettingItem>): SettingItem {
    return {
      kind,
      id: seed?.id ?? '',
      name: seed?.name ?? '',
      description: seed?.description ?? '',
      enabled: seed?.enabled ?? true,
      position: seed?.position ?? items.length,
      config: seed?.config ?? {},
    }
  }

  function openCreate(seed?: Partial<SettingItem>) {
    setEditing(blank(seed))
  }

  async function save(item: SettingItem) {
    setError(null)
    setNotice(null)
    await settingItems.save(item)
    setNotice(t('settings.items.saved'))
    await load()
  }

  async function setDefault(item: SettingItem) {
    if (!defaultSlot || !item.enabled) return
    setBusy(true)
    setError(null)
    setNotice(null)
    try {
      const model =
        bindModelFromItem && typeof item.config.defaultModel === 'string'
          ? item.config.defaultModel.trim()
          : ''
      const params = model ? { model } : undefined
      const slots = [defaultSlot, ...alsoBindSlots]
      for (const slot of slots) {
        await settingItems.setBinding(slot, item.id, params)
      }
      setDefaultItemId(item.id)
      setNotice(t('settings.items.defaultSet'))
    } catch (e) {
      setError(e instanceof Error ? e.message : t('settings.items.defaultFailed'))
    } finally {
      setBusy(false)
    }
  }

  async function remove(item: SettingItem) {
    setError(null)
    setNotice(null)
    setBusy(true)
    try {
      await settingItems.remove(item.kind, item.id)
      await load()
    } catch (e) {
      // An item still selected by a slot answers 409: confirm the cascade.
      if (e instanceof Error && /bound to slots/i.test(e.message)) {
        setBusy(false)
        Modal.confirm({
          title: t('settings.items.remove'),
          content: e.message,
          onOk: async () => {
            setBusy(true)
            try {
              await settingItems.remove(item.kind, item.id, true)
              await load()
            } catch (err) {
              setError(err instanceof Error ? err.message : t('settings.items.deleteFailed'))
            } finally {
              setBusy(false)
            }
          },
        })
        return
      }
      setError(e instanceof Error ? e.message : t('settings.items.deleteFailed'))
    } finally {
      setBusy(false)
    }
  }

  if (loading) return <Loading tip={t('settings.loadingSystem')} />
  if (!def) {
    return <Banner type="warning" description={t('settings.items.unknownKind')} />
  }

  const effectiveDefault =
    defaultItemId ||
    (items.filter((i) => i.enabled).length === 1
      ? items.find((i) => i.enabled)?.id ?? ''
      : '')

  return (
    <div style={sectionGap}>
      <Typography.Text type="tertiary">{t(introKey)}</Typography.Text>
      {error && <Banner type="danger" description={error} />}
      {notice && <Banner type="success" description={notice} />}
      {items.length === 0 && (
        <Typography.Text type="tertiary">{t('settings.items.empty')}</Typography.Text>
      )}
      {items.map((item) => {
        const isDefault = Boolean(effectiveDefault) && item.id === effectiveDefault
        return (
          <div key={item.id} role="group" aria-label={item.name || item.id} style={rowStyle}>
            <div style={rowInfoStyle}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
                <Typography.Text strong>{item.name || item.id}</Typography.Text>
                {/* A nameless item shows its id as the title, so skip the echo. */}
                {item.name && item.name !== item.id && (
                  <Typography.Text
                    type="tertiary"
                    size="small"
                    style={{ fontFamily: 'var(--semi-font-family-code)' }}
                  >
                    {item.id}
                  </Typography.Text>
                )}
                {isDefault && (
                  <Tag color="blue" size="small">
                    {t('settings.items.default')}
                  </Tag>
                )}
                {!item.enabled && (
                  <Tag color="grey" size="small">
                    {t('settings.items.disabled')}
                  </Tag>
                )}
                {renderItemTags?.(item, { isDefault })}
              </div>
              {kind === 'llm' && typeof item.config.defaultModel === 'string' && item.config.defaultModel && (
                <Typography.Text type="tertiary" size="small">
                  {item.config.vendor && item.config.vendor !== 'custom'
                    ? `${String(item.config.vendor)} · ${item.config.defaultModel}`
                    : item.config.defaultModel}
                </Typography.Text>
              )}
              {item.description && (
                <Typography.Text type="tertiary" size="small">
                  {item.description}
                </Typography.Text>
              )}
            </div>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, flexWrap: 'wrap' }}>
              {renderItemAction?.(item)}
              {defaultSlot && item.enabled && !isDefault && (
                <Button
                  size="small"
                  theme="borderless"
                  loading={busy}
                  onClick={() => void setDefault(item)}
                >
                  {t('settings.items.setDefault')}
                </Button>
              )}
              <Button size="small" theme="borderless" onClick={() => setEditing(item)}>
                {t('settings.items.edit')}
              </Button>
              <Button
                size="small"
                type="danger"
                theme="borderless"
                loading={busy}
                onClick={() => void remove(item)}
              >
                {t('settings.items.remove')}
              </Button>
            </div>
          </div>
        )
      })}
      <div>
        {renderAdd ? (
          renderAdd(openCreate)
        ) : (
          <Button theme="solid" onClick={() => openCreate()}>
            {t('settings.items.add')}
          </Button>
        )}
      </div>
      {noteKey && (
        <Typography.Text type="tertiary" size="small">
          {t(noteKey)}
        </Typography.Text>
      )}
      <ItemEditorModal
        kind={kind}
        def={def}
        refs={refs}
        vendors={vendors}
        t={t}
        item={editing}
        onSave={save}
        onClose={() => setEditing(null)}
      />
    </div>
  )
}
