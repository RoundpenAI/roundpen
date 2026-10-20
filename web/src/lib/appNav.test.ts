import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  ADMIN_SETTINGS_SECTIONS,
  PERSONAL_SETTINGS_SECTIONS,
  PRIMARY_MENUS,
  SETTINGS_AREAS,
  isAdminSectionKey,
  matchPrimaryMenu,
  resolveAdminSection,
  resolvePersonalSection,
  visiblePersonalSettingsSections,
  visiblePrimaryMenus,
} from './appNav.ts'

describe('visiblePrimaryMenus', () => {
  it('hides the admin menus for non-admin', () => {
    const keys = visiblePrimaryMenus(false).map((m) => m.id)
    assert.deepEqual(keys, [
      'assistants',
      'projects',
      'issues',
      'workspace',
      'settings',
    ])
  })

  it('shows the admin menus for admin', () => {
    const keys = visiblePrimaryMenus(true).map((m) => m.id)
    assert.deepEqual(keys, [
      'assistants',
      'projects',
      'issues',
      'workspace',
      'settings',
      'admin',
    ])
  })
})

describe('settings sections', () => {
  it('personal sections keep their grouped order', () => {
    assert.deepEqual(
      PERSONAL_SETTINGS_SECTIONS.map((s) => s.key),
      ['profile', 'apps', 'password', 'notifications', 'slots'],
    )
  })

  it('admin sections keep their grouped order', () => {
    assert.deepEqual(
      ADMIN_SETTINGS_SECTIONS.map((s) => s.key),
      [
        'general',
        'llmgw',
        'webtools',
        'browser',
        'proxy',
        'env',
      ],
    )
  })

  it('every section belongs to a declared group', () => {
    for (const area of ['personal', 'admin'] as const) {
      const cfg = SETTINGS_AREAS[area]
      const groupKeys = new Set(cfg.groups.map((g) => g.key))
      for (const s of cfg.sections) {
        assert.ok(groupKeys.has(s.group), `${s.key} has an undeclared group`)
      }
    }
  })

  it('personal and admin sections do not overlap', () => {
    const admin = new Set(ADMIN_SETTINGS_SECTIONS.map((s) => s.key))
    for (const s of PERSONAL_SETTINGS_SECTIONS) {
      assert.ok(!admin.has(s.key), `${s.key} appears in both areas`)
    }
  })
})

describe('resolvePersonalSection', () => {
  it('defaults to apps', () => {
    assert.equal(resolvePersonalSection(undefined), 'apps')
    assert.equal(resolvePersonalSection(''), 'apps')
  })

  it('accepts personal sections', () => {
    assert.equal(resolvePersonalSection('slots'), 'slots')
    assert.equal(resolvePersonalSection('apps'), 'apps')
    assert.equal(resolvePersonalSection('notifications', true), 'notifications')
  })

  it('maps legacy accounts/connectors/mcp/git to apps and agent to slots', () => {
    assert.equal(resolvePersonalSection('accounts'), 'apps')
    assert.equal(resolvePersonalSection('connectors'), 'apps')
    assert.equal(resolvePersonalSection('mcp'), 'apps')
    assert.equal(resolvePersonalSection('git'), 'apps')
    assert.equal(resolvePersonalSection('agent'), 'slots')
  })

  it('rejects unknown, admin, and unsupported notification sections', () => {
    assert.equal(resolvePersonalSection('nope'), 'apps')
    assert.equal(resolvePersonalSection('general'), 'apps')
    assert.equal(resolvePersonalSection('notifications', false), 'apps')
  })
})

describe('visiblePersonalSettingsSections', () => {
  it('includes notifications only when Web Push is supported', () => {
    assert.deepEqual(
      visiblePersonalSettingsSections(true).map((s) => s.key),
      ['profile', 'apps', 'password', 'notifications', 'slots'],
    )
    assert.deepEqual(
      visiblePersonalSettingsSections(false).map((s) => s.key),
      ['profile', 'apps', 'password', 'slots'],
    )
  })
})

describe('resolveAdminSection', () => {
  it('defaults to general', () => {
    assert.equal(resolveAdminSection(undefined), 'general')
    assert.equal(resolveAdminSection('nope'), 'general')
  })

  it('accepts admin sections and rejects personal ones', () => {
    assert.equal(resolveAdminSection('proxy'), 'proxy')
    assert.equal(resolveAdminSection('automode'), 'general')
    assert.equal(resolveAdminSection('git'), 'general')
  })
})

describe('isAdminSectionKey', () => {
  it('separates platform sections from personal ones', () => {
    assert.equal(isAdminSectionKey('proxy'), true)
    assert.equal(isAdminSectionKey('automode'), false)
    assert.equal(isAdminSectionKey('llmgw'), true)
    assert.equal(isAdminSectionKey('oauth'), false)
    assert.equal(isAdminSectionKey('git'), false)
    assert.equal(isAdminSectionKey(undefined), false)
  })
})

describe('PRIMARY_MENUS paths', () => {
  it('matches product routes and label keys', () => {
    assert.equal(PRIMARY_MENUS.find((m) => m.id === 'assistants')?.to, '/a')
    assert.equal(PRIMARY_MENUS.find((m) => m.id === 'workspace')?.to, '/workspace')
    assert.equal(PRIMARY_MENUS.find((m) => m.id === 'settings')?.to, '/settings')
    assert.equal(PRIMARY_MENUS.find((m) => m.id === 'admin')?.to, '/admin/settings')
    assert.equal(
      PRIMARY_MENUS.find((m) => m.id === 'assistants')?.labelKey,
      'nav.assistants',
    )
  })
})

describe('matchPrimaryMenu', () => {
  it('matches workspace path', () => {
    assert.equal(matchPrimaryMenu('/workspace'), 'workspace')
    assert.equal(matchPrimaryMenu('/projects'), 'projects')
    assert.equal(matchPrimaryMenu('/projects/PRJ-1'), 'projects')
    assert.equal(matchPrimaryMenu('/a/x'), 'assistants')
  })

  it('matches the admin area ahead of the settings area', () => {
    assert.equal(matchPrimaryMenu('/admin/settings/oauth'), 'admin')
    assert.equal(matchPrimaryMenu('/settings/apps'), 'settings')
  })
})
