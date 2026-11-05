import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { firstPickedPath, resolvePickedDirectory } from './fnosPickFolder.ts'

describe('firstPickedPath', () => {
  it('reads string arrays and object entries', () => {
    assert.equal(firstPickedPath(['/vol1/1000/Photos']), '/vol1/1000/Photos')
    assert.equal(firstPickedPath([{ path: '/vol1/1000/Docs' }]), '/vol1/1000/Docs')
    assert.equal(firstPickedPath('/vol1/1000/Music'), '/vol1/1000/Music')
    assert.equal(firstPickedPath([]), null)
    assert.equal(firstPickedPath(undefined), null)
  })
})

describe('resolvePickedDirectory', () => {
  it('returns path on success and null on cancel', () => {
    assert.equal(
      resolvePickedDirectory({ code: 0, data: ['/vol1/1000/Photos'] }),
      '/vol1/1000/Photos',
    )
    assert.equal(resolvePickedDirectory({ code: 0, data: [] }), null)
  })

  it('throws when the host bridge is missing or returns an error', () => {
    assert.throws(() => resolvePickedDirectory(null), /未能打开/)
    assert.throws(
      () => resolvePickedDirectory({ code: 1, msg: 'permission denied', data: [] }),
      /permission denied/,
    )
  })
})
