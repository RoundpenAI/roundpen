import type { ProjectWorker } from '../api'

/** Compact elapsed age: `45s`, `12m`, `2h 5m`, `3d`. */
export function formatElapsed(
  fromIso: string | undefined | null,
  now = Date.now(),
): string {
  if (!fromIso) return ''
  const t = new Date(fromIso).getTime()
  if (Number.isNaN(t)) return ''
  const sec = Math.max(0, Math.floor((now - t) / 1000))
  if (sec < 60) return `${sec}s`
  const min = Math.floor(sec / 60)
  if (min < 60) return `${min}m`
  const h = Math.floor(min / 60)
  const rem = min % 60
  if (h < 48) return rem ? `${h}h ${rem}m` : `${h}h`
  return `${Math.floor(h / 24)}d`
}

/** Running with empty transcript >2m, or quiet >15m after last message. */
export function workerLooksStuck(
  w: Pick<
    ProjectWorker,
    'status' | 'messageCount' | 'startedAt' | 'lastMessageAt'
  >,
  now = Date.now(),
): boolean {
  if (w.status !== 'running') return false
  const msgs = w.messageCount ?? 0
  if (msgs === 0) {
    const started = w.startedAt ? new Date(w.startedAt).getTime() : NaN
    return !Number.isNaN(started) && now - started > 2 * 60_000
  }
  const last = w.lastMessageAt ? new Date(w.lastMessageAt).getTime() : NaN
  return !Number.isNaN(last) && now - last > 15 * 60_000
}

/** Anchor time for "how long in this state". */
export function workerAgeAnchor(w: ProjectWorker): string | undefined {
  if (w.status === 'running') {
    return w.lastMessageAt || w.startedAt || w.updatedAt
  }
  if (w.status === 'queued') return w.createdAt
  return w.finishedAt || w.updatedAt
}
