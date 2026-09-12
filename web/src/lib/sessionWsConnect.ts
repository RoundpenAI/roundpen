/**
 * Tear down a WebSocket without triggering reconnect handlers.
 * Concurrent connect() calls must call this on the previous socket first,
 * otherwise an orphaned socket can open while UI ignores its onopen
 * (wsRef already points at a newer socket) and stay stuck on 「连接中」.
 */
export function detachSocket(socket: WebSocket | null | undefined): void {
  if (!socket) return
  socket.onopen = null
  socket.onmessage = null
  socket.onerror = null
  socket.onclose = null
  if (
    socket.readyState === WebSocket.OPEN ||
    socket.readyState === WebSocket.CONNECTING
  ) {
    try {
      socket.close(1000, 'replaced')
    } catch {
      /* ignore */
    }
  }
}

/** Only the socket currently owned by the effect may flip UI to open. */
export function shouldApplySocketOpen(
  disposed: boolean,
  owned: WebSocket | null,
  socket: WebSocket,
): boolean {
  return !disposed && owned === socket
}

/**
 * UI "connected" must follow readyState, not only the onopen event.
 * Missed onopen (Strict Mode / proxy flush) left the chip on 「连接中」
 * until the connect timeout tore the live socket down and reconnected.
 */
export function socketLooksOpen(socket: WebSocket | null | undefined): boolean {
  return socket != null && socket.readyState === WebSocket.OPEN
}
