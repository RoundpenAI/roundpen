export interface Env {
  TICKETS: DurableObjectNamespace
  INSTALLS: DurableObjectNamespace
  PUBLIC_BASE: string
  GITHUB_SCOPES: string
  GOOGLE_SCOPES: string
  TICKET_TTL_SECONDS: string
  GITHUB_CLIENT_ID: string
  GITHUB_CLIENT_SECRET: string
  GOOGLE_CLIENT_ID: string
  GOOGLE_CLIENT_SECRET: string
  STATE_SECRET: string
  /** OpenAI-compatible FREE upstream (e.g. https://openrouter.ai/api/v1). */
  LLM_UPSTREAM_BASE?: string
  /** Secret upstream API key for FREE relay. */
  LLM_UPSTREAM_API_KEY?: string
  /** Upstream model id when NAS sends roundpen-free. */
  LLM_UPSTREAM_MODEL?: string
  /** Max FREE chat requests per install_id per hour. */
  LLM_REQUESTS_PER_HOUR?: string
  /** Max new TOFU registrations per client IP per UTC day. */
  LLM_REGISTER_PER_IP_PER_DAY?: string
}

export type StartBody = {
  /** Console origin only, e.g. http://192.168.1.8:9527 */
  return: string
  /** Opaque CSRF value the NAS minted; echoed on the final redirect and required to redeem the ticket. */
  state: string
  /** Optional scope subset. Each token must be on the provider allowlist. Empty uses the provider default. */
  scopes?: string
}

export type Ticket = {
  installId: string
  /** NAS CSRF state this ticket was minted for. Redeem fails unless the caller presents it. */
  nasState: string
  provider: string
  subject: string
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
  /** Provider id. Missing values are GitHub states sealed before the shared relay. */
  pv?: string
  /** NAS callback path. Missing values use the GitHub cloud callback. */
  cb?: string
}

export type Profile = {
  subject: string
  login: string
  name: string
  email: string
}

export type TokenResult = {
  access_token: string
  token_type?: string
  scope?: string
  refresh_token?: string
  expires_in?: number
}
