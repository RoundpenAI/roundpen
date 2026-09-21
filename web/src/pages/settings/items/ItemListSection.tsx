import { useCallback, useEffect, useState, type ReactNode } from 'react'
import { Banner, Button, Input, Modal, Switch, Typography } from '@douyinfe/semi-ui-19'
import {
  settingItems,
  type ItemKind,
  type KindDef,
  type SettingItem,
} from '../../../api'
import type { MessageKey } from '../../../i18n'
import { Loading } from '../../../components/Loading'
import { sectionGap } from '../constants'
import { Field } from '../parts'
import type { Translate } from '../helpers'
import { ItemFields } from './ItemEditor'

type Props = {
  kind: ItemKind
  introKey: MessageKey
  noteKey?: MessageKey
  t: Translate
  /** Optional per-kind action rendered beside Save/Remove for one item. */
  renderItemAction?: (item: SettingItem) => ReactNode
}

const blankItem = (kind: ItemKind): SettingItem => ({
  kind,
  id: '',
  name: '',
  description: '',
  enabled: true,
  position: 0,
  config: {},
})

/**
 * Admin editor for one kind's items: list, create, edit, enable/disable and
 * delete. Each item saves on its own, matching the per-item API (unlike the
 * whole-document settings form this panel sits in).
 */
export function ItemListSection({ kind, introKey, noteKey, t, renderItemAction }: Props) {
  const [def, setDef] = useState<KindDef | null>(null)
  const [items, setItems] = useState<SettingItem[]>([])
  const [drafts, setDrafts] = useState<SettingItem[]>([])
  const [dirty, setDirty] = useState<boolean[]>([])
  const [refs, setRefs] = useState<Record<string, SettingItem[]>>({})
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
      const loaded = listed.items ?? []
      setItems(loaded)
      setDrafts(loaded.map((i) => ({ ...i, config: { ...i.config } })))
      setDirty(loaded.map(() => false))
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

  function patchAt(index: number, part: Partial<SettingItem>) {
    setDrafts((prev) => prev.map((d, i) => (i === index ? { ...d, ...part } : d)))
    setDirty((prev) => prev.map((d, i) => (i === index ? true : d)))
  }

  function patchConfig(index: number, key: string, value: unknown) {
    setDrafts((prev) =>
      prev.map((d, i) => (i === index ? { ...d, config: { ...d.config, [key]: value } } : d)),
    )
    setDirty((prev) => prev.map((d, i) => (i === index ? true : d)))
  }

  async function save(index: number) {
    setBusy(true)
    setError(null)
    setNotice(null)
    try {
      const res = await settingItems.save(drafts[index])
      setDrafts((prev) => prev.map((d, i) => (i === index ? { ...res.item, config: { ...res.item.config } } : d)))
      setDirty((prev) => prev.map((d, i) => (i === index ? false : d)))
      setNotice(t('settings.items.saved'))
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('settings.items.saveFailed'))
    } finally {
      setBusy(false)
    }
  }

  // A new item is a local draft until it is saved: the id is the admin's to
  // choose and the save call upserts whatever the form holds.
  function add() {
    setDrafts((prev) => [...prev, blankItem(kind)])
    setDirty((prev) => [...prev, true])
    setNotice(null)
  }

  async function remove(index: number) {
    const item = drafts[index]
    setError(null)
    setNotice(null)
    // An unsaved draft is not in the backend yet.
    if (!items.some((i) => i.id === item.id)) {
      setDrafts((prev) => prev.filter((_, i) => i !== index))
      setDirty((prev) => prev.filter((_, i) => i !== index))
      return
    }
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
    return (
      <Banner type="warning" description={t('settings.items.unknownKind')} />
    )
  }

  return (
    <div style={sectionGap}>
      <Typography.Text type="tertiary">{t(introKey)}</Typography.Text>
      {error && <Banner type="danger" description={error} />}
      {notice && <Banner type="success" description={notice} />}
      {drafts.length === 0 && (
        <Typography.Text type="tertiary">{t('settings.items.empty')}</Typography.Text>
      )}
      {drafts.map((draft, index) => (
        <div
          key={`${draft.id}-${index}`}
          role="group"
          aria-label={t('settings.items.group', { n: index + 1 })}
          style={{
            display: 'grid',
            gap: 16,
            gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))',
            alignItems: 'end',
            borderTop: '1px solid var(--semi-color-border)',
            paddingTop: 16,
          }}
        >
          <Field label={t('settings.items.id')} hint={t('settings.items.idHint')}>
            <Input
              autoComplete="off"
              placeholder="us-egress"
              value={draft.id}
              onChange={(v) => patchAt(index, { id: v })}
            />
          </Field>
          <Field label={t('settings.items.name')}>
            <Input
              autoComplete="off"
              value={draft.name}
              onChange={(v) => patchAt(index, { name: v })}
            />
          </Field>
          <Field label={t('settings.items.description')}>
            <Input
              autoComplete="off"
              value={draft.description ?? ''}
              onChange={(v) => patchAt(index, { description: v })}
            />
          </Field>
          <Field label={t('settings.items.enabled')}>
            <Switch
              checked={draft.enabled}
              onChange={(v) => patchAt(index, { enabled: v })}
            />
          </Field>
          <ItemFields
            fields={def.fields}
            config={draft.config}
            refs={refs}
            t={t}
            onChange={(key, value) => patchConfig(index, key, value)}
          />
          <div style={{ display: 'flex', gap: 8 }}>
            <Button
              theme="solid"
              loading={busy}
              disabled={!dirty[index]}
              onClick={() => void save(index)}
            >
              {t('settings.items.save')}
            </Button>
            <Button type="danger" loading={busy} onClick={() => void remove(index)}>
              {t('settings.items.remove')}
            </Button>
            {renderItemAction?.(draft)}
          </div>
        </div>
      ))}
      <div>
        <Button loading={busy} onClick={add}>
          {t('settings.items.add')}
        </Button>
      </div>
      {noteKey && (
        <Typography.Text type="tertiary" size="small">
          {t(noteKey)}
        </Typography.Text>
      )}
    </div>
  )
}

export type { Props as ItemListSectionProps }
