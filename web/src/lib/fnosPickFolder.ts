type PickResult = { code?: number; msg?: string; data?: unknown }

type TrimSdk = {
  isStandaloneWeb?: boolean
  pickUserFile: (params: {
    directory: boolean
    title?: string
    okText?: string
    sidebarGroup?: string[]
  }) => Promise<PickResult | undefined>
}

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
export function resolvePickedDirectory(res: PickResult | undefined | null): string | null {
  if (res == null) {
    throw new Error('未能打开飞牛文件夹选择器，请从飞牛桌面打开本应用后再试')
  }
  const path = firstPickedPath(res.data)
  if (path) return path
  // Empty data: treat as cancel when code is missing/0; otherwise surface host msg.
  if (res.code === undefined || res.code === 0) return null
  throw new Error(res.msg?.trim() || `选择文件夹失败（code ${res.code}）`)
}

// pickUserDirectory opens the fnOS picker for one folder in the user's own files.
// Returns null when the user cancels. Throws on host/bridge errors.
export async function pickUserDirectory(title: string): Promise<string | null> {
  const mod = (await import('@trimjs/web-app')) as { TrimApp: new () => TrimSdk }
  const sdk = new mod.TrimApp()
  if (sdk.isStandaloneWeb) {
    throw new Error('standalone')
  }
  const res = await sdk.pickUserFile({
    directory: true,
    title,
    okText: '确定',
    // Match fnOS docs so users can pick from shares/favorites, not only 我的文件.
    sidebarGroup: ['myFiles', 'otherShare', 'favorites'],
  })
  return resolvePickedDirectory(res)
}
