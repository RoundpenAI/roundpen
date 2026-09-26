import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import type { DirEntry } from '../api'
import {
  crumbSegments,
  fileKind,
  formatBytes,
  formatTime,
  joinPath,
  parentPath,
  rangeKeys,
  renameDest,
  sortEntries,
} from './workspaceFiles.ts'

describe('paths', () => {
  it('joins and walks parents', () => {
    assert.equal(joinPath('.', 'a.txt'), 'a.txt')
    assert.equal(joinPath('sub', 'a.txt'), 'sub/a.txt')
    assert.equal(joinPath('a/b/', 'c'), 'a/b/c')
    assert.equal(parentPath('a/b/c'), 'a/b')
    assert.equal(parentPath('a'), '.')
    assert.equal(parentPath('.'), '.')
  })

  it('builds breadcrumbs from the root down', () => {
    assert.deepEqual(crumbSegments('.'), [{ label: '.', path: '.' }])
    assert.deepEqual(crumbSegments('a/b'), [
      { label: '.', path: '.' },
      { label: 'a', path: 'a' },
      { label: 'b', path: 'a/b' },
    ])
  })

  it('renameDest builds the full target path', () => {
    assert.equal(renameDest('.', 'new.txt'), 'new.txt')
    assert.equal(renameDest('sub', '  new.txt '), 'sub/new.txt')
  })
})

describe('fileKind', () => {
  const kind = (name: string, is_dir = false) => fileKind({ name, is_dir })
  it('classifies folders, images, pdf and text', () => {
    assert.equal(kind('docs', true), 'folder')
    assert.equal(kind('pic.PNG'), 'image')
    assert.equal(kind('doc.pdf'), 'pdf')
    assert.equal(kind('main.go'), 'text')
    assert.equal(kind('page.html'), 'text')
    assert.equal(kind('archive.zip'), 'other')
    assert.equal(kind('Makefile'), 'other')
    assert.equal(kind('.gitignore'), 'other')
    assert.equal(kind('trailing.'), 'other')
  })
})

describe('formatting', () => {
  it('formats sizes', () => {
    assert.equal(formatBytes(0), '0 B')
    assert.equal(formatBytes(512), '512 B')
    assert.equal(formatBytes(1024), '1.00 KB')
    assert.equal(formatBytes(15 * 1024), '15.0 KB')
    assert.equal(formatBytes(200 * 1024), '200 KB')
    assert.equal(formatBytes(5 * 1024 * 1024), '5.00 MB')
  })

  it('formats times and survives the Go zero value', () => {
    assert.equal(formatTime(undefined), '—')
    assert.equal(formatTime(''), '—')
    assert.equal(formatTime('0001-01-01T00:00:00Z'), '—')
    const out = formatTime('2026-09-25T10:05:00Z')
    assert.match(out, /^2026-09-25 \d{2}:05$/)
  })
})

describe('sortEntries', () => {
  it('puts directories first then names', () => {
    const rows: DirEntry[] = [
      { name: 'b.txt', is_dir: false, size: 1 },
      { name: 'a', is_dir: true, size: 0 },
      { name: 'A.txt', is_dir: false, size: 2 },
    ]
    assert.deepEqual(
      sortEntries(rows).map((e) => e.name),
      ['a', 'A.txt', 'b.txt'],
    )
  })
})

describe('rangeKeys', () => {
  it('selects the inclusive span in list order', () => {
    const names = ['a', 'b', 'c', 'd']
    assert.deepEqual(rangeKeys(names, 'a', 'c'), ['a', 'b', 'c'])
    assert.deepEqual(rangeKeys(names, 'c', 'b'), ['b', 'c'])
    assert.deepEqual(rangeKeys(names, 'x', 'b'), ['b'])
  })
})
