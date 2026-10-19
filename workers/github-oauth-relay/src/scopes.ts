export function selectScopes(
  provider: { defaultScopes: string; allowedScopes: string[] },
  requested: string,
): string {
  const raw = requested.trim()
  if (!raw) return provider.defaultScopes
  const allow = new Set(provider.allowedScopes)
  const parts = raw.split(/\s+/).filter(Boolean)
  for (const part of parts) {
    if (!allow.has(part)) {
      throw new Error(`scope not allowed: ${part}`)
    }
  }
  return parts.join(' ')
}
