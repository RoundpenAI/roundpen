import type { CSSProperties } from 'react'
import type { AppSettings } from '../../api'
import type { MessageKey } from '../../i18n'

export const emptySettings: AppSettings = {
  allowPublicRegistration: false,
  defaultImage: 'host',
  defaultTtlSeconds: 1800,
  agentImage: '',
  llmgwEnabled: false,
  llmgwPublicUrl: '',
  llmgwLogBodyMaxBytes: 0,
  llmgwVirtualKeys: '',
  autoMode: {
    environment: ['$defaults'],
    allow: ['$defaults'],
    softDeny: ['$defaults'],
    hardDeny: ['$defaults'],
  },
}

export type AutoModeListKey = 'environment' | 'allow' | 'softDeny' | 'hardDeny'

export const AUTOMODE_DEFAULTS_TOKEN = '$defaults'

export const AUTOMODE_LISTS: {
  key: AutoModeListKey
  labelKey: MessageKey
  hintKey: MessageKey
}[] = [
  {
    key: 'environment',
    labelKey: 'settings.automode.environment',
    hintKey: 'settings.automode.environmentHint',
  },
  {
    key: 'allow',
    labelKey: 'settings.automode.allow',
    hintKey: 'settings.automode.allowHint',
  },
  {
    key: 'softDeny',
    labelKey: 'settings.automode.softDeny',
    hintKey: 'settings.automode.softDenyHint',
  },
  {
    key: 'hardDeny',
    labelKey: 'settings.automode.hardDeny',
    hintKey: 'settings.automode.hardDenyHint',
  },
]

export const SANDBOX_TTL_OPTIONS: { value: number; labelKey: MessageKey }[] = [
  { value: 600, labelKey: 'settings.ttl.10m' },
  { value: 900, labelKey: 'settings.ttl.15m' },
  { value: 1200, labelKey: 'settings.ttl.20m' },
  { value: 1800, labelKey: 'settings.ttl.30m' },
  { value: 3600, labelKey: 'settings.ttl.1h' },
  { value: 7200, labelKey: 'settings.ttl.2h' },
  { value: 14400, labelKey: 'settings.ttl.4h' },
]

export const LOG_BODY_OPTIONS: { value: number; labelKey: MessageKey }[] = [
  { value: 0, labelKey: 'settings.logBody.off' },
  { value: -1, labelKey: 'settings.logBody.legacy' },
  { value: 4096, labelKey: 'settings.logBody.4k' },
  { value: 16384, labelKey: 'settings.logBody.16k' },
  { value: 65536, labelKey: 'settings.logBody.64k' },
  { value: 262144, labelKey: 'settings.logBody.256k' },
]

export const sectionGap: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 16,
}
