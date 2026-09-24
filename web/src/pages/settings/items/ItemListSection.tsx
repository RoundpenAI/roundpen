import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { Banner, Button, Modal, Tag, Typography } from '@douyinfe/semi-ui-19'
import {
  settingItems,
  type ItemKind,
  type KindDef,
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
export function ItemListSection({ kind, introKey, noteKey, t, renderItemAction }: Props) {
  const [def, setDef] = useState<KindDef | null>(null)
  const [items, setItems] = useState<SettingItem[]>([])
  const [refs, setRefs] = useState<Record<string, SettingItem[]>>({})
  const [editing, setEditing] = useState<SettingItem | null>(null)
  const [creating, setCreating] = useState(false)
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      const [schema, listed] = await Promise.all([
        settingItems.schema(),
        settingItems.list(kind),
      ])
      const kindDef = schema.kinds.find((k) => k.kind === kind) ?? null
      setDef(kindDef)
      setItems(listed.items ?? [])
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
  }, [kind, t])

  useEffect(() => {
    void load()
  }, [load])

  function blank(): SettingItem {
    return {
      kind,
      id: '',
      name: '',
      description: '',
      enabled: true,
      position: items.length,
      config: {},
    }
  }

  async function save(item: SettingItem) {
    setError(null)
    setNotice(null)
    await settingItems.save(item)
    setNotice(t('settings.items.saved'))
    await load()
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

  return (
    <div style={sectionGap}>
      <Typography.Text type="tertiary">{t(introKey)}</Typography.Text>
      {error && <Banner type="danger" description={error} />}
      {notice && <Banner type="success" description={notice} />}
      {items.length === 0 && (
        <Typography.Text type="tertiary">{t('settings.items.empty')}</Typography.Text>
      )}
      {items.map((item) => (
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
              {!item.enabled && (
                <Tag color="grey" size="small">
                  {t('settings.items.disabled')}
                </Tag>
              )}
            </div>
            {item.description && (
              <Typography.Text type="tertiary" size="small">
                {item.description}
              </Typography.Text>
            )}
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            {renderItemAction?.(item)}
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
      ))}
      <div>
        <Button theme="solid" onClick={() => setCreating(true)}>
          {t('settings.items.add')}
        </Button>
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
        t={t}
        item={editing ?? (creating ? blank() : null)}
        onSave={save}
        onClose={() => {
          setEditing(null)
          setCreating(false)
        }}
      />
    </div>
  )
}
