import { Form, Select } from '@douyinfe/semi-ui-19'
import { Loading } from '../../components/Loading'
import { sectionGap } from './constants'
import { Field } from './parts'
import type { SectionBase } from './helpers'

type Props = SectionBase & {
  builderOptions: { value: string; label: string }[]
}

export function BuildsSection({ isAdmin, loading, t, form, patch, builderOptions }: Props) {
  return (
    isAdmin && loading ? (
      <Loading tip={t('settings.loadingSystem')} />
    ) : isAdmin ? (
      <Form labelPosition="top" labelAlign="left" style={sectionGap}>
      <div style={{ ...sectionGap, paddingTop: 16 }}>
        <Field label={t('settings.builds.engine')}>
          <Select
            value={form.templateBuilder}
            onChange={(v) => patch({ templateBuilder: String(v) })}
            optionList={builderOptions.map((o) => ({
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
