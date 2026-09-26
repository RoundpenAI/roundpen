import { Form, Input, Select } from '@douyinfe/semi-ui-19'
import { Loading } from '../../components/Loading'
import { sectionGap } from './constants'
import { Field, Toggle } from './parts'
import type { SectionBase } from './helpers'

type Props = SectionBase & {
  defaultImageOptions: { value: string; label: string }[]
  sandboxTtlOptions: { value: number; label: string }[]
}

export function GeneralSection({ isAdmin, loading, t, form, patch, defaultImageOptions, sandboxTtlOptions }: Props) {
  return (
    isAdmin && loading ? (
      <Loading tip={t('settings.loadingSystem')} />
    ) : isAdmin ? (
      <Form labelPosition="top" labelAlign="left" style={sectionGap}>
      <div style={{ ...sectionGap, paddingTop: 16 }}>
        <Toggle
          checked={form.allowPublicRegistration}
          onChange={(v) => patch({ allowPublicRegistration: v })}
        >
          {t('settings.general.allowRegistration')}
        </Toggle>
        <Field label={t('settings.general.defaultImage')}>
          {defaultImageOptions.length > 0 ? (
            <Select
              value={form.defaultImage}
              onChange={(v) => patch({ defaultImage: String(v) })}
              optionList={defaultImageOptions}
              style={{ width: '100%' }}
            />
          ) : (
            <Input
              value={form.defaultImage}
              onChange={(v) => patch({ defaultImage: v })}
            />
          )}
        </Field>
        <Field
          label={t('settings.general.agentImage')}
          hint={t('settings.general.agentImageHint')}
        >
          <Input
            value={form.agentImage}
            onChange={(v) => patch({ agentImage: v })}
            placeholder="ghcr.io/roundpenai/code-agent:0.1.0"
          />
        </Field>
        <Field label={t('settings.general.defaultTtl')}>
          <Select
            value={form.defaultTtlSeconds}
            onChange={(v) =>
              patch({ defaultTtlSeconds: Number(v) })
            }
            optionList={sandboxTtlOptions.map((o) => ({
              value: o.value,
              label: o.label,
            }))}
            style={{ width: '100%' }}
          />
        </Field>
      </div>
      </Form>
    ) : null
  )
}
