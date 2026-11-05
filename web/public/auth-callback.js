// Keep in sync with web/src/lib/fnosPickFolder.ts (FNOS_AUTH_RESULT_TYPE).
const MSG_TYPE = 'roundpen:fnos-auth-result'

function parseCallback(href) {
  const params = new URL(href).searchParams
  const method = params.get('method') || undefined
  const pathRaw = params.get('path')
  let path
  if (pathRaw) {
    try {
      const parsed = JSON.parse(pathRaw)
      if (Array.isArray(parsed) && parsed.every((p) => typeof p === 'string')) {
        path = parsed
      }
    } catch {
      /* ignore */
    }
  }
  return {
    status: params.get('status') || undefined,
    error: params.get('error') || undefined,
    method,
    appName: params.get('appName') || undefined,
    state: params.get('state') || undefined,
    path,
  }
}

const result = parseCallback(window.location.href)
const msg = document.getElementById('msg')

if (window.opener && !window.opener.closed) {
  window.opener.postMessage(
    { type: MSG_TYPE, result },
    window.location.origin,
  )
  if (msg) {
    msg.textContent =
      result.status === 'success'
        ? '授权成功，可关闭此窗口。'
        : result.status === 'cancel'
          ? '已取消。'
          : '授权结束，可关闭此窗口。'
  }
  try {
    window.close()
  } catch {
    /* some browsers block close */
  }
} else if (msg) {
  msg.textContent =
    '授权已完成。请回到 AI助手 窗口，如未自动更新请再点一次「选择文件夹」。'
}
