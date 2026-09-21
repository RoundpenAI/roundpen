import { useEffect, useState } from 'react'
import { Banner, Button, Input, Modal, Switch, Typography } from '@douyinfe/semi-ui-19'
import type { ItemKind, KindDef, SettingItem } from '../../../api'
import { Field } from '../parts'
import type { Translate } from '../helpers'
import { ItemFields } from './ItemEditor'

type Props = {
  kind: ItemKind
  def: KindDef
  refs: Record<string, SettingItem[]>
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
export function ItemEditorModal({ kind, def, refs, t, item, onSave, onClose }: Props) {
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
    setDraft((prev) => (prev ? { ...prev, config: { ...prev.config, [key]: value } } : prev))
  }

  async function submit() {
    if (!draft) return
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
          <Field label={t('settings.items.name')}>
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
