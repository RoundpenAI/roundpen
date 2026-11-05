import { isFnOSApp } from '../api/client'
import { pickUserDirectory } from './fnosPickFolder'

export type FnosOpenAction = 'open' | 'details' | 'manager'

export type FnosClientAction = {
  kind: 'fnos.open'
  action: FnosOpenAction
  path: string
}

type TrimOpenSdk = {
  isStandaloneWeb?: boolean
  openFile: (path: string) => Promise<unknown>
  showFileDetails: (paths: string[], options?: object) => Promise<unknown>
  openFileManager: (path: string) => Promise<unknown>
}

/** True when the SPA can call Trim host open APIs. */
export function canOpenOnFnOSHost(): boolean {
  return isFnOSApp()
}

/**
 * Parse an OpenHostPath (or similar) tool result for a fnOS client action.
 */
export function parseFnosClientAction(output: unknown): FnosClientAction | null {
  const obj = asObject(output)
  if (!obj) return null
  const raw = obj._clientAction
  const actionObj = asObject(raw)
  if (!actionObj || actionObj.kind !== 'fnos.open') return null
  const path = String(actionObj.path ?? '').trim()
  if (!path) return null
  let action = String(actionObj.action ?? 'open').trim().toLowerCase()
  if (action !== 'details' && action !== 'manager') action = 'open'
  return { kind: 'fnos.open', action: action as FnosOpenAction, path }
}

/**
 * Open a host path via Trim JS SDK. Returns false when not in fnOS host UI.
 */
export async function fulfillFnosOpen(action: FnosClientAction): Promise<boolean> {
  if (!canOpenOnFnOSHost()) return false
  const mod = (await import('@trimjs/web-app')) as { TrimApp: new () => TrimOpenSdk }
  const sdk = new mod.TrimApp()
  if (sdk.isStandaloneWeb) {
    throw new Error('standalone')
  }
  switch (action.action) {
    case 'details':
      await sdk.showFileDetails([action.path])
      break
    case 'manager':
      await sdk.openFileManager(action.path)
      break
    default:
      await sdk.openFile(action.path)
  }
  return true
}

/** Re-export folder picker for settings/NAS flows. */
export { pickUserDirectory }

function asObject(v: unknown): Record<string, unknown> | null {
  if (!v) return null
  if (typeof v === 'string') {
    try {
      const parsed = JSON.parse(v) as unknown
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        return parsed as Record<string, unknown>
      }
    } catch {
      return null
    }
    return null
  }
  if (typeof v === 'object' && !Array.isArray(v)) {
    return v as Record<string, unknown>
  }
  return null
}
