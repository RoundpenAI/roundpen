import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  cleanSuggestText,
  suggestContextFor,
  truncateSuggestBio,
} from './sessionPromptSuggestions.ts'
import { en } from '../i18n/en.ts'
import { zh_CN } from '../i18n/zh_CN.ts'

function fill(
  template: string,
  vars: Record<string, string>,
): string {
  let s = template
  for (const [k, v] of Object.entries(vars)) {
    s = s.replaceAll(`{${k}}`, v)
  }
  return s
}

describe('cleanSuggestText', () => {
  it('trims and collapses whitespace', () => {
    assert.equal(cleanSuggestText('  代码\n  助手  '), '代码 助手')
  })

  it('treats nullish as empty', () => {
    assert.equal(cleanSuggestText(undefined), '')
    assert.equal(cleanSuggestText(null), '')
  })
})

describe('truncateSuggestBio', () => {
  it('leaves short bios alone', () => {
    assert.equal(truncateSuggestBio('管仓库做 code review'), '管仓库做 code review')
  })

  it('truncates long bios with an ellipsis', () => {
    const long =
      '负责公司内部代码仓库的审查、发布协调，以及每周技术周报整理与分发工作'
    const out = truncateSuggestBio(long, 20)
    assert.ok(out.endsWith('…'))
    assert.ok(out.length <= 20)
    assert.ok(!out.includes(long.slice(-4)))
  })
})

describe('suggestContextFor', () => {
  it('uses name and truncated bio when both present', () => {
    const ctx = suggestContextFor(
      { name: '小李', bio: '帮我管代码仓库、做 code review' },
      '助手',
    )
    assert.equal(ctx.name, '小李')
    assert.equal(ctx.hasBio, true)
    assert.equal(ctx.bio, '帮我管代码仓库、做 code review')
  })

  it('falls back to default name when blank; bio stays empty', () => {
    const ctx = suggestContextFor({ name: '  ', bio: '' }, '助手')
    assert.equal(ctx.name, '助手')
    assert.equal(ctx.hasBio, false)
    assert.equal(ctx.bio, '')
  })

  it('handles missing assistant', () => {
    const ctx = suggestContextFor(null, 'Assistant')
    assert.equal(ctx.name, 'Assistant')
    assert.equal(ctx.hasBio, false)
  })
})

describe('tailored suggestion copy', () => {
  it('zh strings include name and bio', () => {
    const vars = { name: '小李', bio: '管仓库做 review' }
    assert.match(
      fill(zh_CN['chat.suggest.introBio'], vars),
      /小李.*管仓库做 review/,
    )
    assert.match(
      fill(zh_CN['chat.suggest.prioritizeBio'], vars),
      /小李.*管仓库做 review/,
    )
    assert.match(
      fill(zh_CN['chat.suggest.digestBio'], vars),
      /小李.*管仓库做 review/,
    )
  })

  it('empty-bio fallback still names the assistant', () => {
    const vars = { name: '小李', bio: '' }
    assert.match(fill(zh_CN['chat.suggest.intro'], vars), /小李/)
    assert.match(fill(en['chat.suggest.intro'], vars), /小李/)
    assert.doesNotMatch(fill(zh_CN['chat.suggest.intro'], vars), /\{name\}/)
  })
})
