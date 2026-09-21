import { useCallback, useEffect, useState } from 'react'
import { Banner, Select, Typography } from '@douyinfe/semi-ui-19'
import {
  settingItems,
  type ItemKind,
  type SettingItem,
  type SettingBinding,
  type SlotDef,
} from '../../../api'
import type { MessageKey } from '../../../i18n'
import { Loading } from '../../../components/Loading'
import { sectionGap } from '../constants'
import { Field } from '../parts'
import type { Translate } from '../helpers'

type Props = {
  kind: ItemKind
  t: Translate
}

// The catalog falls back to the raw key, so compare against it to fall back to
// the backend-provided slot name.
function slotLabel(slot: SlotDef, t: Translate): string {
  const key = `settings.slotName.${slot.key}` as MessageKey
  const translated = t(key)
  return translated === key ? slot.name : translated
}

/**
 * Admin defaults for a kind's slots: each usage point picks one item, or
 * "inherit" to leave it unset (resolution then falls through to the slot's
 * default). Saved immediately, like the item list beside it.
 */
export function SlotBindings({ kind, t }: Props) {
  const [slots, setSlots] = useState<SlotDef[]>([])
  const [chosen, setChosen] = useState<Record<string, string>>({})
  const [items, setItems] = useState<SettingItem[]>([])
  const [loading, setLoading] = useState(true)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      const [bindings, listed] = await Promise.all([
        settingItems.bindings(),
        settingItems.list(kind),
      ])
      const mine = (bindings.slots ?? []).filter((s) => s.kind === kind)
      setSlots(mine)
      const current: Record<string, string> = {}
      for (const slot of mine) {
        const entry = (bindings.bindings ?? []).find(
          (b: SettingBinding) => b.slot === slot.key,
        )
        current[slot.key] = entry?.itemId ?? ''
      }
      setChosen(current)
      setItems(listed.items ?? [])
    } catch (e) {
      setError(e instanceof Error ? e.message : t('settings.slots.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [kind, t])

  useEffect(() => {
    void load()
  }, [load])

  async function select(slot: string, itemId: string) {
    setBusy(true)
    setError(null)
    try {
      if (itemId === '') {
        await settingItems.clearBinding(slot)
      } else {
        await settingItems.setBinding(slot, itemId)
      }
      setChosen((prev) => ({ ...prev, [slot]: itemId }))
    } catch (e) {
      setError(e instanceof Error ? e.message : t('settings.slots.saveFailed'))
      await load()
    } finally {
      setBusy(false)
    }
  }

  if (loading) return <Loading tip={t('settings.loadingSystem')} />
  if (slots.length === 0) return null

  return (
    <div style={sectionGap}>
      <Typography.Text type="tertiary">{t('settings.slots.intro')}</Typography.Text>
      {error && <Banner type="danger" description={error} />}
      {slots.map((slot) => (
        <Field
          key={slot.key}
          label={slotLabel(slot, t)}
          hint={slot.description}
        >
          <Select
            style={{ maxWidth: 360 }}
            disabled={busy}
            value={chosen[slot.key] ?? ''}
            onChange={(v) => void select(slot.key, String(v))}
            optionList={[
              { value: '', label: t('settings.slots.inherit') },
              ...items
                .filter((i) => i.enabled)
                .map((i) => ({ value: i.id, label: i.name || i.id })),
            ]}
          />
        </Field>
      ))}
    </div>
  )
}
