import { Input, InputNumber, Select, Switch, TextArea, Typography } from '@douyinfe/semi-ui-19'
import type { ItemField, SettingItem } from '../../../api'
import type { MessageKey } from '../../../i18n'
import { Field } from '../parts'
import type { Translate } from '../helpers'

type Props = {
  fields: ItemField[]
  config: Record<string, unknown>
  refs?: Record<string, SettingItem[]>
  t: Translate
  onChange: (key: string, value: unknown) => void
}

function labelFor(field: ItemField, t: Translate): string {
  const key = `settings.itemField.${field.key}` as MessageKey
  const translated = t(key)
  // The catalog falls back to the raw key when it has no entry, so an
  // untranslated field still shows the backend-provided label.
  return translated === key ? (field.label ?? field.key) : translated
}

/** Renders one item config value from its backend-declared field type. */
export function ItemFieldInput({ field, config, refs, t, onChange }: Props & { field: ItemField }) {
  const value = config[field.key]
  const label = labelFor(field, t)
  const hint = field.hint

  switch (field.type) {
    case 'bool':
      return (
        <Field label={label} hint={hint}>
          <Switch checked={Boolean(value)} onChange={(v) => onChange(field.key, v)} />
        </Field>
      )
    case 'int':
      return (
        <Field label={label} hint={hint}>
          <InputNumber
            value={typeof value === 'number' ? value : undefined}
            onChange={(v) => onChange(field.key, v ?? 0)}
          />
        </Field>
      )
    case 'enum':
      return (
        <Field label={label} hint={hint}>
          <Select
            value={typeof value === 'string' ? value : undefined}
            onChange={(v) => onChange(field.key, v)}
            optionList={(field.options ?? []).map((o) => ({ value: o, label: o }))}
          />
        </Field>
      )
    case 'itemRef': {
      const options = (refs?.[field.refKind ?? ''] ?? []).filter((i) => i.enabled)
      return (
        <Field label={label} hint={hint}>
          <Select
            value={typeof value === 'string' ? value : ''}
            onChange={(v) => onChange(field.key, v)}
            optionList={[{ value: '', label: t('settings.items.none') }].concat(
              options.map((i) => ({ value: i.id, label: i.name || i.id })),
            )}
          />
        </Field>
      )
    }
    case 'json':
      return (
        <Field label={label} hint={hint}>
          <TextArea
            autosize={{ minRows: 2, maxRows: 8 }}
            value={value === undefined ? '' : JSON.stringify(value, null, 2)}
            onChange={(v) => onChange(field.key, parseJSONField(v))}
          />
        </Field>
      )
    case 'secret':
      return (
        <Field label={label} hint={hint ?? t('settings.items.secretHint')}>
          <Input
            mode="password"
            autoComplete="new-password"
            value={typeof value === 'string' ? value : ''}
            onChange={(v) => onChange(field.key, v)}
          />
        </Field>
      )
    default:
      return (
        <Field label={label} hint={hint}>
          <Input
            autoComplete="off"
            placeholder={field.type === 'url' ? 'https://…' : undefined}
            value={typeof value === 'string' ? value : ''}
            onChange={(v) => onChange(field.key, v)}
          />
        </Field>
      )
  }
}

/** JSON textareas submit on every keystroke, so keep unparsable text as-is. */
function parseJSONField(raw: string): unknown {
  const text = raw.trim()
  if (text === '') return undefined
  try {
    return JSON.parse(text)
  } catch {
    return raw
  }
}

export function ItemFields({ fields, config, refs, t, onChange }: Props) {
  const visible = fields.filter((f) => !f.advanced)
  const advanced = fields.filter((f) => f.advanced)
  return (
    <>
      {visible.map((field) => (
        <ItemFieldInput
          key={field.key}
          field={field}
          fields={fields}
          config={config}
          refs={refs}
          t={t}
          onChange={onChange}
        />
      ))}
      {advanced.length > 0 && (
        <details>
          <Typography.Text type="tertiary" size="small">
            {t('settings.items.advanced')}
          </Typography.Text>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 16, paddingTop: 8 }}>
            {advanced.map((field) => (
              <ItemFieldInput
                key={field.key}
                field={field}
                fields={fields}
                config={config}
                refs={refs}
                t={t}
                onChange={onChange}
              />
            ))}
          </div>
        </details>
      )}
    </>
  )
}
