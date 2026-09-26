import type { DirEntry } from '../api'

/** Join a directory path with an entry name ("." means the workspace root). */
export function joinPath(base: string, name: string): string {
  if (!base || base === '.') return name
  return `${base.replace(/\/+$/, '')}/${name}`
}

/** Parent directory of a workspace-relative path ("." at the root). */
export function parentPath(path: string): string {
  const parts = path.split('/').filter(Boolean)
  parts.pop()
  return parts.length ? parts.join('/') : '.'
}

export type Crumb = { label: string; path: string }

/** Breadcrumb segments from the workspace root down to `path` ("." first). */
export function crumbSegments(path: string): Crumb[] {
  const out: Crumb[] = [{ label: '.', path: '.' }]
  let acc = ''
  for (const part of path.split('/').filter((p) => p && p !== '.')) {
    acc = acc ? `${acc}/${part}` : part
    out.push({ label: part, path: acc })
  }
  return out
}

export function extensionOf(name: string): string {
  const i = name.lastIndexOf('.')
  if (i <= 0 || i === name.length - 1) return ''
  return name.slice(i + 1).toLowerCase()
}

export type FileKind = 'folder' | 'text' | 'image' | 'pdf' | 'other'

// Kept in step with the server's inlineContentType whitelist: svg/html count
// as text because the API serves them as text/plain on purpose.
const IMAGE_EXT = new Set(['png', 'jpg', 'jpeg', 'gif', 'webp'])
const TEXT_EXT = new Set([
  'txt', 'md', 'markdown', 'json', 'yaml', 'yml', 'toml', 'csv', 'log',
  'sh', 'bash', 'py', 'js', 'mjs', 'ts', 'tsx', 'jsx', 'css', 'go', 'rs',
  'java', 'c', 'h', 'cpp', 'rb', 'php', 'sql', 'html', 'htm', 'svg', 'xml',
])

export function fileKind(entry: Pick<DirEntry, 'name' | 'is_dir'>): FileKind {
  if (entry.is_dir) return 'folder'
  const ext = extensionOf(entry.name)
  if (IMAGE_EXT.has(ext)) return 'image'
  if (ext === 'pdf') return 'pdf'
  if (TEXT_EXT.has(ext)) return 'text'
  return 'other'
}

export function formatBytes(size: number): string {
  if (!Number.isFinite(size) || size <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = size
  let unit = 0
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024
    unit += 1
  }
  const digits = unit === 0 ? 0 : value >= 100 ? 0 : value >= 10 ? 1 : 2
  return `${value.toFixed(digits)} ${units[unit]}`
}

/** Local "YYYY-MM-DD HH:mm"; the Go zero time (0001-01-01) reads as "—". */
export function formatTime(iso: string | undefined): string {
  if (!iso) return '—'
  const t = Date.parse(iso)
  if (!Number.isFinite(t) || t <= 0) return '—'
  const d = new Date(t)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

/** Directories first, then case-insensitive name order. */
export function sortEntries(entries: DirEntry[]): DirEntry[] {
  return [...entries].sort((a, b) => {
    if (a.is_dir !== b.is_dir) return a.is_dir ? -1 : 1
    return a.name.localeCompare(b.name)
  })
}

/** Full destination path when renaming `name` inside `dir`. */
export function renameDest(dir: string, next: string): string {
  return joinPath(dir, next.trim())
}

/** Names between two selection anchors inclusive, in on-screen order. */
export function rangeKeys(names: string[], from: string, to: string): string[] {
  const a = names.indexOf(from)
  const b = names.indexOf(to)
  if (a < 0 || b < 0) return [to]
  const [lo, hi] = a <= b ? [a, b] : [b, a]
  return names.slice(lo, hi + 1)
}
