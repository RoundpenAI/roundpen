import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  buildChangeSet,
  shouldFoldDiff,
  toUnifiedDiff,
  workspacePreviewHref,
} from './changeSet.ts'

describe('buildChangeSet', () => {
  it('aggregates Edit old/new into a file change', () => {
    const cs = buildChangeSet([
      {
        title: 'Edit',
        status: 'completed',
        kind: 'edit',
        input: {
          path: 'projects/demo/a.ts',
          old_string: 'const x = 1\n',
          new_string: 'const x = 2\nconst y = 3\n',
        },
      },
    ])
    assert.ok(cs)
    assert.equal(cs!.files.length, 1)
    assert.equal(cs!.files[0]?.path, 'projects/demo/a.ts')
    assert.ok(cs!.files[0]!.unified.includes('--- a/projects/demo/a.ts'))
    assert.ok(cs!.plus >= 1)
  })

  it('skips running and non-edit tools', () => {
    const cs = buildChangeSet([
      {
        title: 'Edit',
        status: 'in_progress',
        input: { path: 'a.ts', old_string: 'a', new_string: 'b' },
      },
      {
        title: 'Read',
        status: 'completed',
        input: { path: 'a.ts' },
      },
    ])
    assert.equal(cs, null)
  })

  it('merges multiple edits of the same path', () => {
    const cs = buildChangeSet([
      {
        title: 'Edit',
        status: 'completed',
        input: { path: 'f.go', old_string: 'a', new_string: 'b' },
      },
      {
        title: 'Edit',
        status: 'completed',
        input: { path: 'f.go', old_string: 'b', new_string: 'c\nd' },
      },
    ])
    assert.ok(cs)
    assert.equal(cs!.files.length, 1)
    assert.ok(cs!.files[0]!.unified.includes('+c'))
  })
})

describe('toUnifiedDiff / fold / href', () => {
  it('builds a readable hunk', () => {
    const u = toUnifiedDiff('x.ts', 'old\n', 'new\n')
    assert.ok(u.includes('-old'))
    assert.ok(u.includes('+new'))
  })

  it('folds large diffs', () => {
    const lines = Array.from({ length: 100 }, (_, i) => `+line ${i}`)
    assert.equal(shouldFoldDiff(lines.join('\n')), true)
    assert.equal(shouldFoldDiff('+a\n-b\n'), false)
  })

  it('builds workspace preview deep links', () => {
    assert.equal(
      workspacePreviewHref('projects/foo/bar.ts'),
      '/workspace?path=projects%2Ffoo&preview=bar.ts',
    )
    assert.equal(workspacePreviewHref('README.md'), '/workspace?path=.&preview=README.md')
  })
})
