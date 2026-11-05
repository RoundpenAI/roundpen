export type ThoughtSeg = {
  text: string
  streaming?: boolean
  startedAtMs?: number
  durationMs?: number
}

/**
 * Tool boundaries split one turn's reasoning into several completed segments
 * ("已思考1秒" / "已思考2秒"). Fold them into a single block — it's still the
 * same stretch of thinking — with durations summed (or a live timer if any
 * segment is still streaming).
 */
export function mergeThoughtSegments(thoughts: ThoughtSeg[]): ThoughtSeg[] {
  const parts = thoughts.filter((t) => (t.text || '').trim())
  if (parts.length <= 1) return parts
  const text = parts.map((t) => t.text.trim()).join('\n\n')
  const streaming = parts.some((t) => t.streaming)
  let startedAtMs: number | undefined
  for (const t of parts) {
    if (typeof t.startedAtMs !== 'number') continue
    startedAtMs =
      startedAtMs === undefined
        ? t.startedAtMs
        : Math.min(startedAtMs, t.startedAtMs)
  }
  let durationMs: number | undefined
  if (!streaming) {
    durationMs = parts.reduce((sum, t) => {
      if (typeof t.durationMs === 'number' && t.durationMs >= 0) {
        return sum + t.durationMs
      }
      return sum
    }, 0)
  }
  return [{ text, streaming, startedAtMs, durationMs }]
}
