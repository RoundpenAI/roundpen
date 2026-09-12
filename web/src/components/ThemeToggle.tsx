import { Switch } from '@douyinfe/semi-ui-19'
import { IconMoon, IconSun } from '@douyinfe/semi-icons'
import { useUiPreference } from './SemiAppProvider'

export function ThemeToggle() {
  const { preference, setTheme } = useUiPreference()
  const dark = preference.theme === 'dark'
  return (
    <Switch
      checked={dark}
      onChange={(checked) => setTheme(checked ? 'dark' : 'light')}
      checkedText={<IconMoon size="small" />}
      uncheckedText={<IconSun size="small" />}
      aria-label="切换深色模式"
    />
  )
}
