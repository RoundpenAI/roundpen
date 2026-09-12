export type PrimaryMenuId = 'assistants' | 'settings' | 'registry'

export type PrimaryMenu = {
  id: PrimaryMenuId
  to: string
  label: string
  admin?: boolean
}

export const PRIMARY_MENUS: PrimaryMenu[] = [
  { id: 'assistants', to: '/a', label: '助手' },
  { id: 'settings', to: '/settings', label: '设置' },
  { id: 'registry', to: '/registry', label: '镜像', admin: true },
]

export type SettingsSectionKey =
  | 'runtime'
  | 'git'
  | 'general'
  | 'preview'
  | 'builds'
  | 'browser'
  | 'llmgw'
  | 'system'

export type SettingsSection = {
  key: SettingsSectionKey
  label: string
  admin?: boolean
}

export const SETTINGS_SECTIONS: SettingsSection[] = [
  { key: 'runtime', label: 'Agent runtime' },
  { key: 'git', label: 'Git personal tokens' },
  { key: 'general', label: 'General', admin: true },
  { key: 'preview', label: 'Preview', admin: true },
  { key: 'builds', label: 'Builds', admin: true },
  { key: 'browser', label: 'Browser', admin: true },
  { key: 'llmgw', label: 'LLM gateway', admin: true },
  { key: 'system', label: 'System', admin: true },
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
  return 'runtime'
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
  if (pathname.startsWith('/settings')) return 'settings'
  if (pathname.startsWith('/registry')) return 'registry'
  return 'assistants'
}
