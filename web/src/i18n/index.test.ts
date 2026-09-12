import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { en, type MessageKey } from './en.ts'
import { zh_CN } from './zh_CN.ts'

describe('i18n catalogs', () => {
  it('zh_CN has every en key', () => {
    for (const key of Object.keys(en) as MessageKey[]) {
      assert.equal(typeof zh_CN[key], 'string', `missing zh: ${key}`)
      assert.ok(zh_CN[key].length > 0, `empty zh: ${key}`)
    }
  })

  it('en and zh share the same key set', () => {
    const enKeys = Object.keys(en).sort()
    const zhKeys = Object.keys(zh_CN).sort()
    assert.deepEqual(zhKeys, enKeys)
  })

  it('interpolates placeholders', () => {
    const template = en['nav.signOutUser']
    assert.equal(template.replaceAll('{user}', 'alice'), 'Sign out (alice)')
    assert.equal(
      zh_CN['nav.signOutUser'].replaceAll('{user}', 'alice'),
      '退出 (alice)',
    )
  })
})
