import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import { classifyTool, formatGroupStats } from './toolStats.ts'

describe('classifyTool', () => {
  it('prefers ACP kind over the title', () => {
    assert.equal(classifyTool({ title: 'Bash', status: 'completed', kind: 'read' }), 'explore')
  })

  it('maps Claude titles when kind is missing', () => {
    assert.equal(classifyTool({ title: 'Edit', status: 'completed' }), 'edit')
    assert.equal(classifyTool({ title: 'Read src/a.ts', status: 'completed' }), 'explore')
    assert.equal(classifyTool({ title: 'Grep', status: 'completed' }), 'search')
    assert.equal(classifyTool({ title: 'Bash', status: 'completed' }), 'command')
  })
})

describe('formatGroupStats', () => {
  it('aggregates a finished mixed turn', () => {
    const calls = [
      ...Array.from({ length: 18 }, (_, i) => ({
        title: 'Edit',
        status: 'completed',
        kind: 'edit',
        input: {
          path: `src/f${i}.ts`,
          old_string: 'keep\nold',
          new_string: 'keep\nnew\nextra',
        },
      })),
      ...Array.from({ length: 8 }, (_, i) => ({
        title: 'Read',
        status: 'completed',
        kind: 'read',
        input: { path: `src/r${i}.ts` },
      })),
      { title: 'Grep', status: 'completed', kind: 'search' },
      { title: 'Grep', status: 'completed', kind: 'search' },
      { title: 'Grep', status: 'completed', kind: 'search' },
      { title: 'Bash', status: 'completed', kind: 'execute' },
      { title: 'Bash', status: 'completed', kind: 'execute' },
    ]
    const stats = formatGroupStats(calls)
    assert.equal(
      stats.label,
      'Edited 18 files, explored 8 files, 3 searches, ran 2 commands',
    )
    assert.equal(stats.plus, 36)
    assert.equal(stats.minus, 18)
  })

  it('names the file while a write is still running', () => {
    const stats = formatGroupStats([
      {
        title: 'Edit',
        status: 'in_progress',
        kind: 'edit',
        input: { path: 'src/a.ts', old_string: 'x', new_string: 'y' },
      },
    ])
    assert.equal(stats.label, 'Editing a.ts')
    assert.equal(stats.plus, 1)
    assert.equal(stats.minus, 1)
  })

  it('switches a finished single edit to Edited', () => {
    const stats = formatGroupStats([
      {
        title: 'Edit',
        status: 'completed',
        kind: 'edit',
        input: { path: 'src/a.ts', old_string: 'x', new_string: 'y' },
      },
    ])
    assert.equal(stats.label, 'Edited a.ts')
  })

  it('keeps finished work in the past while a later read is still going', () => {
    const stats = formatGroupStats([
      {
        title: 'Edit',
        status: 'completed',
        kind: 'edit',
        input: { path: 'a.ts', old_string: 'x', new_string: 'y' },
      },
      {
        title: 'Read',
        status: 'in_progress',
        kind: 'read',
        input: { path: 'b.ts' },
      },
    ])
    assert.equal(stats.label, 'Edited a.ts, reading b.ts')
  })

  it('names a running command and a search query', () => {
    const stats = formatGroupStats([
      {
        title: 'Grep',
        status: 'in_progress',
        kind: 'search',
        input: { pattern: 'handleClick' },
      },
      {
        title: 'Bash',
        status: 'in_progress',
        kind: 'execute',
        input: { command: 'go test ./internal/acp/...' },
      },
    ])
    assert.equal(stats.label, 'Searching for handleClick, running go test ./internal/acp/...')
  })

  it('dedupes reads and notes a failed command', () => {
    const stats = formatGroupStats([
      { title: 'Read', status: 'completed', input: { path: 'a.ts' } },
      { title: 'Read', status: 'completed', input: { path: 'a.ts' } },
      { title: 'Bash', status: 'failed', kind: 'execute', input: { command: 'make' } },
    ])
    assert.equal(stats.label, 'Read a.ts, ran make, 1 failed')
  })
})
