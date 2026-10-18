import { useEffect, useState } from 'react'
import { Banner, Button, Input, Modal, Switch, Typography } from '@douyinfe/semi-ui-19'
import type { ItemKind, KindDef, LlmgwVendor, SettingItem } from '../../../api'
import { fieldLabel } from '../../../lib/settingItemLabels'
import { Field } from '../parts'
import type { Translate } from '../helpers'
import { ItemFields } from './ItemEditor'

// Mirrors the server's slug rules, so an id it would reject never leaves the
// browser (and a value it would accept — "My proxy" — is not blocked here).
function slugify(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._]+/g, '-')
    .replace(/^[-._]+|[-._]+$/g, '')
    .slice(0, 64)
    .replace(/[-._]+$/, '')
}

// validate reports the first problem the server would reject, in the user's
// language, so a missing field is answered without a round-trip.
function validate(
  kind: ItemKind,
  def: KindDef,
  draft: SettingItem,
  vendors: LlmgwVendor[] | undefined,
  t: Translate,
): string | null {
  if (!slugify(draft.id)) return t('settings.items.idRequired')
  if (kind === 'llm') {
    const vendorId = String(draft.config.vendor ?? 'custom')
    const preset = vendors?.find((v) => v.id === vendorId)
    const builtin = Boolean(preset && preset.id !== 'custom' && preset.models.length > 0)
    if (!builtin) {
      if (!String(draft.config.protocol ?? '').trim()) {
        return t('settings.items.fieldRequired', { field: t('settings.itemField.protocol') })
      }
      if (!String(draft.config.baseUrl ?? '').trim()) {
        return t('settings.items.fieldRequired', { field: t('settings.itemField.baseUrl') })
      }
    } else if (!String(draft.config.defaultModel ?? '').trim()) {
      return t('settings.items.fieldRequired', { field: t('settings.llmgw.model') })
    }
  }
  for (const f of def.fields) {
    const value = draft.config[f.key]
    const label = fieldLabel(kind, f, t)
    if (f.required && (value === undefined || value === null || (typeof value === 'string' && !value.trim()))) {
      return t('settings.items.fieldRequired', { field: label })
    }
    if (f.type === 'json' && typeof value === 'string' && value.trim() !== '') {
      try {
        JSON.parse(value)
      } catch {
        return t('settings.items.jsonInvalid', { field: label })
      }
    }
  }
  return null
}

type Props = {
  kind: ItemKind
  def: KindDef
  refs: Record<string, SettingItem[]>
  vendors?: LlmgwVendor[]
  t: Translate
  /** The item to edit, a blank one to create, or null when closed. */
  item: SettingItem | null
  onSave: (item: SettingItem) => Promise<void>
  onClose: () => void
}

/**
 * Item form in an overlay: the list stays a scannable summary and the fields
 * (including a kind's advanced ones) only appear while editing. A save failure
 * — validation from the backend — is shown here rather than behind the modal.
 */
export function ItemEditorModal({ kind, def, refs, vendors, t, item, onSave, onClose }: Props) {
  const [draft, setDraft] = useState<SettingItem | null>(item)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    setDraft(item)
    setError(null)
  }, [item])

  const isNew = Boolean(item) && !item?.id
  const open = draft !== null

  function patch(part: Partial<SettingItem>) {
    setDraft((prev) => (prev ? { ...prev, ...part } : prev))
  }

  function patchConfig(key: string, value: unknown) {
    setDraft((prev) => {
      if (!prev) return prev
      const config = { ...prev.config, [key]: value }
      let id = prev.id
      if (kind === 'llm' && key === 'vendor' && typeof value === 'string') {
        const preset = vendors?.find((v) => v.id === value)
        if (preset && preset.id !== 'custom') {
          config.protocol = preset.protocol
          config.baseUrl = preset.baseUrl
          const ids = preset.models.map((m) => m.id)
          if (!ids.includes(String(config.defaultModel ?? ''))) {
            config.defaultModel = ids[0] ?? ''
          }
          if (!id) id = preset.id
        }
      }
      return { ...prev, id, config }
    })
  }

  async function submit() {
    if (!draft) return
    const problem = validate(kind, def, draft, vendors, t)
    if (problem) {
      setError(problem)
      return
    }
    setBusy(true)
    setError(null)
    try {
      await onSave(draft)
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('settings.items.saveFailed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      title={isNew ? t('settings.items.newTitle') : t('settings.items.editTitle')}
      visible={open}
      onCancel={() => {
        if (!busy) onClose()
      }}
      footer={null}
      maskClosable={!busy}
      closeOnEsc={!busy}
      width={520}
    >
      {draft && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
          {error && <Banner type="danger" description={error} />}
          <Field
            label={t('settings.items.id')}
            hint={isNew ? t('settings.items.idHint') : t('settings.items.idLocked')}
          >
            <Input
              autoComplete="off"
              placeholder="us-egress"
              disabled={!isNew}
              value={draft.id}
              onChange={(v) => patch({ id: v })}
            />
          </Field>
          <Field label={t('settings.items.name')} hint={t('settings.items.nameHint')}>
            <Input autoComplete="off" value={draft.name} onChange={(v) => patch({ name: v })} />
          </Field>
          <Field label={t('settings.items.description')}>
            <Input
              autoComplete="off"
              value={draft.description ?? ''}
              onChange={(v) => patch({ description: v })}
            />
          </Field>
          <Field label={t('settings.items.enabled')}>
            <Switch checked={draft.enabled} onChange={(v) => patch({ enabled: v })} />
          </Field>
          <ItemFields
            kind={kind}
            fields={def.fields}
            config={draft.config}
            refs={refs}
            vendors={vendors}
            t={t}
            onChange={patchConfig}
          />
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
            <Button onClick={onClose} disabled={busy}>
              {t('settings.items.cancel')}
            </Button>
            <Button theme="solid" loading={busy} onClick={() => void submit()}>
              {t('settings.items.save')}
            </Button>
          </div>
          <Typography.Text type="tertiary" size="small">
            {t('settings.items.kindHint', { kind })}
          </Typography.Text>
        </div>
      )}
    </Modal>
  )
}
