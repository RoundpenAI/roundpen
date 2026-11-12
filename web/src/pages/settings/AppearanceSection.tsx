import { Typography } from '@douyinfe/semi-ui-19'
import { useT } from '../../i18n'
import { UiChromeStrip } from '../../components/UiChromeStrip'
import { sectionGap } from './constants'

/** Personal settings: locale + theme, linked to the global uiPreference strip. */
export function AppearanceSection() {
  const t = useT()
  return (
    <div style={sectionGap}>
      <div>
        <Typography.Title heading={4} style={{ margin: 0 }}>
          {t('settings.appearance.title')}
        </Typography.Title>
        <Typography.Text type="tertiary">
          {t('settings.appearance.hint')}
        </Typography.Text>
      </div>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 12,
          flexWrap: 'wrap',
        }}
      >
        <Typography.Text>{t('settings.appearance.controls')}</Typography.Text>
        <UiChromeStrip showFeedback={false} />
      </div>
    </div>
  )
}
