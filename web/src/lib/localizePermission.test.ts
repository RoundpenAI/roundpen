import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { en, type MessageKey } from '../i18n/en.ts'
import { zh_CN } from '../i18n/zh_CN.ts'
import {
  localizePermissionOptionName,
  localizePermissionTitle,
  parseAnnotatedTitle,
} from './localizePermission.ts'

function makeT(catalog: Record<MessageKey, string>) {
  return (key: MessageKey, vars?: Record<string, string | number>) => {
    let s = catalog[key] ?? key
    if (vars) {
      for (const [k, v] of Object.entries(vars)) {
        s = s.replaceAll(`{${k}}`, String(v))
      }
    }
    return s
  }
}

describe('localizePermission', () => {
  const tZh = makeT(zh_CN)
  const tEn = makeT(en)

  it('parses annotated titles', () => {
    assert.deepEqual(
      parseAnnotatedTitle(
        'Bash · high_side_effect — this tool can change the system or an authenticated browser session; confirm before running',
      ),
      {
        tool: 'Bash',
        rule: 'high_side_effect',
        reason:
          'this tool can change the system or an authenticated browser session; confirm before running',
      },
    )
    assert.deepEqual(parseAnnotatedTitle('Edit'), { tool: 'Edit' })
  })

  it('localizes high_side_effect titles and options for zh', () => {
    const title = localizePermissionTitle(
      'Bash · high_side_effect — this tool can change the system or an authenticated browser session; confirm before running',
      tZh,
    )
    assert.equal(
      title,
      'Bash · 高副作用 — 此工具可能改动系统或已登录的浏览器会话，运行前请确认',
    )
    assert.equal(
      localizePermissionOptionName('allow_all', 'Allow all tools (session)', tZh),
      '允许全部工具（本会话）',
    )
  })

  it('keeps English labels under en locale', () => {
    const title = localizePermissionTitle(
      'Bash · high_side_effect — this tool can change the system or an authenticated browser session; confirm before running',
      tEn,
    )
    assert.match(title, /^Bash · high side effect — /)
    assert.equal(
      localizePermissionOptionName('allow', 'Allow once', tEn),
      'Allow once',
    )
  })

  it('leaves classifier prose alone', () => {
    const raw = 'Bash · Outside workspace — looks risky'
    assert.equal(localizePermissionTitle(raw, tZh), raw)
    assert.equal(
      localizePermissionOptionName('opt-0', '继续', tZh),
      '继续',
    )
  })

  it('localizes hard-deny style reasons with host', () => {
    assert.equal(
      localizePermissionTitle(
        'Bash · Cloud Metadata — blocked access to cloud instance metadata (169.254.169.254)',
        tZh,
      ),
      'Bash · 云元数据 — 已拦截对云实例元数据的访问（169.254.169.254）',
    )
  })
})
