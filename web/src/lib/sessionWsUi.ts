export type WsUiStatus = 'connecting' | 'open' | 'error'

export type WsUiEvent =
  | 'effect_start'
  | 'socket_open'
  | 'socket_close'
  | 'effect_cleanup'
  | 'socket_error'
  | 'connect_timeout'

/** How long to wait for WebSocket onopen before surfacing a timeout. */
export const WS_CONNECT_TIMEOUT_MS = 8000

/**
 * Single source of truth for chat WS chrome (header + input).
 * Cleanup must not flip status — under React Strict Mode that races remount onopen.
 * Remounted effects must emit effect_start so UI leaves stale "已连接" while reconnecting.
 */
export function nextWsUiStatus(
  current: WsUiStatus,
  event: WsUiEvent,
): WsUiStatus {
  switch (event) {
    case 'effect_start':
    case 'socket_close':
      return 'connecting'
    case 'socket_open':
      return 'open'
    case 'socket_error':
    case 'connect_timeout':
      return 'error'
    case 'effect_cleanup':
      return current
  }
}

/**
 * Text for the header connection chip.
 * When detail is set (timeout / close / error), show it in place of bare「连接中」.
 */
export function wsStatusLabel(
  status: WsUiStatus,
  detail?: string | null,
): string {
  if (status === 'open') return '已连接'
  const d = detail?.trim()
  if (d) return d
  return status === 'error' ? '连接失败' : '连接中'
}

export function wsConnectTimeoutDetail(): string {
  return `连接超时（${WS_CONNECT_TIMEOUT_MS / 1000}s）`
}

export function wsCloseDetail(code: number, reason: string): string {
  const r = reason.trim()
  if (code === 1000) return r || '连接已关闭'
  if (code === 1006) return '连接异常中断（可能未鉴权或服务未升级）'
  if (code === 1008) return r || '连接被拒绝'
  if (code === 1011) return r || '服务端错误'
  if (r) return `连接关闭 ${code}: ${r}`
  return `连接关闭（${code}）`
}

/** Composer always invites input; connection state is only a header hint. */
export function wsInputPlaceholder(_status: WsUiStatus): string {
  return '给助手发消息…'
}

/**
 * Never force-disable send for connecting — queue until the socket is ready
 * (WeChat-style). Semi still gates empty content when this is undefined.
 */
export function wsCanSendProp(_status: WsUiStatus): boolean | undefined {
  return undefined
}

/**
 * Delay before the Nth reconnect after the last successful open.
 * attempt 0 = first failure → retry immediately (tab switch / HMR remount).
 */
export function wsReconnectDelayMs(attempt: number): number {
  if (attempt <= 0) return 0
  return Math.min(1000 * 2 ** (attempt - 1), 8000)
}
