export type ToolCallLike = {
  title: string
  status: string
  kind?: string
  input?: unknown
  output?: unknown
}

export type ToolBucket = 'edit' | 'explore' | 'search' | 'command' | 'other'

const PATH_KEYS = [
  'path',
  'file',
  'file_path',
  'filePath',
  'target_file',
  'targetFile',
  'filename',
  'fileName',
  'dest',
  'destination',
]

type Diff = { plus: number; minus: number }

export function classifyTool(call: ToolCallLike): ToolBucket {
  const kind = (call.kind || '').trim().toLowerCase()
  switch (kind) {
    case 'edit':
    case 'delete':
    case 'move':
      return 'edit'
    case 'read':
    case 'fetch':
      return 'explore'
    case 'search':
      return 'search'
    case 'execute':
      return 'command'
    default:
      break
  }

  const name = toolName(call)
  if (
    /^(edit|write|strreplace|str_replace|search_replace|searchreplace|apply_patch|applypatch|notebookedit|notebook_edit|delete|unlink|rm|move|mv|rename)$/.test(
      name,
    )
  ) {
    return 'edit'
  }
  if (
    /^(read|view|cat|glob|ls|list|listdir|list_dir|readdir|stat|webfetch|web_fetch|fetch)$/.test(
      name,
    ) ||
    name.startsWith('browser_')
  ) {
    return 'explore'
  }
  if (
    /^(grep|search|find|websearch|web_search|rg|ripgrep)$/.test(name)
  ) {
    return 'search'
  }
  if (
    /^(bash|shell|command|exec|terminal|run|sh)$/.test(name) ||
    name.startsWith('roundpen_ensure_')
  ) {
    return 'command'
  }
  return 'other'
}

type BucketAcc = {
  running: ToolCallLike[]
  done: ToolCallLike[]
}

export function formatGroupStats(calls: ToolCallLike[]): {
  label: string
  plus: number
  minus: number
} {
  const order: ToolBucket[] = []
  const byBucket = new Map<ToolBucket, BucketAcc>()
  let failed = 0
  let plus = 0
  let minus = 0

  for (const call of calls) {
    if (call.status === 'failed') failed += 1
    const bucket = classifyTool(call)
    if (!byBucket.has(bucket)) {
      byBucket.set(bucket, { running: [], done: [] })
      order.push(bucket)
    }
    const acc = byBucket.get(bucket)!
    if (isRunning(call.status)) acc.running.push(call)
    else acc.done.push(call)
    if (bucket === 'edit') {
      const d = diffForCall(call)
      plus += d.plus
      minus += d.minus
    }
  }

  const clauses: string[] = []
  for (const bucket of order) {
    const acc = byBucket.get(bucket)
    if (!acc) continue
    clauses.push(...phraseBucket(bucket, acc))
  }
  if (clauses.length === 0) {
    clauses.push(`${calls.length} tool ${plural(calls.length, 'call')}`)
  }
  if (failed) clauses.push(`${failed} failed`)

  return { label: joinClauses(clauses), plus, minus }
}

function isRunning(status: string): boolean {
  return status === 'pending' || status === 'in_progress'
}

function phraseBucket(bucket: ToolBucket, acc: BucketAcc): string[] {
  switch (bucket) {
    case 'edit':
      return phraseCounted(acc, {
        doneOne: (call) => `Edited ${fileLabel(call)}`,
        doneMany: (n) => `Edited ${n} files`,
        runOne: (call) => `Editing ${fileLabel(call)}`,
        runMany: (n) => `Editing ${n} files`,
        files: true,
      })
    case 'explore':
      return phraseExplore(acc)
    case 'search':
      return phraseCounted(acc, {
        doneOne: (call) => {
          const q = queryLabel(call)
          return q ? `Searched for ${q}` : '1 search'
        },
        doneMany: (n) => `${n} searches`,
        runOne: (call) => {
          const q = queryLabel(call)
          return q ? `Searching for ${q}` : 'Searching'
        },
        runMany: (n) => `Searching (${n})`,
      })
    case 'command':
      return phraseCounted(acc, {
        doneOne: (call) => `Ran ${commandLabel(call)}`,
        doneMany: (n) => `Ran ${n} commands`,
        runOne: (call) => `Running ${commandLabel(call)}`,
        runMany: (n) => `Running ${n} commands`,
      })
    default:
      return phraseOther(acc)
  }
}

