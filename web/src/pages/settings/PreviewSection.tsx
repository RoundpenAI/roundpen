import { Form, Input, Select } from '@douyinfe/semi-ui-19'
import { Loading } from '../../components/Loading'
import { sectionGap } from './constants'
import { Field } from './parts'
import type { SectionBase } from './helpers'

type Props = SectionBase & {
  previewTtlOptions: { value: number; label: string }[]
}

export function PreviewSection({ isAdmin, loading, t, form, patch, previewTtlOptions }: Props) {
  return (
    isAdmin && loading ? (
      <Loading tip={t('settings.loadingSystem')} />
    ) : isAdmin ? (
      <Form labelPosition="top" labelAlign="left" style={sectionGap}>
      <div style={{ ...sectionGap, paddingTop: 16 }}>
        <Field label={t('settings.preview.publicUrl')}>
          <Input
            inputMode="url"
            autoComplete="url"
            placeholder="http://127.0.0.1:19001"
            value={form.previewPublicUrl}
            onChange={(v) => patch({ previewPublicUrl: v })}
          />
        </Field>
        <Field label={t('settings.preview.tokenTtl')}>
          <Select
            value={form.previewTokenTtlSeconds}
            onChange={(v) =>
              patch({ previewTokenTtlSeconds: Number(v) })
            }
            optionList={previewTtlOptions.map((o) => ({
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
