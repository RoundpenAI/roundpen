import { Form } from '@douyinfe/semi-ui-19'
import { Loading } from '../../components/Loading'
import { sectionGap } from './constants'
import { Toggle } from './parts'
import type { SectionBase } from './helpers'

type Props = SectionBase

export function GeneralSection({ isAdmin, loading, t, form, patch }: Props) {
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
      </div>
      </Form>
    ) : null
  )
}
