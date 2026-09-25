import { Collapse, Form, Input, Select, Typography } from '@douyinfe/semi-ui-19'
import { Loading } from '../../components/Loading'
import { sectionGap } from './constants'
import { Field, Toggle } from './parts'
import type { SectionBase } from './helpers'

type Props = SectionBase & {
  logBodyOptions: { value: number; label: string }[]
}

export function LlmgwSection({ isAdmin, loading, t, form, patch, logBodyOptions }: Props) {
  return (
    isAdmin && loading ? (
      <Loading tip={t('settings.loadingSystem')} />
    ) : isAdmin ? (
      <Form labelPosition="top" labelAlign="left" style={sectionGap}>
      <div style={{ ...sectionGap, paddingTop: 16 }}>
        <Typography.Text type="tertiary" size="small">
          {t('settings.llmgw.intro')}
        </Typography.Text>

        <Toggle
          checked={form.llmgwEnabled}
          onChange={(v) => patch({ llmgwEnabled: v })}
        >
          {t('settings.llmgw.enable')}
        </Toggle>

        <div style={sectionGap}>
          <Typography.Text strong size="small">
            {t('settings.llmgw.upstreamTitle')}
          </Typography.Text>
          <Typography.Text type="tertiary" size="small">
            {t('settings.llmgw.upstreamHint')}
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
              label={t('settings.llmgw.openaiBase')}
              hint={t('settings.llmgw.openaiBaseHint')}
            >
              <Input
                inputMode="url"
                autoComplete="off"
                placeholder="https://api.openai.com"
                value={form.llmgwOpenaiBaseUrl}
                onChange={(v) => patch({ llmgwOpenaiBaseUrl: v })}
              />
            </Field>
            <Field
              label={t('settings.llmgw.openaiKey')}
              hint={t('settings.llmgw.keepSecret')}
            >
              <Input
                mode="password"
                autoComplete="new-password"
                placeholder={t('settings.browser.keepMasked')}
                value={form.llmgwOpenaiApiKey}
                onChange={(v) => patch({ llmgwOpenaiApiKey: v })}
              />
            </Field>
            <Field
              label={t('settings.llmgw.anthropicBase')}
              hint={t('settings.llmgw.anthropicBaseHint')}
            >
              <Input
                inputMode="url"
                autoComplete="off"
                placeholder="https://api.anthropic.com"
                value={form.llmgwAnthropicBaseUrl}
                onChange={(v) => patch({ llmgwAnthropicBaseUrl: v })}
              />
            </Field>
            <Field
              label={t('settings.llmgw.anthropicKey')}
              hint={t('settings.llmgw.keepSecret')}
            >
              <Input
                mode="password"
                autoComplete="new-password"
                placeholder={t('settings.browser.keepMasked')}
                value={form.llmgwAnthropicApiKey}
                onChange={(v) => patch({ llmgwAnthropicApiKey: v })}
              />
            </Field>
            <Field
              label={t('settings.llmgw.openaiProxy')}
              hint={t('settings.proxy.urlHint')}
            >
              <Input
                autoComplete="off"
                placeholder="socks5://10.0.0.9:1080"
                value={form.llmgwOpenaiProxy}
                onChange={(v) => patch({ llmgwOpenaiProxy: v })}
              />
            </Field>
            <Field
              label={t('settings.llmgw.anthropicProxy')}
              hint={t('settings.proxy.urlHint')}
            >
              <Input
                autoComplete="off"
                placeholder="http://10.0.0.8:8080"
                value={form.llmgwAnthropicProxy}
                onChange={(v) => patch({ llmgwAnthropicProxy: v })}
              />
            </Field>
          </div>
        </div>

        <div
          style={{
            ...sectionGap,
            borderTop: '1px solid var(--semi-color-border)',
            paddingTop: 16,
          }}
        >
          <Typography.Text strong size="small">
            {t('settings.llmgw.agentsTitle')}
          </Typography.Text>
          <Typography.Text type="tertiary" size="small">
            {t('settings.llmgw.agentsHint')}
          </Typography.Text>
          <Field
            label={t('settings.llmgw.publicUrl')}
            hint={t('settings.llmgw.publicUrlHint')}
          >
            <Input
              inputMode="url"
              autoComplete="url"
              placeholder="http://127.0.0.1:9527"
              value={form.llmgwPublicUrl}
              onChange={(v) => patch({ llmgwPublicUrl: v })}
            />
          </Field>
          <Field
            label={t('settings.llmgw.defaultModel')}
            hint={t('settings.llmgw.defaultModelHint')}
          >
            <Input
              spellCheck={false}
              autoComplete="off"
              placeholder="e.g. gpt-4o-mini or claude-sonnet-4"
              value={form.llmgwDefaultModel}
              onChange={(v) => patch({ llmgwDefaultModel: v })}
            />
          </Field>
          <Field
            label={t('settings.llmgw.virtualKeys')}
            hint={t('settings.llmgw.virtualKeysHint')}
          >
            <Input
              spellCheck={false}
              autoComplete="off"
              placeholder="vk-dev:dev"
              value={form.llmgwVirtualKeys}
              onChange={(v) => patch({ llmgwVirtualKeys: v })}
            />
          </Field>
        </div>

        <Collapse>
          <Collapse.Panel header={t('settings.llmgw.advanced')} itemKey="advanced">
            <div style={sectionGap}>
              <Field
                label={t('settings.llmgw.embedModel')}
                hint={t('settings.llmgw.embedModelHint')}
              >
                <Input
                  spellCheck={false}
                  placeholder="text-embedding-3-small"
                  value={form.llmgwEmbeddingModel}
                  onChange={(v) =>
                    patch({ llmgwEmbeddingModel: v })
                  }
                />
              </Field>
              <Field
                label={t('settings.llmgw.logBody')}
                hint={t('settings.llmgw.logBodyHint')}
              >
                <Select
                  value={form.llmgwLogBodyMaxBytes}
                  onChange={(v) =>
                    patch({ llmgwLogBodyMaxBytes: Number(v) })
                  }
                  optionList={logBodyOptions.map((o) => ({
                    value: o.value,
                    label: o.label,
                  }))}
                  style={{ width: '100%' }}
                />
              </Field>
              <Typography.Text type="tertiary" size="small">
                    {t('settings.llmgw.secretsNote')}
                  </Typography.Text>
            </div>
          </Collapse.Panel>
        </Collapse>
      </div>
      </Form>
    ) : null
  )
}
