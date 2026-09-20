import type { MessageKey } from '../i18n/translate'

export type PrimaryMenuId =
  | 'assistants'
  | 'issues'
  | 'workspace'
  | 'settings'
  | 'admin'
  | 'registry'

export type PrimaryMenu = {
  id: PrimaryMenuId
  to: string
  labelKey: MessageKey
  admin?: boolean
}

export const PRIMARY_MENUS: PrimaryMenu[] = [
  { id: 'assistants', to: '/a', labelKey: 'nav.assistants' },
  { id: 'issues', to: '/issues', labelKey: 'nav.issues' },
  { id: 'workspace', to: '/workspace', labelKey: 'nav.workspace' },
  { id: 'settings', to: '/settings', labelKey: 'nav.settings' },
  { id: 'admin', to: '/admin/settings', labelKey: 'nav.admin', admin: true },
  { id: 'registry', to: '/registry', labelKey: 'nav.registry', admin: true },
]

export type SettingsSectionKey =
  | 'accounts'
  | 'password'
  | 'git'
  | 'agent'
  | 'general'
  | 'oauth'
  | 'llmgw'
  | 'webtools'
  | 'browser'
  | 'preview'
  | 'builds'
  | 'proxy'
  | 'automode'
  | 'system'

export type SettingsGroupKey =
  | 'account'
  | 'workspace'
  | 'access'
  | 'integrations'
  | 'sandbox'

export type SettingsSection = {
  key: SettingsSectionKey
  labelKey: MessageKey
  group: SettingsGroupKey
}

export type SettingsGroup = {
  key: SettingsGroupKey
  labelKey: MessageKey
}

export const PERSONAL_SETTINGS_TITLE_KEY: MessageKey = 'settings.title'
export const ADMIN_SETTINGS_TITLE_KEY: MessageKey = 'settings.adminTitle'

export const PERSONAL_SETTINGS_GROUPS: SettingsGroup[] = [
  { key: 'account', labelKey: 'settings.group.account' },
  { key: 'workspace', labelKey: 'settings.group.workspace' },
]

export const PERSONAL_SETTINGS_SECTIONS: SettingsSection[] = [
  { key: 'accounts', labelKey: 'settings.section.accounts', group: 'account' },
  { key: 'password', labelKey: 'settings.section.password', group: 'account' },
  { key: 'git', labelKey: 'settings.section.git', group: 'workspace' },
  { key: 'agent', labelKey: 'settings.section.agent', group: 'workspace' },
]

export const ADMIN_SETTINGS_GROUPS: SettingsGroup[] = [
  { key: 'access', labelKey: 'settings.group.access' },
  { key: 'integrations', labelKey: 'settings.group.integrations' },
  { key: 'sandbox', labelKey: 'settings.group.sandbox' },
]

export const ADMIN_SETTINGS_SECTIONS: SettingsSection[] = [
  { key: 'general', labelKey: 'settings.section.general', group: 'access' },
  { key: 'oauth', labelKey: 'settings.section.oauth', group: 'access' },
  { key: 'llmgw', labelKey: 'settings.section.llmgw', group: 'integrations' },
  { key: 'webtools', labelKey: 'settings.section.webtools', group: 'integrations' },
  { key: 'browser', labelKey: 'settings.section.browser', group: 'integrations' },
  { key: 'preview', labelKey: 'settings.section.preview', group: 'integrations' },
  { key: 'builds', labelKey: 'settings.section.builds', group: 'sandbox' },
  { key: 'proxy', labelKey: 'settings.section.proxy', group: 'sandbox' },
  { key: 'automode', labelKey: 'settings.section.automode', group: 'sandbox' },
  { key: 'system', labelKey: 'settings.section.system', group: 'sandbox' },
]

export const PERSONAL_SETTINGS_FALLBACK: SettingsSectionKey = 'accounts'
export const ADMIN_SETTINGS_FALLBACK: SettingsSectionKey = 'general'

export const PRIMARY_COLLAPSED_KEY = 'roundpen.app.primaryCollapsed'

export function visiblePrimaryMenus(isAdmin: boolean): PrimaryMenu[] {
  return PRIMARY_MENUS.filter((m) => !m.admin || isAdmin)
}

function resolveSection(
  raw: string | undefined,
  sections: SettingsSection[],
  fallback: SettingsSectionKey,
): SettingsSectionKey {
  const key = (raw ?? '').trim()
  return sections.some((s) => s.key === key)
    ? (key as SettingsSectionKey)
    : fallback
}

export function resolvePersonalSection(raw: string | undefined): SettingsSectionKey {
  return resolveSection(raw, PERSONAL_SETTINGS_SECTIONS, PERSONAL_SETTINGS_FALLBACK)
}

export function resolveAdminSection(raw: string | undefined): SettingsSectionKey {
  return resolveSection(raw, ADMIN_SETTINGS_SECTIONS, ADMIN_SETTINGS_FALLBACK)
}

/** True when the raw path segment names a platform-admin section. */
export function isAdminSectionKey(raw: string | undefined): boolean {
  const key = (raw ?? '').trim()
  return ADMIN_SETTINGS_SECTIONS.some((s) => s.key === key)
}

export type SettingsArea = 'personal' | 'admin'

export type SettingsAreaConfig = {
  basePath: string
  titleKey: MessageKey
  groups: SettingsGroup[]
  sections: SettingsSection[]
  resolve: (raw: string | undefined) => SettingsSectionKey
}

export const SETTINGS_AREAS: Record<SettingsArea, SettingsAreaConfig> = {
  personal: {
    basePath: '/settings',
    titleKey: PERSONAL_SETTINGS_TITLE_KEY,
    groups: PERSONAL_SETTINGS_GROUPS,
    sections: PERSONAL_SETTINGS_SECTIONS,
    resolve: resolvePersonalSection,
  },
  admin: {
    basePath: '/admin/settings',
    titleKey: ADMIN_SETTINGS_TITLE_KEY,
    groups: ADMIN_SETTINGS_GROUPS,
    sections: ADMIN_SETTINGS_SECTIONS,
    resolve: resolveAdminSection,
  },
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
  if (pathname.startsWith('/issues')) return 'issues'
  if (pathname.startsWith('/workspace')) return 'workspace'
  if (pathname.startsWith('/admin')) return 'admin'
  if (pathname.startsWith('/settings')) return 'settings'
  if (pathname.startsWith('/registry')) return 'registry'
  return 'assistants'
}
