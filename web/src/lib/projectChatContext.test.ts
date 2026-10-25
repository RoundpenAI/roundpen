import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { roleLabel } from './projectChatLabels.ts'

describe('roleLabel', () => {
  it('maps known worker roles', () => {
    assert.equal(roleLabel('implementer'), '实现')
    assert.equal(roleLabel('reviewer'), '审查')
    assert.equal(roleLabel('tester'), '测试')
  })

  it('falls back for unknown roles', () => {
    assert.equal(roleLabel(''), 'Worker')
    assert.equal(roleLabel(undefined), 'Worker')
  })
})
