import { Input, InputNumber, Select, Switch, TextArea, Typography } from '@douyinfe/semi-ui-19'
import type { ItemField, ItemKind, LlmgwVendor, SettingItem } from '../../../api'
import { Field } from '../parts'
import { fieldHint, fieldLabel, optionDescription, optionLabel } from '../../../lib/settingItemLabels'
import type { Translate } from '../helpers'

type Props = {
  kind: ItemKind
  fields: ItemField[]
  config: Record<string, unknown>
  refs?: Record<string, SettingItem[]>
  vendors?: LlmgwVendor[]
  t: Translate
  onChange: (key: string, value: unknown) => void
}

function vendorOf(config: Record<string, unknown>, vendors?: LlmgwVendor[]): LlmgwVendor | undefined {
  const id = typeof config.vendor === 'string' && config.vendor !== '' ? config.vendor : 'custom'
  return vendors?.find((v) => v.id === id)
}

/** Renders one item config value from its backend-declared field type. */
export function ItemFieldInput({ kind, field, config, refs, t, onChange }: Props & { field: ItemField }) {
  const value = config[field.key]
  const label = fieldLabel(kind, field, t)
  const hint = fieldHint(kind, field, t) || undefined

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
    case 'enum': {
      // The selected option's description replaces the field hint: an enum
      // usually needs different fields filled in per option.
      const selected = typeof value === 'string' ? value : ''
      const describes = optionDescription(kind, field, selected, t)
      return (
        <Field label={label} hint={describes || hint}>
          <Select
            value={selected || undefined}
            onChange={(v) => onChange(field.key, v)}
            optionList={(field.options ?? []).map((o) => ({
              value: o,
              label: optionLabel(kind, field, o, t),
            }))}
          />
        </Field>
      )
    }
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

export function ItemFields({ kind, fields, config, refs, vendors, t, onChange }: Props) {
  const preset = vendorOf(config, vendors)
  const builtin = Boolean(preset && preset.id !== 'custom' && preset.models.length > 0)
  // A built-in platform fills protocol and base URL itself; the model is a dropdown.
  const hidden = new Set(builtin ? ['protocol', 'baseUrl', 'defaultModel'] : [])
  const visible = fields.filter((f) => !f.advanced && !hidden.has(f.key))
  const advanced = fields.filter((f) => f.advanced && !hidden.has(f.key))
  return (
    <>
      {visible.map((field) => (
        <span key={field.key} style={{ display: 'contents' }}>
          <ItemFieldInput
            kind={kind}
            field={field}
            fields={fields}
            config={config}
            refs={refs}
            vendors={vendors}
            t={t}
            onChange={onChange}
          />
          {builtin && field.key === 'vendor' && preset && (
            <Field label={t('settings.llmgw.model')} hint={t('settings.llmgw.modelHint')}>
              <Select
                value={typeof config.defaultModel === 'string' ? config.defaultModel : undefined}
                onChange={(v) => onChange('defaultModel', v)}
                optionList={preset.models.map((m) => ({ value: m.id, label: m.label }))}
                style={{ width: '100%' }}
              />
            </Field>
          )}
        </span>
      ))}
      {advanced.length > 0 && (
        <details>
          <summary style={{ cursor: 'pointer' }}>
            <Typography.Text type="tertiary" size="small">
              {t('settings.items.advanced')}
            </Typography.Text>
          </summary>
          <div style={{ display: 'flex', flexDirection: 'column', gap: 16, paddingTop: 8 }}>
            {advanced.map((field) => (
              <ItemFieldInput
                key={field.key}
                kind={kind}
                field={field}
                fields={fields}
                config={config}
                refs={refs}
                vendors={vendors}
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
