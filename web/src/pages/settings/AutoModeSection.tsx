import { Button, Form, Input, Modal, Switch, Typography } from '@douyinfe/semi-ui-19'
import type { AutoModeDefaults, AutoModeSettings } from '../../api'
import { Loading } from '../../components/Loading'
import { AUTOMODE_DEFAULTS_TOKEN, AUTOMODE_LISTS, sectionGap, type AutoModeListKey } from './constants'
import { Field } from './parts'
import type { SectionBase, Translate } from './helpers'

type Props = SectionBase & {
  patchAutoMode: (next: Partial<AutoModeSettings>) => void
  setAutoModeList: (key: AutoModeListKey, custom: string[]) => void
  openDefaults: () => Promise<void>
}

export function AutoModeSection({ isAdmin, loading, t, form, patchAutoMode, setAutoModeList, openDefaults }: Props) {
  return (
    isAdmin && loading ? (
      <Loading tip={t('settings.loadingSystem')} />
    ) : isAdmin ? (
      <Form labelPosition="top" labelAlign="left" style={sectionGap}>
        <div style={{ ...sectionGap, paddingTop: 16 }}>
          <Typography.Text type="tertiary">
            {t('settings.automode.intro')}
          </Typography.Text>
          {AUTOMODE_LISTS.map(({ key, labelKey, hintKey }) => {
            const withDefaults = form.autoMode[key].includes(AUTOMODE_DEFAULTS_TOKEN)
            const custom = form.autoMode[key].filter(
              (e) => e !== AUTOMODE_DEFAULTS_TOKEN,
            )
            return (
              <div
                key={key}
                style={{
                  ...sectionGap,
                  borderTop: '1px solid var(--semi-color-border)',
                  paddingTop: 16,
                }}
              >
                <Field label={t(labelKey)} hint={t(hintKey)}>
                  <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                    {custom.map((entry, i) => (
                      <div key={i} style={{ display: 'flex', gap: 8 }}>
                        <Input
                          autoComplete="off"
                          value={entry}
                          onChange={(v) =>
                            setAutoModeList(
                              key,
                              custom.map((e, idx) => (idx === i ? v : e)),
                            )
                          }
                        />
                        <Button
                          type="danger"
                          onClick={() =>
                            setAutoModeList(
                              key,
                              custom.filter((_, idx) => idx !== i),
                            )
                          }
                        >
                          {t('settings.automode.remove')}
                        </Button>
                      </div>
                    ))}
                    <Button onClick={() => setAutoModeList(key, [...custom, ''])}>
                      {t('settings.automode.add')}
                    </Button>
                  </div>
                </Field>
                <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
                  <Switch
                    checked={withDefaults}
                    onChange={(v) =>
                      patchAutoMode({
                        [key]: v ? [...custom, AUTOMODE_DEFAULTS_TOKEN] : custom,
                      } as Partial<AutoModeSettings>)
                    }
                  />
                  <Typography.Text size="small">
                    {t('settings.automode.defaultsToggle')}
                  </Typography.Text>
                </div>
              </div>
            )
          })}
          <Button onClick={() => void openDefaults()}>
            {t('settings.automode.defaultsView')}
          </Button>
          <Typography.Text type="tertiary" size="small">
            {t('settings.automode.note')}
          </Typography.Text>
        </div>
      </Form>
    ) : null
  )

}

type ModalProps = {
  t: Translate
  defaultsOpen: boolean
  setDefaultsOpen: (open: boolean) => void
  defaultsLoading: boolean
  defaultsView: AutoModeDefaults | null
}

export function AutoModeDefaultsModal({
  t,
  defaultsOpen,
  setDefaultsOpen,
  defaultsLoading,
  defaultsView,
}: ModalProps) {
  return (
    <Modal
      title={t('settings.automode.defaultsTitle')}
      visible={defaultsOpen}
      footer={null}
      width={720}
      onCancel={() => setDefaultsOpen(false)}
    >
      {defaultsLoading ? (
        <Loading tip={t('settings.loadingSystem')} />
      ) : defaultsView ? (
        <div style={{ ...sectionGap, maxHeight: '60vh', overflowY: 'auto' }}>
          {AUTOMODE_LISTS.map(({ key, labelKey }) => (
            <div key={key}>
              <Typography.Title heading={6} style={{ margin: '0 0 4px' }}>
                {t(labelKey)}
              </Typography.Title>
              {(defaultsView[key] ?? []).map((entry, i) => (
                <Typography.Paragraph key={i} size="small" style={{ margin: '0 0 6px' }}>
                  {entry}
                </Typography.Paragraph>
              ))}
            </div>
          ))}
        </div>
      ) : (
        <Typography.Text type="danger">
          {t('settings.automode.defaultsFailed')}
        </Typography.Text>
      )}
    </Modal>
  )
}