function phraseExplore(acc: BucketAcc): string[] {
  const all = [...acc.done, ...acc.running]
  if (all.length > 0 && all.every((c) => toolName(c).startsWith('browser_'))) {
    return phraseCounted(acc, {
      doneOne: (call) => {
        const url = urlLabel(call)
        return url ? `Opened ${url}` : 'Used the browser'
      },
      doneMany: (n) => `Used the browser ${n} times`,
      runOne: (call) => {
        const url = urlLabel(call)
        return url ? `Opening ${url}` : 'Using the browser'
      },
      runMany: (n) => `Using the browser (${n})`,
    })
  }
  const listing = (c: ToolCallLike) =>
    /^(glob|ls|list|listdir|list_dir|readdir)$/.test(toolName(c))
  if (all.length > 0 && all.every(listing)) {
    return phraseCounted(acc, {
      doneOne: (call) => {
        const p = patternLabel(call)
        return p ? `Listed ${p}` : 'Listed files'
      },
      doneMany: (n) => `Listed ${n} times`,
      runOne: (call) => {
        const p = patternLabel(call)
        return p ? `Listing ${p}` : 'Listing files'
      },
      runMany: (n) => `Listing files (${n})`,
    })
  }
  return phraseCounted(acc, {
    doneOne: (call) => `Read ${fileLabel(call)}`,
    doneMany: (n) => `Explored ${n} files`,
    runOne: (call) => `Reading ${fileLabel(call)}`,
    runMany: (n) => `Exploring ${n} files`,
    files: true,
  })
}

function phraseOther(acc: BucketAcc): string[] {
  return phraseCounted(acc, {
    doneOne: (call) => call.title || 'Used a tool',
    doneMany: (n) => `Used ${n} tools`,
    runOne: (call) => call.title || 'Working',
    runMany: (n) => `Running ${n} tools`,
  })
}

function phraseCounted(
  acc: BucketAcc,
  how: {
    doneOne: (call: ToolCallLike) => string
    doneMany: (n: number) => string
    runOne: (call: ToolCallLike) => string
    runMany: (n: number) => string
    files?: boolean
  },
): string[] {
  const doneN = how.files ? countFiles(acc.done) : acc.done.length
  const runN = how.files ? countFiles(acc.running) : acc.running.length
  const out: string[] = []
  if (doneN) {
    out.push(
      doneN === 1 && acc.done[0] ? how.doneOne(acc.done[0]) : how.doneMany(doneN),
    )
  }
  if (runN) {
    out.push(
      runN === 1 && acc.running[0]
        ? how.runOne(acc.running[0])
        : how.runMany(runN),
    )
  }
  return out
}

function countFiles(calls: ToolCallLike[]): number {
  const files = new Set<string>()
  let nameless = 0
  for (const call of calls) {
    const paths = collectPaths(call, classifyTool(call) === 'explore')
    if (paths.length === 0) nameless += 1
    else paths.forEach((p) => files.add(p))
  }
  return files.size + nameless
}

function fileLabel(call: ToolCallLike): string {
  const paths = collectPaths(call, classifyTool(call) === 'explore')
  if (paths[0]) return shortPath(paths[0])
  return 'a file'
}

function shortPath(path: string): string {
  const clean = path.replace(/\\/g, '/').replace(/\/+$/, '')
  const parts = clean.split('/').filter(Boolean)
  const name = parts[parts.length - 1] || clean
  return clip(name, 40)
}

function queryLabel(call: ToolCallLike): string {
  const rec = asRecord(call.input)
  const raw =
    pickString(rec, ['pattern', 'query', 'regex', 'search', 'grep']) || ''
  return raw ? clip(raw, 32) : ''
}

function patternLabel(call: ToolCallLike): string {
  const rec = asRecord(call.input)
  const raw =
    pickString(rec, ['pattern', 'glob', 'glob_pattern', 'path', 'target']) || ''
  return raw ? clip(raw, 36) : ''
}

function commandLabel(call: ToolCallLike): string {
  const rec = asRecord(call.input)
  const raw =
    pickString(rec, ['command', 'cmd', 'script']) ||
    (typeof rec._ === 'string' ? rec._ : '')
  if (raw) return clip(raw.replace(/\s+/g, ' ').trim(), 42)
  const title = (call.title || '').replace(/^(Bash|Shell|Command)\s*[·:]\s*/i, '')
  return clip(title || 'a command', 42)
}

function urlLabel(call: ToolCallLike): string {
  const rec = asRecord(call.input)
  const raw = pickString(rec, ['url', 'href', 'target']) || ''
  if (!raw) return ''
  try {
    return clip(new URL(raw).host || raw, 36)
  } catch {
    return clip(raw, 36)
  }
}

function asRecord(value: unknown): Record<string, unknown> {
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    return value as Record<string, unknown>
  }
  return {}
}

function clip(value: string, max: number): string {
  return value.length > max ? `${value.slice(0, max)}…` : value
}

function joinClauses(parts: string[]): string {
  if (parts.length === 0) return ''
  const rest = parts.slice(1).map((p) => p.charAt(0).toLowerCase() + p.slice(1))
  return [parts[0], ...rest].join(', ')
}

function toolName(call: ToolCallLike): string {
  const raw = (call.title || '').trim()
  const first = raw.split(/[\s:·]+/, 1)[0] || ''
  return first.toLowerCase().replace(/[^a-z0-9_]/g, '')
}

