export interface Env {
  TICKETS: DurableObjectNamespace
  INSTALLS: DurableObjectNamespace
  PUBLIC_BASE: string
  GITHUB_SCOPES: string
  TICKET_TTL_SECONDS: string
  GITHUB_CLIENT_ID: string
  GITHUB_CLIENT_SECRET: string
  STATE_SECRET: string
}

export type StartBody = {
  /** Console origin only, e.g. http://192.168.1.8:9527 */
  return: string
  /** Opaque CSRF value the NAS minted; echoed on the final redirect. */
  state: string
}

export type Ticket = {
  installId: string
  login: string
  name: string
  email: string
  accessToken: string
  tokenType: string
  scope: string
  refreshToken?: string
  expiresIn?: number
}

export type OAuthState = {
  v: 1
  inst: string
  ret: string
  st: string
  exp: number
  cv: string
}
