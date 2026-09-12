import type { UiLocale } from '../lib/uiPreference'
import { en, type MessageKey } from './en'
import { zh_CN } from './zh_CN'

const catalogs: Record<UiLocale, Record<MessageKey, string>> = {
  en,
  zh_CN,
}

export type { MessageKey }

export function translate(
  locale: UiLocale,
  key: MessageKey,
  vars?: Record<string, string | number>,
): string {
  let s =
    catalogs[locale][key] ?? catalogs.zh_CN[key] ?? catalogs.en[key] ?? key
  if (vars) {
    for (const [k, v] of Object.entries(vars)) {
      s = s.replaceAll(`{${k}}`, String(v))
    }
  }
  return s
}
