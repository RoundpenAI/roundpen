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
  visiblePrimaryMenus,
} from './appNav.ts'

describe('visiblePrimaryMenus', () => {
  it('hides the admin menus for non-admin', () => {
    const keys = visiblePrimaryMenus(false).map((m) => m.id)
    assert.deepEqual(keys, ['assistants', 'issues', 'workspace', 'settings'])
  })

  it('shows the admin menus for admin', () => {
    const keys = visiblePrimaryMenus(true).map((m) => m.id)
    assert.deepEqual(keys, [
      'assistants',
      'issues',
      'workspace',
      'settings',
      'admin',
      'registry',
    ])
  })
})

describe('settings sections', () => {
  it('personal sections keep their grouped order', () => {
    assert.deepEqual(
      PERSONAL_SETTINGS_SECTIONS.map((s) => s.key),
      ['accounts', 'password', 'git', 'agent'],
    )
  })

  it('admin sections keep their grouped order', () => {
    assert.deepEqual(
      ADMIN_SETTINGS_SECTIONS.map((s) => s.key),
      [
        'general',
        'oauth',
        'llmgw',
        'webtools',
        'browser',
        'preview',
        'builds',
        'proxy',
        'automode',
        'system',
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
  it('defaults to accounts', () => {
    assert.equal(resolvePersonalSection(undefined), 'accounts')
    assert.equal(resolvePersonalSection(''), 'accounts')
  })

  it('accepts personal sections', () => {
    assert.equal(resolvePersonalSection('git'), 'git')
    assert.equal(resolvePersonalSection('agent'), 'agent')
  })

  it('rejects unknown and admin sections', () => {
    assert.equal(resolvePersonalSection('nope'), 'accounts')
    assert.equal(resolvePersonalSection('general'), 'accounts')
  })
})

describe('resolveAdminSection', () => {
  it('defaults to general', () => {
    assert.equal(resolveAdminSection(undefined), 'general')
    assert.equal(resolveAdminSection('nope'), 'general')
  })

  it('accepts admin sections and rejects personal ones', () => {
    assert.equal(resolveAdminSection('automode'), 'automode')
    assert.equal(resolveAdminSection('git'), 'general')
  })
})

describe('isAdminSectionKey', () => {
  it('separates platform sections from personal ones', () => {
    assert.equal(isAdminSectionKey('automode'), true)
    assert.equal(isAdminSectionKey('oauth'), true)
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
    assert.equal(PRIMARY_MENUS.find((m) => m.id === 'registry')?.to, '/registry')
    assert.equal(
      PRIMARY_MENUS.find((m) => m.id === 'assistants')?.labelKey,
      'nav.assistants',
    )
  })
})

describe('matchPrimaryMenu', () => {
  it('matches workspace path', () => {
    assert.equal(matchPrimaryMenu('/workspace'), 'workspace')
    assert.equal(matchPrimaryMenu('/a/x'), 'assistants')
  })

  it('matches the admin area ahead of the settings area', () => {
    assert.equal(matchPrimaryMenu('/admin/settings/oauth'), 'admin')
    assert.equal(matchPrimaryMenu('/settings/accounts'), 'settings')
  })
})
