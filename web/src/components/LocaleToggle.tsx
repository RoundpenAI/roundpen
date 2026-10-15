import { Select } from '@douyinfe/semi-ui-19'
import { useUiPreference } from './SemiAppProvider'
import type { UiLocale } from '../lib/uiPreference'
import { useT } from '../i18n'

const OPTIONS: { value: UiLocale; label: string }[] = [
  { value: 'zh_CN', label: '中文' },
  { value: 'en', label: 'English' },
]

export function LocaleToggle({ block }: { block?: boolean }) {
  const t = useT()
  const { preference, setLocale } = useUiPreference()
  return (
    <Select
      value={preference.locale}
      onChange={(v) => setLocale(v as UiLocale)}
      optionList={OPTIONS}
      aria-label={t('nav.language')}
      style={block ? { width: '100%' } : { width: 110 }}
    />
  )
}
