import { useCallback } from 'react'
import { useUiPreference } from '../components/SemiAppProvider'
import { translate, type MessageKey } from './translate'

export type { MessageKey }
export { translate }
export { en } from './en'
export { zh_CN } from './zh_CN'

export function useT() {
  const { preference } = useUiPreference()
  return useCallback(
    (key: MessageKey, vars?: Record<string, string | number>) =>
      translate(preference.locale, key, vars),
    [preference.locale],
  )
}
