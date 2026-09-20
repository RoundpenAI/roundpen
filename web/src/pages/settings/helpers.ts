import { templateDisplayName } from '../../api'
import type { AppSettings, Template } from '../../api'
import type { MessageKey } from '../../i18n'

export type Translate = (key: MessageKey, vars?: Record<string, string | number>) => string

export type SectionBase = {
  isAdmin: boolean
  loading: boolean
  t: Translate
  form: AppSettings
  patch: (partial: Partial<AppSettings>) => void
}

export function optionsWithCurrentValue(
  options: { value: string; label: string }[],
  current: string,
  currentSuffix: (value: string) => string,
): { value: string; label: string }[] {
  if (options.some((o) => o.value === current)) return options
  return [...options, { value: current, label: currentSuffix(current) }]
}

export function formatDurationSeconds(
  seconds: number,
  t: Translate,
): string {
  if (!Number.isFinite(seconds)) return String(seconds)
  if (seconds < 0) return String(seconds)
  if (seconds % 3600 === 0) {
    const h = seconds / 3600
    return h === 1 ? t('settings.duration.1h') : t('settings.duration.nh', { n: h })
  }
  if (seconds % 60 === 0) {
    const m = seconds / 60
    return m === 1 ? t('settings.duration.1m') : t('settings.duration.nm', { n: m })
  }
  return t('settings.duration.s', { n: seconds })
}

export function ttlOptionsWithCurrent(
  options: { value: number; label: string }[],
  current: number,
  currentSuffix: (value: string) => string,
  t: Translate,
) {
  if (options.some((o) => o.value === current)) return options
  return [
    ...options,
    {
      value: current,
      label: currentSuffix(formatDurationSeconds(current, t)),
    },
  ]
}

export function numberOptionsWithCurrent(
  options: { value: number; label: string }[],
  current: number,
  currentSuffix: (value: string) => string,
) {
  if (options.some((o) => o.value === current)) return options
  return [
    ...options,
    { value: current, label: currentSuffix(String(current)) },
  ]
}

export function templateRef(t: Template): string {
  const name = templateDisplayName(t)
  return name.includes('/') ? (name.split('/').pop() ?? name) : name
}
