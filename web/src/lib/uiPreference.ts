export type UiTheme = 'dark' | 'light'
export type UiLocale = 'zh_CN' | 'en'

export type UiPreference = {
  theme: UiTheme
  locale: UiLocale
}

export const UI_PREFERENCE_KEY = 'roundpen.ui'

export const DEFAULT_UI_PREFERENCE: UiPreference = {
  theme: 'dark',
  locale: 'zh_CN',
}

function isTheme(v: unknown): v is UiTheme {
  return v === 'dark' || v === 'light'
}

function isLocale(v: unknown): v is UiLocale {
  return v === 'zh_CN' || v === 'en'
}

export function loadUiPreference(): UiPreference {
  try {
    const raw = localStorage.getItem(UI_PREFERENCE_KEY)
    if (!raw) return { ...DEFAULT_UI_PREFERENCE }
    const parsed = JSON.parse(raw) as Partial<UiPreference>
    return {
      theme: isTheme(parsed.theme) ? parsed.theme : DEFAULT_UI_PREFERENCE.theme,
      locale: isLocale(parsed.locale)
        ? parsed.locale
        : DEFAULT_UI_PREFERENCE.locale,
    }
  } catch {
    return { ...DEFAULT_UI_PREFERENCE }
  }
}

export function saveUiPreference(pref: UiPreference): void {
  localStorage.setItem(UI_PREFERENCE_KEY, JSON.stringify(pref))
}

/** Semi global dark mode: body[theme-mode=dark]. Light = remove attribute. */
export function applyThemeToDocument(theme: UiTheme): void {
  if (theme === 'dark') {
    document.body.setAttribute('theme-mode', 'dark')
  } else {
    document.body.removeAttribute('theme-mode')
  }
}
