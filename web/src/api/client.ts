export type User = {
  username: string
  email: string
  fullname: string
  orgName: string
  apiKey: string
  role: string
}

export type Sandbox = {
  sandboxID: string
  name: string
  category?: string
  isDefault?: boolean
  templateID: string
  clientID: string
  state: string
  metadata?: Record<string, string>
}

export type DirEntry = {
  name: string
  is_dir: boolean
  size: number
  mod_time?: string
}

export type SetupStep = {
  title: string
  detail?: string
  command?: string
}

export type FileList = {
  entries: DirEntry[]
  path?: string
  host_path?: string
}

export class ApiError extends Error {
  status: number
  code?: string
  engine?: string
  setup?: SetupStep[]
  constructor(status: number, message: string, extra?: Partial<ApiError>) {
    super(message)
    this.status = status
    this.code = extra?.code
    this.engine = extra?.engine
    this.setup = extra?.setup
  }
}

export function apiErrorMessage(body: unknown, fallback: string): string {
  if (body && typeof body === 'object') {
    const o = body as { error?: unknown; message?: unknown }
    if (typeof o.error === 'string' && o.error.trim()) return o.error
    if (typeof o.message === 'string' && o.message.trim()) return o.message
  }
  return fallback
}

export async function parseError(res: Response): Promise<ApiError> {
  let message = res.statusText
  let extra: Partial<ApiError> = {}
  try {
    const body = await res.json()
    message = apiErrorMessage(body, message)
    if (body && typeof body === 'object') {
      const o = body as { code?: unknown; engine?: unknown; setup?: unknown }
      extra = {
        code: typeof o.code === 'string' ? o.code : undefined,
        engine: typeof o.engine === 'string' ? o.engine : undefined,
        setup: Array.isArray(o.setup) ? (o.setup as SetupStep[]) : undefined,
      }
    }
  } catch {
    /* ignore */
  }
  return new ApiError(res.status, message, extra)
}

export async function api<T>(
  path: string,
  init: RequestInit = {},
): Promise<T> {
  const headers = new Headers(init.headers)
  if (init.body && !(init.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json')
  }
  const res = await fetch(path, {
    ...init,
    headers,
    credentials: 'include',
  })
  if (!res.ok) throw await parseError(res)
  if (res.status === 204) return undefined as T
  const ct = res.headers.get('Content-Type') ?? ''
  if (ct.includes('application/json')) return (await res.json()) as T
  return (await res.text()) as T
}
