import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
  type ReactNode,
} from 'react'
import { ConfigProvider } from '@douyinfe/semi-ui-19'
import zh_CN from '@douyinfe/semi-ui-19/lib/es/locale/source/zh_CN'
import en_GB from '@douyinfe/semi-ui-19/lib/es/locale/source/en_GB'
import {
  applyThemeToDocument,
  loadUiPreference,
  saveUiPreference,
  type UiLocale,
  type UiPreference,
  type UiTheme,
} from '../lib/uiPreference'

type Ctx = {
  preference: UiPreference
  setTheme: (theme: UiTheme) => void
  setLocale: (locale: UiLocale) => void
}

const UiContext = createContext<Ctx | null>(null)

export function useUiPreference(): Ctx {
  const ctx = useContext(UiContext)
  if (!ctx) throw new Error('useUiPreference requires SemiAppProvider')
  return ctx
}

const LOCALE_MAP = {
  zh_CN,
  en: en_GB,
} as const

export function SemiAppProvider({ children }: { children: ReactNode }) {
  const [preference, setPreference] = useState<UiPreference>(() =>
    loadUiPreference(),
  )

  useEffect(() => {
    applyThemeToDocument(preference.theme)
  }, [preference.theme])

  const setTheme = useCallback((theme: UiTheme) => {
    setPreference((prev) => {
      const next = { ...prev, theme }
      saveUiPreference(next)
      return next
    })
  }, [])

  const setLocale = useCallback((locale: UiLocale) => {
    setPreference((prev) => {
      const next = { ...prev, locale }
      saveUiPreference(next)
      return next
    })
  }, [])

  const value = useMemo(
    () => ({ preference, setTheme, setLocale }),
    [preference, setTheme, setLocale],
  )

  return (
    <UiContext.Provider value={value}>
      <ConfigProvider locale={LOCALE_MAP[preference.locale]}>
        {children}
      </ConfigProvider>
    </UiContext.Provider>
  )
}
