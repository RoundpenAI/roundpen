import { Collapse, Form, Input, Select, Typography } from '@douyinfe/semi-ui-19'
import { Loading } from '../../components/Loading'
import { ItemListSection } from './items/ItemListSection'
import { SlotBindings } from './items/SlotBindings'
import { sectionGap } from './constants'
import { Field, Toggle } from './parts'
import type { SectionBase } from './helpers'

type Props = SectionBase & {
  logBodyOptions: { value: number; label: string }[]
}

// Providers are setting items (kind "llm") and each usage point picks one via
// a slot binding; the switches below stay in the settings document.
export function LlmgwSection({ isAdmin, loading, t, form, patch, logBodyOptions }: Props) {
  if (!isAdmin) return null
  if (loading) return <Loading tip={t('settings.loadingSystem')} />

  return (
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
          <ItemListSection
            kind="llm"
            introKey="settings.llmgw.upstreamHint"
            noteKey="settings.llmgw.secretsNote"
            t={t}
          />
        </div>

        <SlotBindings kind="llm" t={t} />

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
                label={t('settings.llmgw.logBody')}
                hint={t('settings.llmgw.logBodyHint')}
              >
                <Select
                  value={form.llmgwLogBodyMaxBytes}
                  onChange={(v) => patch({ llmgwLogBodyMaxBytes: Number(v) })}
                  optionList={logBodyOptions.map((o) => ({
                    value: o.value,
                    label: o.label,
                  }))}
                  style={{ width: '100%' }}
                />
              </Field>
            </div>
          </Collapse.Panel>
        </Collapse>
      </div>
    </Form>
  )
}
