import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { keyboardInset, resolveAppViewport } from './visualViewport.ts'

describe('keyboardInset', () => {
  it('is zero when the visual viewport fills the window', () => {
    assert.equal(keyboardInset(800, 800, 0), 0)
  })

  it('reports the covered band below the visual viewport', () => {
    assert.equal(keyboardInset(800, 500, 0), 300)
    assert.equal(keyboardInset(800, 500, 50), 250)
  })

  it('never goes negative', () => {
    assert.equal(keyboardInset(500, 600, 0), 0)
  })
})

describe('resolveAppViewport', () => {
  it('ignores residual URL-bar offset in standalone when idle', () => {
    const got = resolveAppViewport({
      innerHeight: 844,
      vvHeight: 780,
      vvOffsetTop: 64,
      standalone: true,
    })
    assert.deepEqual(got, {
      height: 844,
      offsetTop: 0,
      keyboardInset: 0,
      keyboardOpen: false,
    })
  })

  it('follows visualViewport in browser tabs', () => {
    const got = resolveAppViewport({
      innerHeight: 800,
      vvHeight: 500,
      vvOffsetTop: 50,
      standalone: false,
    })
    assert.equal(got.keyboardOpen, true)
    assert.equal(got.height, 500)
    assert.equal(got.offsetTop, 50)
    assert.equal(got.keyboardInset, 250)
  })

  it('shrinks height for the keyboard in standalone without offsetTop', () => {
    const got = resolveAppViewport({
      innerHeight: 844,
      vvHeight: 400,
      vvOffsetTop: 20,
      standalone: true,
    })
    assert.equal(got.keyboardOpen, true)
    assert.equal(got.height, 400)
    assert.equal(got.offsetTop, 0)
  })
})
