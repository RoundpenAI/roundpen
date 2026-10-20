import { describe, expect, it } from 'vitest'
import type { AgentMessage } from '../api'
import { inferDeliveryNeed, repoFromPath } from './deliveryNeed'

function tool(
  title: string,
  opts: {
    status?: string
    kind?: string
    input?: Record<string, unknown>
  } = {},
): AgentMessage {
  return {
    id: Math.random().toString(36).slice(2),
    sessionId: 's1',
    role: 'tool',
    content: '',
    createdAt: new Date().toISOString(),
    meta: {
      type: 'tool_call',
      toolId: title.split(/[\s:·]/)[0],
      title,
      status: opts.status ?? 'completed',
      kind: opts.kind,
      input: opts.input,
    },
  }
}

describe('repoFromPath', () => {
  it('maps projects paths to repo root', () => {
    expect(repoFromPath('projects/foo/src/a.ts')).toBe('projects/foo')
    expect(repoFromPath('/workspace/projects/foo/a.ts')).toBe('projects/foo')
    expect(repoFromPath('notes/x.md')).toBeNull()
  })
})

describe('inferDeliveryNeed', () => {
  it('hides bar when there are no git-backed edits', () => {
    expect(
      inferDeliveryNeed([
        tool('Write · notes/memo.md', {
          kind: 'edit',
          input: { path: 'notes/memo.md', contents: 'hi' },
        }),
      ]),
    ).toEqual({ openPR: false, mergePR: false })
  })

  it('offers open PR after edits under projects/', () => {
    expect(
      inferDeliveryNeed([
        tool('Edit · projects/demo/a.ts', {
          kind: 'edit',
          input: {
            path: 'projects/demo/a.ts',
            old_string: 'a',
            new_string: 'b',
          },
        }),
      ]),
    ).toEqual({ openPR: true, mergePR: false })
  })

  it('clears open PR after GitCommit', () => {
    expect(
      inferDeliveryNeed([
        tool('Edit', {
          kind: 'edit',
          input: { path: 'projects/demo/a.ts', old_string: 'a', new_string: 'b' },
        }),
        tool('GitCommit', {
          input: { repo_path: 'projects/demo', message: 'fix: a' },
        }),
      ]),
    ).toEqual({ openPR: false, mergePR: false })
  })

  it('offers merge after CreatePullRequest until MergePullRequest', () => {
    const afterCreate = inferDeliveryNeed([
      tool('CreatePullRequest', {
        input: { repo_path: 'projects/demo', title: 'feat' },
      }),
    ])
    expect(afterCreate).toEqual({ openPR: false, mergePR: true })

    const afterMerge = inferDeliveryNeed([
      tool('CreatePullRequest', {
        input: { repo_path: 'projects/demo', title: 'feat' },
      }),
      tool('MergePullRequest', {
        input: { repo_path: 'projects/demo', number: 3 },
      }),
    ])
    expect(afterMerge).toEqual({ openPR: false, mergePR: false })
  })

  it('can offer both when dirty again after an open PR', () => {
    expect(
      inferDeliveryNeed([
        tool('CreatePullRequest', {
          input: { repo_path: 'projects/demo', title: 'feat' },
        }),
        tool('Edit', {
          kind: 'edit',
          input: { path: 'projects/demo/b.ts', old_string: 'x', new_string: 'y' },
        }),
      ]),
    ).toEqual({ openPR: true, mergePR: true })
  })

  it('ignores failed edits and failed PR creates', () => {
    expect(
      inferDeliveryNeed([
        tool('Edit', {
          status: 'failed',
          kind: 'edit',
          input: { path: 'projects/demo/a.ts', old_string: 'a', new_string: 'b' },
        }),
        tool('CreatePullRequest', {
          status: 'failed',
          input: { repo_path: 'projects/demo', title: 'feat' },
        }),
      ]),
    ).toEqual({ openPR: false, mergePR: false })
  })
})
