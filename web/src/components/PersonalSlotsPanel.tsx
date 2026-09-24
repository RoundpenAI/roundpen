import { useCallback, useEffect, useState } from 'react'
import { Banner, Input, Select, Typography } from '@douyinfe/semi-ui-19'
import {
  settingItems,
  type ItemKind,
  type SettingItem,
  type SlotDef,
  type UserBindingEntry,
} from '../api'
import { useT } from '../i18n'
import { slotDescription, slotLabel } from '../lib/settingItemLabels'
import { Loading } from './Loading'

/**
 * Personal overrides for every user-selectable slot: pick your own provider,
 * sandbox source or search backend; "Inherit" keeps the platform default.
 * Slots that feed a sandbox report the rebuild that applies the change.
 */
export function PersonalSlotsPanel() {
  const t = useT()
  const [slots, setSlots] = useState<Record<string, UserBindingEntry>>({})
  const [defs, setDefs] = useState<SlotDef[]>([])
  const [items, setItems] = useState<Record<string, SettingItem[]>>({})
  const [models, setModels] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [notice, setNotice] = useState<string | null>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      const schema = await settingItems.schema()
      const mine = schema.slots.filter((s) => s.userOverride)
      setDefs(mine)
      const bindings = await settingItems.userBindings()
      setSlots(bindings.slots ?? {})
      const overrides: Record<string, string> = {}
      for (const slot of mine) {
        overrides[slot.key] = String(
          (bindings.slots?.[slot.key]?.params?.model as string) ?? '',
        )
      }
      setModels(overrides)
      const kinds = [...new Set(mine.map((s) => s.kind))] as ItemKind[]
      const loaded: Record<string, SettingItem[]> = {}
      for (const kind of kinds) {
        loaded[kind] = (await settingItems.userItems(kind)).items ?? []
      }
      setItems(loaded)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('personalSlots.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  async function save(slot: SlotDef, itemId: string, model: string) {
    setBusy(true)
    setError(null)
    setNotice(null)
    try {
      const params = model.trim() ? { model: model.trim() } : undefined
      const res =
        itemId === ''
          ? await settingItems.clearUserBinding(slot.key)
          : await settingItems.setUserBinding(slot.key, itemId, params)
      if (res.rebuildError) {
        setError(`${t('agentEnv.proxy.rebuildFailed')} ${res.rebuildError}`)
      } else if (res.status && res.status !== 'absent') {
        setNotice(t('personalSlots.rebuilt'))
      } else if (res.status === 'absent') {
        // No sandbox to rebuild: the selection applies on next creation.
        setNotice(t('personalSlots.saved'))
      }
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('personalSlots.saveFailed'))
      await load()
    } finally {
      setBusy(false)
    }
  }

  if (loading) return <Loading tip={t('personalSlots.loading')} />

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      <Typography.Title heading={5}>{t('personalSlots.title')}</Typography.Title>
      <Typography.Text type="tertiary">{t('personalSlots.intro')}</Typography.Text>
      {error && <Banner type="danger" description={error} />}
      {notice && <Banner type="success" description={notice} />}
      {defs.map((slot) => {
        const entry = slots[slot.key]
        const chosen = entry?.itemId ?? ''
        const options = items[slot.kind] ?? []
        const hasModel = (slot.bindingFields ?? []).some((f) => f.key === 'model')
        return (
          <div
            key={slot.key}
            role="group"
            aria-label={slotLabel(slot, t)}
            style={{ display: 'flex', flexDirection: 'column', gap: 8 }}
          >
            <Typography.Text strong>{slotLabel(slot, t)}</Typography.Text>
            <Typography.Text type="tertiary" size="small">
              {slotDescription(slot, t)}
            </Typography.Text>
            <Select
              style={{ maxWidth: 360 }}
              disabled={busy}
              value={chosen}
              onChange={(v) => void save(slot, String(v), models[slot.key] ?? '')}
              optionList={[
                {
                  value: '',
                  label: entry?.effectiveItemId
                    ? `${t('personalSlots.inherit')} — ${entry.effectiveItemId}`
                    : t('personalSlots.inherit'),
                },
                ...options.map((i) => ({ value: i.id, label: i.name || i.id })),
              ]}
            />
            {hasModel && chosen !== '' && (
              <Input
                style={{ maxWidth: 360 }}
                autoComplete="off"
                placeholder={t('personalSlots.modelPlaceholder')}
                value={models[slot.key] ?? ''}
                onChange={(v) => setModels((p) => ({ ...p, [slot.key]: v }))}
                onBlur={() => void save(slot, chosen, models[slot.key] ?? '')}
              />
            )}
          </div>
        )
      })}
    </div>
  )
}
