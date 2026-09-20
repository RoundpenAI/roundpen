import { Button, Form, Input, Select, Typography } from '@douyinfe/semi-ui-19'
import { Loading } from '../../components/Loading'
import { CDP_OPTIONS, sectionGap } from './constants'
import { optionsWithCurrentValue } from './helpers'
import { Field } from './parts'
import type { SectionBase } from './helpers'

type Props = SectionBase & {
  currentSuffix: (value: string) => string
  browserTest: string
  testBrowser: () => Promise<void>
}

export function BrowserSection({ isAdmin, loading, t, form, patch, currentSuffix, browserTest, testBrowser }: Props) {
  return (
    isAdmin && loading ? (
      <Loading tip={t('settings.loadingSystem')} />
    ) : isAdmin ? (
      <Form labelPosition="top" labelAlign="left" style={sectionGap}>
      <div style={{ ...sectionGap, paddingTop: 16 }}>
        <Field label={t('settings.browser.cdpProvider')}>
          <Select
            value={form.cdpProvider}
            onChange={(v) => patch({ cdpProvider: String(v) })}
            optionList={optionsWithCurrentValue(
              CDP_OPTIONS.map((o) => ({
                value: o.value,
                label: t(o.labelKey),
              })),
              form.cdpProvider,
              currentSuffix,
            )}
            style={{ width: '100%' }}
          />
        </Field>
        {(form.cdpProvider === 'remote' ||
          form.cdpProvider === 'cloud' ||
          form.cdpProvider === 'host') && (
          <Field
            label={
              form.cdpProvider === 'host'
                ? t('settings.browser.hostCdpUrl')
                    : t('settings.browser.cdpEndpoint')
            }
          >
            <Input
              inputMode="url"
              autoComplete="off"
              spellCheck={false}
              placeholder={
                form.cdpProvider === 'host'
                  ? 'http://127.0.0.1:9222'
                  : 'wss://browser.example/devtools/browser/…'
              }
              value={form.cdpEndpoint}
              onChange={(v) => patch({ cdpEndpoint: v })}
            />
          </Field>
        )}
        {(form.cdpProvider === 'remote' ||
          form.cdpProvider === 'cloud') && (
          <Field label={t('settings.browser.cdpToken')}>
            <Input
              mode="password"
              autoComplete="new-password"
              placeholder={t('settings.browser.keepMasked')}
              value={form.cdpToken}
              onChange={(v) => patch({ cdpToken: v })}
            />
          </Field>
        )}
        {(form.cdpProvider === 'auto' ||
          form.cdpProvider === 'docker') && (
          <Field label={t('settings.browser.guestPort')}>
            <Input
              inputMode="numeric"
              spellCheck={false}
              value={String(form.cdpPort || 3000)}
              onChange={(v) =>
                patch({ cdpPort: Number(v) || 3000 })
              }
            />
          </Field>
        )}
        <div
          style={{
            display: 'flex',
            alignItems: 'center',
            flexWrap: 'wrap',
            gap: 8,
          }}
        >
          <Button
            size="small"
            theme="borderless"
            onClick={() => void testBrowser()}
          >
            {t('settings.browser.test')}
          </Button>
          {browserTest && (
            <Typography.Text type="tertiary" size="small">
              {browserTest}
            </Typography.Text>
          )}
        </div>
        <Typography.Text type="tertiary" size="small">
          {t('settings.browser.hint')}
        </Typography.Text>
      </div>
      </Form>
    ) : null
  )
}
