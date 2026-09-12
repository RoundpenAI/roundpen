const store = new Map<string, string>()
;(globalThis as { localStorage?: Storage }).localStorage = {
  getItem: (k) => store.get(k) ?? null,
  setItem: (k, v) => void store.set(k, String(v)),
  removeItem: (k) => void store.delete(k),
  clear: () => store.clear(),
  key: () => null,
  get length() {
    return store.size
  },
} as Storage

;(globalThis as { document?: Document }).document = {
  body: {
    attrs: new Map<string, string>(),
    setAttribute(name: string, value: string) {
      this.attrs.set(name, value)
    },
    removeAttribute(name: string) {
      this.attrs.delete(name)
    },
    getAttribute(name: string) {
      return this.attrs.get(name) ?? null
    },
    hasAttribute(name: string) {
      return this.attrs.has(name)
    },
  },
} as unknown as Document

import assert from 'node:assert/strict'
import { describe, it, beforeEach } from 'node:test'
import {
  DEFAULT_UI_PREFERENCE,
  loadUiPreference,
  saveUiPreference,
  applyThemeToDocument,
  type UiPreference,
} from './uiPreference.ts'

describe('uiPreference', () => {
  beforeEach(() => {
    localStorage.clear()
    document.body.removeAttribute('theme-mode')
  })

  it('defaults to dark zh_CN', () => {
    assert.deepEqual(loadUiPreference(), DEFAULT_UI_PREFERENCE)
    assert.equal(DEFAULT_UI_PREFERENCE.theme, 'dark')
    assert.equal(DEFAULT_UI_PREFERENCE.locale, 'zh_CN')
  })

  it('round-trips save/load', () => {
    const next: UiPreference = { theme: 'light', locale: 'en' }
    saveUiPreference(next)
    assert.deepEqual(loadUiPreference(), next)
  })

  it('ignores corrupt JSON', () => {
    localStorage.setItem('roundpen.ui', '{not-json')
    assert.deepEqual(loadUiPreference(), DEFAULT_UI_PREFERENCE)
  })

  it('applyThemeToDocument sets body theme-mode', () => {
    applyThemeToDocument('dark')
    assert.equal(document.body.getAttribute('theme-mode'), 'dark')
    applyThemeToDocument('light')
    assert.equal(document.body.hasAttribute('theme-mode'), false)
  })
})
