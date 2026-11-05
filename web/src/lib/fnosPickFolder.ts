type PickResult = { code?: number; msg?: string; data?: unknown }

type AppAuthResult = {
  status?: 'success' | 'cancel' | 'error' | string
  error?: string
  method?: string
  appName?: string
  state?: string
  path?: string[]
}

type TrimSdk = {
  isStandaloneWeb?: boolean
  ready: () => Promise<void>
  pickUserFile: (params: {
    directory: boolean
    title?: string
    okText?: string
    sidebarGroup?: string[]
  }) => Promise<PickResult | undefined>
  openAppAuth: (
    method: 'pickUserFile',
    params: {
      appName: string
      directory?: boolean
      sidebarGroup?: string[]
      redirectUri?: string
      state?: string
    },
    options?: { target?: string; features?: string },
  ) => Promise<string>
}

/** Must match deploy/fnos/manifest appname and auth-callback.html. */
export const FNOS_APP_NAME = 'roundpen'

/** postMessage type from public/auth-callback.html → opener. */
export const FNOS_AUTH_RESULT_TYPE = 'roundpen:fnos-auth-result'

const AUTH_TIMEOUT_MS = 5 * 60 * 1000

const PICK_SIDEBAR = ['myFiles', 'otherShare', 'favorites'] as const

/** Normalize pickUserFile `data` into the first absolute path, if any. */
export function firstPickedPath(data: unknown): string | null {
  if (typeof data === 'string') {
    const p = data.trim()
    return p || null
  }
  if (!Array.isArray(data) || data.length === 0) return null
  const raw = data[0]
  if (typeof raw === 'string') {
    const p = raw.trim()
    return p || null
  }
  if (raw && typeof raw === 'object') {
    const path = (raw as { path?: unknown }).path
    if (typeof path === 'string') {
      const p = path.trim()
      return p || null
    }
  }
  return null
}

/** Resolve a pickUserFile bridge response into a path, cancel, or error. */
export function resolvePickedDirectory(
  res: PickResult | undefined | null,
): string | null {
  if (res == null) {
    throw new Error(
      '未能打开飞牛文件夹选择器，请从飞牛桌面打开本应用后再试',
    )
  }
  const path = firstPickedPath(res.data)
  if (path) return path
  // Empty data: treat as cancel when code is missing/0; otherwise surface host msg.
  if (res.code === undefined || res.code === 0) return null
  throw new Error(res.msg?.trim() || `选择文件夹失败（code ${res.code}）`)
}

function newAuthState(): string {
  try {
    return crypto.randomUUID()
  } catch {
    return `rp-${Date.now()}-${Math.random().toString(16).slice(2)}`
  }
}

function authCallbackPath(): string {
  // Same as api/client appBasePath — keep local to avoid pulling the API
  // client into node unit tests.
  const base = (import.meta.env.BASE_URL || '/').replace(/\/$/, '')
  return `${base}/auth-callback.html`
}

function loadTrimSdk(): Promise<{ TrimApp: new () => TrimSdk }> {
  return import('@trimjs/web-app') as Promise<{ TrimApp: new () => TrimSdk }>
}

async function pickInHost(sdk: TrimSdk, title: string): Promise<string | null> {
  const res = await sdk.pickUserFile({
    directory: true,
    title,
    okText: '确定',
    sidebarGroup: [...PICK_SIDEBAR],
  })
  return resolvePickedDirectory(res)
}

/**
 * Standalone browser (desktop type=url new window): openAppAuth + callback
 * postMessage. Official fnOS path when isStandaloneWeb is true.
 */
function pickViaAppAuth(sdk: TrimSdk): Promise<string | null> {
  const state = newAuthState()
  const redirectUri = authCallbackPath()

  return new Promise((resolve, reject) => {
    let settled = false
    const finish = (fn: () => void) => {
      if (settled) return
      settled = true
      window.removeEventListener('message', onMessage)
      window.clearTimeout(timer)
      fn()
    }

    const onMessage = (event: MessageEvent) => {
      if (event.origin !== window.location.origin) return
      const data = event.data as
        | { type?: string; result?: AppAuthResult }
        | undefined
      if (data?.type !== FNOS_AUTH_RESULT_TYPE) return
      const result = data.result
      if (!result || result.state !== state) return

      if (result.status === 'cancel') {
        finish(() => resolve(null))
        return
      }
      if (result.status === 'error') {
        finish(() =>
          reject(
            new Error(
              result.error === 'access_denied'
                ? 'access_denied'
                : result.error || 'auth failed',
            ),
          ),
        )
        return
      }
      finish(() => resolve(firstPickedPath(result.path)))
    }

    window.addEventListener('message', onMessage)
    const timer = window.setTimeout(() => {
      finish(() => reject(new Error('auth_timeout')))
    }, AUTH_TIMEOUT_MS)

    void sdk
      .openAppAuth(
        'pickUserFile',
        {
          appName: FNOS_APP_NAME,
          directory: true,
          sidebarGroup: [...PICK_SIDEBAR],
          redirectUri,
          state,
        },
        {
          target: '_blank',
          features: 'width=750,height=630',
        },
      )
      .catch((e) => {
        finish(() =>
          reject(e instanceof Error ? e : new Error(String(e))),
        )
      })
  })
}

// pickUserDirectory opens the fnOS picker for one folder in the user's own files.
// Works in the desktop iframe and in a standalone / type=url window.
// Returns null when the user cancels. Throws on host/bridge/auth errors.
export async function pickUserDirectory(title: string): Promise<string | null> {
  const mod = await loadTrimSdk()
  const sdk = new mod.TrimApp()
  await sdk.ready()
  if (sdk.isStandaloneWeb) {
    return pickViaAppAuth(sdk)
  }
  return pickInHost(sdk, title)
}