function collectPaths(call: ToolCallLike, includeOutputList: boolean): string[] {
  const found = new Set<string>()
  addPathsFromValue(call.input, found)
  if (includeOutputList) addOutputPathList(call.output, found)
  return [...found]
}

function addPathsFromValue(value: unknown, into: Set<string>): void {
  if (value == null) return
  if (typeof value === 'string') {
    const p = normalizePath(value)
    if (p && looksLikePath(p) && !p.includes('\n')) into.add(p)
    return
  }
  if (Array.isArray(value)) {
    for (const item of value) addPathsFromValue(item, into)
    return
  }
  if (typeof value !== 'object') return
  const rec = value as Record<string, unknown>
  for (const key of PATH_KEYS) {
    const v = rec[key]
    if (typeof v === 'string') {
      const p = normalizePath(v)
      if (p) into.add(p)
    } else if (Array.isArray(v)) {
      for (const item of v) {
        if (typeof item === 'string') {
          const p = normalizePath(item)
          if (p) into.add(p)
        }
      }
    }
  }
  if (Array.isArray(rec.paths)) {
    for (const item of rec.paths) {
      if (typeof item === 'string') {
        const p = normalizePath(item)
        if (p) into.add(p)
      }
    }
  }
}

function addOutputPathList(output: unknown, into: Set<string>): void {
  const text = outputText(output)
  if (!text) return
  const lines = text.split(/\r?\n/)
  if (lines.length > 80) return
  for (const line of lines) {
    const trimmed = line.trim()
    if (!trimmed || trimmed.startsWith('{') || trimmed.startsWith('[')) continue
    if (/\s/.test(trimmed)) continue
    if (!looksLikePath(trimmed)) continue
    const p = normalizePath(trimmed)
    if (p) into.add(p)
  }
}

function outputText(output: unknown): string {
  if (output == null) return ''
  if (typeof output === 'string') return output
  if (typeof output === 'object' && output !== null) {
    const rec = output as Record<string, unknown>
    for (const key of ['content', 'text', 'output', 'result', 'stdout']) {
      if (typeof rec[key] === 'string') return rec[key]
    }
  }
  return ''
}

function normalizePath(raw: string): string {
  return raw.trim().replace(/\\/g, '/')
}

function looksLikePath(value: string): boolean {
  if (!value || value.length > 512) return false
  if (value.startsWith('/') || value.startsWith('./') || value.startsWith('../')) {
    return true
  }
  return /[\\/]/.test(value) || /\.[a-zA-Z0-9]{1,8}$/.test(value)
}

function diffForCall(call: ToolCallLike): Diff {
  const fromStrings = stringsDiff(call.input)
  if (fromStrings) return fromStrings
  const fromOutput = unifiedDiff(outputText(call.output))
  if (fromOutput.plus || fromOutput.minus) return fromOutput
  return { plus: 0, minus: 0 }
}

function stringsDiff(input: unknown): Diff | null {
  if (!input || typeof input !== 'object') return null
  const rec = input as Record<string, unknown>
  const oldStr = pickString(rec, [
    'old_string',
    'oldString',
    'old_str',
    'oldText',
  ])
  const newStr = pickString(rec, [
    'new_string',
    'newString',
    'new_str',
    'newText',
  ])
  if (oldStr != null && newStr != null) return lineDelta(oldStr, newStr)

  const patch = pickString(rec, ['patch', 'diff', 'unified_diff'])
  if (patch) {
    const d = unifiedDiff(patch)
    if (d.plus || d.minus) return d
  }
  return null
}

function pickString(
  rec: Record<string, unknown>,
  keys: string[],
): string | null {
  for (const key of keys) {
    if (typeof rec[key] === 'string') return rec[key]
  }
  return null
}

function lineDelta(oldText: string, newText: string): Diff {
  const oldLines = oldText.split('\n')
  const newLines = newText.split('\n')
  const bag = new Map<string, number>()
  for (const line of oldLines) {
    bag.set(line, (bag.get(line) ?? 0) + 1)
  }
  let common = 0
  for (const line of newLines) {
    const n = bag.get(line) ?? 0
    if (n > 0) {
      bag.set(line, n - 1)
      common += 1
    }
  }
  return {
    plus: newLines.length - common,
    minus: oldLines.length - common,
  }
}

function unifiedDiff(text: string): Diff {
  let plus = 0
  let minus = 0
  for (const line of text.split(/\r?\n/)) {
    if (line.startsWith('+++') || line.startsWith('---') || line.startsWith('@@')) {
      continue
    }
    if (line.startsWith('+')) plus += 1
    else if (line.startsWith('-')) minus += 1
  }
  return { plus, minus }
}

function plural(n: number, one: string, many = `${one}s`): string {
  return n === 1 ? one : many
}
