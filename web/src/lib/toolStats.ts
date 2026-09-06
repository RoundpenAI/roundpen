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

export function formatGroupStats(calls: ToolCallLike[]): {
  label: string
  plus: number
  minus: number
} {
  const editFiles = new Set<string>()
  const exploreFiles = new Set<string>()
  let namelessEdits = 0
  let namelessExplores = 0
  let searches = 0
  let commands = 0
  let failed = 0
  let plus = 0
  let minus = 0

  for (const call of calls) {
    if (call.status === 'failed') failed += 1
    const bucket = classifyTool(call)

    switch (bucket) {
      case 'edit': {
        const paths = collectPaths(call, false)
        if (paths.length === 0) namelessEdits += 1
        else paths.forEach((p) => editFiles.add(p))
        const d = diffForCall(call)
        plus += d.plus
        minus += d.minus
        break
      }
      case 'explore': {
        const paths = collectPaths(call, true)
        if (paths.length === 0) namelessExplores += 1
        else paths.forEach((p) => exploreFiles.add(p))
        break
      }
      case 'search':
        searches += 1
        break
      case 'command':
        commands += 1
        break
      default:
        break
    }
  }

  const edited = editFiles.size + namelessEdits
  const explored = exploreFiles.size + namelessExplores
  const parts: string[] = []

  if (edited) {
    parts.push(`Editing ${edited} ${plural(edited, 'file')}`)
  }
  if (explored) {
    parts.push(`explored ${explored} ${plural(explored, 'file')}`)
  }
  if (searches) {
    parts.push(`${searches} ${plural(searches, 'search', 'searches')}`)
  }
  if (commands) {
    parts.push(`ran ${commands} ${plural(commands, 'command')}`)
  }
  if (parts.length === 0) {
    parts.push(`${calls.length} tool ${plural(calls.length, 'call')}`)
  }
  if (failed) {
    parts.push(`${failed} failed`)
  }

  return { label: parts.join(', '), plus, minus }
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
