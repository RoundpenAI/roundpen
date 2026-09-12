import type { MessageKey } from '../i18n/translate'

export type PrimaryMenuId = 'assistants' | 'workspace' | 'settings' | 'registry'

export type PrimaryMenu = {
  id: PrimaryMenuId
  to: string
  labelKey: MessageKey
  admin?: boolean
}

export const PRIMARY_MENUS: PrimaryMenu[] = [
  { id: 'assistants', to: '/a', labelKey: 'nav.assistants' },
  { id: 'workspace', to: '/workspace', labelKey: 'nav.workspace' },
  { id: 'settings', to: '/settings', labelKey: 'nav.settings' },
  { id: 'registry', to: '/registry', labelKey: 'nav.registry', admin: true },
]

export type SettingsSectionKey =
  | 'git'
  | 'general'
  | 'preview'
  | 'builds'
  | 'browser'
  | 'llmgw'
  | 'system'

export type SettingsSection = {
  key: SettingsSectionKey
  labelKey: MessageKey
  admin?: boolean
}

export const SETTINGS_SECTIONS: SettingsSection[] = [
  { key: 'git', labelKey: 'settings.section.git' },
  { key: 'general', labelKey: 'settings.section.general', admin: true },
  { key: 'preview', labelKey: 'settings.section.preview', admin: true },
  { key: 'builds', labelKey: 'settings.section.builds', admin: true },
  { key: 'browser', labelKey: 'settings.section.browser', admin: true },
  { key: 'llmgw', labelKey: 'settings.section.llmgw', admin: true },
  { key: 'system', labelKey: 'settings.section.system', admin: true },
]

export const PRIMARY_COLLAPSED_KEY = 'roundpen.app.primaryCollapsed'

export function visiblePrimaryMenus(isAdmin: boolean): PrimaryMenu[] {
  return PRIMARY_MENUS.filter((m) => !m.admin || isAdmin)
}

export function visibleSettingsSections(isAdmin: boolean): SettingsSection[] {
  return SETTINGS_SECTIONS.filter((s) => !s.admin || isAdmin)
}

export function resolveSettingsSection(
  raw: string | undefined,
  isAdmin: boolean,
): SettingsSectionKey {
  const key = (raw ?? '').trim() as SettingsSectionKey
  const allowed = visibleSettingsSections(isAdmin)
  if (allowed.some((s) => s.key === key)) return key
  return 'git'
}

export function readPrimaryCollapsed(): boolean {
  try {
    return localStorage.getItem(PRIMARY_COLLAPSED_KEY) === '1'
  } catch {
    return false
  }
}

export function writePrimaryCollapsed(collapsed: boolean): void {
  try {
    localStorage.setItem(PRIMARY_COLLAPSED_KEY, collapsed ? '1' : '0')
  } catch {
    /* ignore */
  }
}

/** Which primary menu matches the current pathname. */
export function matchPrimaryMenu(pathname: string): PrimaryMenuId {
  if (pathname.startsWith('/workspace')) return 'workspace'
  if (pathname.startsWith('/settings')) return 'settings'
  if (pathname.startsWith('/registry')) return 'registry'
  return 'assistants'
}
