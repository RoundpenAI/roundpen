/** Minimal handle for an open chat event stream (SSE). */
export type ChatStreamHandle = {
  readyState: number
  close: () => void
}

export const STREAM_CONNECTING = 0
export const STREAM_OPEN = 1
export const STREAM_CLOSED = 3

/**
 * Tear down a stream without triggering reconnect handlers on a replaced
 * attachment (Strict Mode remount / rapid reconnect).
 */
export function detachStream(handle: ChatStreamHandle | null | undefined): void {
  if (!handle) return
  try {
    handle.close()
  } catch {
    /* ignore */
  }
}

/** Only the stream currently owned by the effect may flip UI to open. */
export function shouldApplyStreamOpen(
  disposed: boolean,
  owned: ChatStreamHandle | null,
  handle: ChatStreamHandle,
): boolean {
  return !disposed && owned === handle
}

export function streamLooksOpen(
  handle: ChatStreamHandle | null | undefined,
): boolean {
  return handle != null && handle.readyState === STREAM_OPEN
}
