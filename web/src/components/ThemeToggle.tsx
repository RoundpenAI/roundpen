import { Switch } from '@douyinfe/semi-ui-19'
import { IconMoon, IconSun } from '@douyinfe/semi-icons'
import { useUiPreference } from './SemiAppProvider'
import { useT } from '../i18n'

export function ThemeToggle() {
  const t = useT()
  const { preference, setTheme } = useUiPreference()
  const dark = preference.theme === 'dark'
  return (
    <Switch
      checked={dark}
      onChange={(checked) => setTheme(checked ? 'dark' : 'light')}
      checkedText={<IconMoon size="small" />}
      uncheckedText={<IconSun size="small" />}
      aria-label={t('nav.theme')}
    />
  )
}
