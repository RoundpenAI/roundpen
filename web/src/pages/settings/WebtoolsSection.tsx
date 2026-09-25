import { Form, Input, Typography } from '@douyinfe/semi-ui-19'
import { Loading } from '../../components/Loading'
import { sectionGap } from './constants'
import { Field } from './parts'
import type { SectionBase } from './helpers'

type Props = SectionBase

export function WebtoolsSection({ isAdmin, loading, t, form, patch }: Props) {
  return (
    isAdmin && loading ? (
      <Loading tip={t('settings.loadingSystem')} />
    ) : isAdmin ? (
      <Form labelPosition="top" labelAlign="left" style={sectionGap}>
        <div style={{ ...sectionGap, paddingTop: 16 }}>
          <Typography.Text type="tertiary">
            {t('settings.webtools.intro')}
          </Typography.Text>
          <div
            style={{
              display: 'grid',
              gap: 16,
              gridTemplateColumns:
                'repeat(auto-fit, minmax(220px, 1fr))',
            }}
          >
            <Field
              label={t('settings.webtools.endpoint')}
              hint={t('settings.webtools.endpointHint')}
            >
              <Input
                inputMode="url"
                autoComplete="off"
                placeholder="https://api.tavily.com"
                value={form.webSearchEndpoint}
                onChange={(v) => patch({ webSearchEndpoint: v })}
              />
            </Field>
            <Field
              label={t('settings.webtools.apiKey')}
              hint={t('settings.webtools.keepSecret')}
            >
              <Input
                mode="password"
                autoComplete="new-password"
                placeholder={t('settings.browser.keepMasked')}
                value={form.webSearchApiKey}
                onChange={(v) => patch({ webSearchApiKey: v })}
              />
            </Field>
            <Field
              label={t('settings.webtools.proxy')}
              hint={t('settings.proxy.urlHint')}
            >
              <Input
                autoComplete="off"
                placeholder="http://10.0.0.8:8080"
                value={form.webSearchProxy}
                onChange={(v) => patch({ webSearchProxy: v })}
              />
            </Field>
          </div>
          <Typography.Text type="tertiary" size="small">
            {t('settings.webtools.note')}
          </Typography.Text>
        </div>
      </Form>
    ) : null
  )
}
