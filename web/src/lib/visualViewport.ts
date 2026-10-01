/** Keep the app shell sized to the visual viewport (iOS Safari keyboard / PWA). */

const KEYBOARD_OPEN_PX = 80

export function keyboardInset(
  innerHeight: number,
  vvHeight: number,
  vvOffsetTop: number,
): number {
  return Math.max(0, innerHeight - vvHeight - vvOffsetTop)
}

/** Home-screen / installed app (iOS navigator.standalone or display-mode). */
export function isStandaloneDisplay(win: Window = window): boolean {
  const nav = win.navigator as Navigator & { standalone?: boolean }
  if (nav.standalone) return true
  try {
    return (
      win.matchMedia('(display-mode: standalone)').matches ||
      win.matchMedia('(display-mode: fullscreen)').matches ||
      win.matchMedia('(display-mode: minimal-ui)').matches
    )
  } catch {
    return false
  }
}

export type ViewportMetrics = {
  innerHeight: number
  vvHeight: number
  vvOffsetTop: number
  standalone: boolean
}

export type AppViewport = {
  height: number
  offsetTop: number
  keyboardInset: number
  keyboardOpen: boolean
}

/**
 * Resolve CSS viewport vars. In standalone PWA, Safari often reports a
 * residual visualViewport offset/height as if the URL bar still existed —
 * ignore that when the keyboard is closed.
 */
export function resolveAppViewport(m: ViewportMetrics): AppViewport {
  const inset = keyboardInset(m.innerHeight, m.vvHeight, m.vvOffsetTop)
  const keyboardOpen = inset > KEYBOARD_OPEN_PX
  if (m.standalone && !keyboardOpen) {
    return {
      height: m.innerHeight,
      offsetTop: 0,
      keyboardInset: 0,
      keyboardOpen: false,
    }
  }
  return {
    height: m.vvHeight,
    // Standalone + keyboard: keep top at 0 so we do not double-count Safari's
    // layout scroll (offsetTop) on top of the shrunk height.
    offsetTop: m.standalone ? 0 : m.vvOffsetTop,
    keyboardInset: inset,
    keyboardOpen,
  }
}

export function syncVisualViewportInsets(
  root: HTMLElement = document.documentElement,
  win: Window = window,
): void {
  const standalone = isStandaloneDisplay(win)
  root.classList.toggle('display-standalone', standalone)

  const vv = win.visualViewport
  const resolved = resolveAppViewport({
    innerHeight: win.innerHeight,
    vvHeight: vv?.height ?? win.innerHeight,
    vvOffsetTop: vv?.offsetTop ?? 0,
    standalone,
  })

  root.style.setProperty('--app-height', `${resolved.height}px`)
  root.style.setProperty('--vv-offset-top', `${resolved.offsetTop}px`)
  root.style.setProperty('--keyboard-inset', `${resolved.keyboardInset}px`)
  root.classList.toggle('keyboard-open', resolved.keyboardOpen)
}

/** Bind visualViewport listeners; call once at app boot. Returns cleanup. */
export function installVisualViewportInsets(
  root: HTMLElement = document.documentElement,
  win: Window = window,
): () => void {
  const sync = () => syncVisualViewportInsets(root, win)
  sync()
  const vv = win.visualViewport
  vv?.addEventListener('resize', sync)
  vv?.addEventListener('scroll', sync)
  win.addEventListener('resize', sync)
  win.addEventListener('orientationchange', sync)

  const medias = [
    '(display-mode: standalone)',
    '(display-mode: fullscreen)',
    '(display-mode: minimal-ui)',
  ]
    .map((q) => {
      try {
        return win.matchMedia(q)
      } catch {
        return null
      }
    })
    .filter((m): m is MediaQueryList => m != null)
  for (const mq of medias) {
    mq.addEventListener('change', sync)
  }

  return () => {
    vv?.removeEventListener('resize', sync)
    vv?.removeEventListener('scroll', sync)
    win.removeEventListener('resize', sync)
    win.removeEventListener('orientationchange', sync)
    for (const mq of medias) {
      mq.removeEventListener('change', sync)
    }
  }
}
