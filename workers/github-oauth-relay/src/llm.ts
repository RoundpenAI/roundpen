/** FREE LLM relay: rp1 install auth → rate limit → OpenAI-compatible upstream (SSE passthrough). */

import type { Env } from './types'

const LLM_PREFIX = '/llm'

export function isLLMPath(pathname: string): boolean {
  return pathname === LLM_PREFIX || pathname.startsWith(LLM_PREFIX + '/')
}

/** Strip /llm so /llm/v1/chat/completions → /v1/chat/completions for the upstream. */
export function llmUpstreamPath(pathname: string): string {
  const rest = pathname.slice(LLM_PREFIX.length)
  if (!rest || rest === '/') return '/v1/models'
  return rest.startsWith('/') ? rest : '/' + rest
}

export function mapFreeModel(model: string, mapped: string): string {
  const m = model.trim()
  if (!m || m === 'roundpen-free') return mapped
  return m
}

/** Rewrite JSON body.model when present; leave non-JSON / empty bodies alone. */
export async function rewriteModelBody(req: Request, mappedModel: string): Promise<Uint8Array | null> {
  if (req.method === 'GET' || req.method === 'HEAD') return null
  const raw = new Uint8Array(await req.arrayBuffer())
  if (raw.byteLength === 0) return raw
  const ct = (req.headers.get('content-type') || '').toLowerCase()
  if (!ct.includes('application/json')) return raw
  try {
    const body = JSON.parse(new TextDecoder().decode(raw)) as Record<string, unknown>
    if (body && typeof body === 'object' && 'model' in body) {
      body.model = mapFreeModel(String(body.model || ''), mappedModel)
      return new TextEncoder().encode(JSON.stringify(body))
    }
  } catch {
    /* pass through */
  }
  return raw
}

export function buildUpstreamURL(base: string, path: string, search: string): string {
  const root = base.replace(/\/+$/, '')
  let p = path.startsWith('/') ? path : '/' + path
  // Upstream bases that already end in /v1 must not get a second /v1.
  if (/\/v\d+$/.test(root) && (p === '/v1' || p.startsWith('/v1/'))) {
    p = p.slice('/v1'.length) || '/'
  }
  return root + p + (search || '')
}

export async function forwardLLM(
  req: Request,
  env: Env,
  opts: {
    installId: string
    upstreamBase: string
    apiKey: string
    mappedModel: string
  },
): Promise<Response> {
  const url = new URL(req.url)
  const path = llmUpstreamPath(url.pathname)
  const target = buildUpstreamURL(opts.upstreamBase, path, url.search)

  const body = await rewriteModelBody(req, opts.mappedModel)
  const headers = new Headers()
  headers.set('Authorization', `Bearer ${opts.apiKey}`)
  headers.set('Content-Type', req.headers.get('Content-Type') || 'application/json')
  const accept = req.headers.get('Accept')
  if (accept) headers.set('Accept', accept)
  // OpenRouter optional attribution
  headers.set('HTTP-Referer', env.PUBLIC_BASE || 'https://auth.eaxi.com')
  headers.set('X-Title', 'Roundpen FREE')

  const init: RequestInit = {
    method: req.method,
    headers,
    body: body && req.method !== 'GET' && req.method !== 'HEAD' ? body : undefined,
  }
  const upstream = await fetch(target, init)
  // Stream through; do not buffer. Drop hop-by-hop headers.
  const out = new Headers(upstream.headers)
  out.delete('content-encoding')
  out.delete('transfer-encoding')
  out.delete('connection')
  out.set('cache-control', 'no-store')
  out.set('x-roundpen-install', opts.installId)
  return new Response(upstream.body, { status: upstream.status, headers: out })
}
