import type { MessageKey } from '../i18n/translate'

export type PrimaryMenuId =
  | 'assistants'
  | 'projects'
  | 'issues'
  | 'workspace'
  | 'settings'
  | 'admin'

export type PrimaryMenu = {
  id: PrimaryMenuId
  to: string
  labelKey: MessageKey
  admin?: boolean
}

export const PRIMARY_MENUS: PrimaryMenu[] = [
  { id: 'assistants', to: '/a', labelKey: 'nav.assistants' },
  { id: 'projects', to: '/projects', labelKey: 'nav.projects' },
  { id: 'issues', to: '/issues', labelKey: 'nav.issues' },
  { id: 'workspace', to: '/workspace', labelKey: 'nav.workspace' },
  { id: 'settings', to: '/settings', labelKey: 'nav.settings' },
  { id: 'admin', to: '/admin/settings', labelKey: 'nav.admin', admin: true },
]

export type SettingsSectionKey =
  | 'profile'
  | 'apps'
  | 'password'
  | 'slots'
  | 'notifications'
  | 'oauth'
  | 'plugins'
  | 'llmgw'
  | 'webtools'
  | 'browser'
  | 'proxy'
  | 'env'
  | 'automode'

export type SettingsGroupKey =
  | 'account'
  | 'workspace'
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
  { key: 'profile', labelKey: 'settings.section.profile', group: 'account' },
  { key: 'apps', labelKey: 'settings.section.apps', group: 'account' },
  { key: 'password', labelKey: 'settings.section.password', group: 'account' },
  { key: 'notifications', labelKey: 'settings.section.notifications', group: 'account' },
  { key: 'slots', labelKey: 'settings.section.slots', group: 'workspace' },
]

/** Old settings URLs that moved or merged into another section. */
const LEGACY_PERSONAL_SECTIONS: Record<string, SettingsSectionKey> = {
  accounts: 'apps',
  connectors: 'apps',
  mcp: 'apps',
  git: 'apps',
  // Agent env (status / upgrade / model source) was removed; proxy & co. live here.
  agent: 'slots',
}

export const ADMIN_SETTINGS_GROUPS: SettingsGroup[] = [
  { key: 'integrations', labelKey: 'settings.group.integrations' },
  { key: 'sandbox', labelKey: 'settings.group.sandbox' },
]

export const ADMIN_SETTINGS_SECTIONS: SettingsSection[] = [
  // Federated login (GitHub / Gitea / the plugin that enables it) is hidden
  // until we want it back. The pages stay in the codebase.
  { key: 'llmgw', labelKey: 'settings.section.llmgw', group: 'integrations' },
  { key: 'webtools', labelKey: 'settings.section.webtools', group: 'integrations' },
  { key: 'browser', labelKey: 'settings.section.browser', group: 'integrations' },
  { key: 'proxy', labelKey: 'settings.section.proxy', group: 'sandbox' },
  { key: 'env', labelKey: 'settings.section.env', group: 'sandbox' },
]

export const PERSONAL_SETTINGS_FALLBACK: SettingsSectionKey = 'apps'
export const ADMIN_SETTINGS_FALLBACK: SettingsSectionKey = 'llmgw'

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

/** Personal settings that should appear for this browser's Web Push support. */
export function visiblePersonalSettingsSections(pushSupported: boolean): SettingsSection[] {
  if (pushSupported) return PERSONAL_SETTINGS_SECTIONS
  return PERSONAL_SETTINGS_SECTIONS.filter((s) => s.key !== 'notifications')
}

export function resolvePersonalSection(
  raw: string | undefined,
  pushSupported = true,
): SettingsSectionKey {
  const legacy = LEGACY_PERSONAL_SECTIONS[(raw ?? '').trim()]
  if (legacy) return legacy
  return resolveSection(
    raw,
    visiblePersonalSettingsSections(pushSupported),
    PERSONAL_SETTINGS_FALLBACK,
  )
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
  if (pathname.startsWith('/projects')) return 'projects'
  if (pathname.startsWith('/issues')) return 'issues'
  if (pathname.startsWith('/workspace')) return 'workspace'
  if (pathname.startsWith('/admin')) return 'admin'
  if (pathname.startsWith('/settings')) return 'settings'
  return 'assistants'
}
