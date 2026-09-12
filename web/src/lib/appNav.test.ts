import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  PRIMARY_MENUS,
  SETTINGS_SECTIONS,
  resolveSettingsSection,
  visibleSettingsSections,
  visiblePrimaryMenus,
} from './appNav.ts'

describe('visiblePrimaryMenus', () => {
  it('hides registry for non-admin', () => {
    const keys = visiblePrimaryMenus(false).map((m) => m.id)
    assert.deepEqual(keys, ['assistants', 'workspace', 'settings'])
  })

  it('shows registry for admin', () => {
    const keys = visiblePrimaryMenus(true).map((m) => m.id)
    assert.deepEqual(keys, ['assistants', 'workspace', 'settings', 'registry'])
  })
})

describe('visibleSettingsSections', () => {
  it('non-admin only runtime + git', () => {
    assert.deepEqual(
      visibleSettingsSections(false).map((s) => s.key),
      ['runtime', 'git'],
    )
  })

  it('admin gets all sections in order', () => {
    assert.deepEqual(
      visibleSettingsSections(true).map((s) => s.key),
      SETTINGS_SECTIONS.map((s) => s.key),
    )
  })
})

describe('resolveSettingsSection', () => {
  it('defaults to runtime', () => {
    assert.equal(resolveSettingsSection(undefined, false), 'runtime')
    assert.equal(resolveSettingsSection('', true), 'runtime')
  })

  it('accepts known visible section', () => {
    assert.equal(resolveSettingsSection('git', false), 'git')
    assert.equal(resolveSettingsSection('general', true), 'general')
  })

  it('rejects unknown or unauthorized section', () => {
    assert.equal(resolveSettingsSection('nope', true), 'runtime')
    assert.equal(resolveSettingsSection('general', false), 'runtime')
  })
})

describe('PRIMARY_MENUS paths', () => {
  it('matches product routes and label keys', () => {
    assert.equal(PRIMARY_MENUS.find((m) => m.id === 'assistants')?.to, '/a')
    assert.equal(PRIMARY_MENUS.find((m) => m.id === 'workspace')?.to, '/workspace')
    assert.equal(PRIMARY_MENUS.find((m) => m.id === 'settings')?.to, '/settings')
    assert.equal(PRIMARY_MENUS.find((m) => m.id === 'registry')?.to, '/registry')
    assert.equal(
      PRIMARY_MENUS.find((m) => m.id === 'assistants')?.labelKey,
      'nav.assistants',
    )
  })
})

describe('matchPrimaryMenu', () => {
  it('matches workspace path', async () => {
    const { matchPrimaryMenu } = await import('./appNav.ts')
    assert.equal(matchPrimaryMenu('/workspace'), 'workspace')
    assert.equal(matchPrimaryMenu('/a/x'), 'assistants')
  })
})
