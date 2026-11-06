export type ThoughtView = {
  text: string
  streaming?: boolean
  startedAtMs?: number
  durationMs?: number
}

/** One activity turn → one 已思考 row (join segments; sum durations). */
export function mergeThoughtViews(thoughts: ThoughtView[]): ThoughtView | null {
  const parts = thoughts.filter((t) => t.text.trim())
  if (parts.length === 0) return null
  if (parts.length === 1) return parts[0] ?? null
  const streaming = parts.some((t) => t.streaming)
  let startedAtMs: number | undefined
  for (const t of parts) {
    if (t.startedAtMs == null) continue
    if (startedAtMs == null || t.startedAtMs < startedAtMs) {
      startedAtMs = t.startedAtMs
    }
  }
  let durationMs: number | undefined
  if (!streaming) {
    durationMs = parts.reduce((sum, t) => sum + (t.durationMs ?? 0), 0)
  }
  return {
    text: parts.map((t) => t.text).join('\n\n'),
    streaming,
    startedAtMs,
    durationMs,
  }
}
