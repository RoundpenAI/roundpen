import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  buildSendPayload,
  contentsHaveSendableText,
  type SlashCommandOption,
} from './slashCommand.ts'

const catalog: SlashCommandOption[] = [
  { name: 'review', kind: 'skill' },
  { name: 'clear', kind: 'action' },
]

describe('buildSendPayload', () => {
  it('sends plain text when no chip is present', () => {
    assert.deepEqual(buildSendPayload([{ type: 'text', text: ' hello ' }], catalog), {
      kind: 'text',
      text: 'hello',
    })
  })

  it('keeps an unmatched slash text as plain text', () => {
    assert.deepEqual(
      buildSendPayload([{ type: 'text', text: '/etc/hosts 坏了' }], catalog),
      { kind: 'text', text: '/etc/hosts 坏了' },
    )
  })

  it('sends a skill-only chip as a command without args', () => {
    assert.deepEqual(
      buildSendPayload([{ type: 'skillSlot', value: 'review', label: 'review' }], catalog),
      { kind: 'command', name: 'review', args: '' },
    )
  })

  it('carries the text after the chip as args', () => {
    const contents = [
      { type: 'skillSlot', value: 'review', label: 'review' },
      { type: 'text', text: ' 关注并发 ' },
    ]
    assert.deepEqual(buildSendPayload(contents, catalog), {
      kind: 'command',
      name: 'review',
      args: '关注并发',
    })
  })

  it('falls back to plain text when text precedes the chip', () => {
    const contents = [
      { type: 'text', text: 'hey ' },
      { type: 'skillSlot', value: 'review', label: 'review' },
    ]
    assert.deepEqual(buildSendPayload(contents, catalog), {
      kind: 'text',
      text: 'hey /review',
    })
  })

  it('keeps the literal command text when the chip is not in the catalog', () => {
    const contents = [
      { type: 'skillSlot', value: 'gone', label: 'gone' },
      { type: 'text', text: 'x' },
    ]
    assert.deepEqual(buildSendPayload(contents, catalog), {
      kind: 'text',
      text: '/gone x',
    })
  })

  it('returns null when there is nothing to send', () => {
    assert.equal(buildSendPayload([], catalog), null)
    assert.equal(buildSendPayload([{ type: 'text', text: '   ' }], catalog), null)
  })
})

describe('contentsHaveSendableText', () => {
  it('accepts a chip-only composer', () => {
    assert.equal(contentsHaveSendableText([{ type: 'skillSlot', value: 'review' }]), true)
  })

  it('rejects blank and chip-less content', () => {
    assert.equal(contentsHaveSendableText([]), false)
    assert.equal(contentsHaveSendableText(undefined), false)
    assert.equal(contentsHaveSendableText([{ type: 'text', text: '  ' }]), false)
    assert.equal(contentsHaveSendableText([{ type: 'skillSlot', value: '' }]), false)
  })
})
