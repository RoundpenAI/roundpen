/** Pure TOFU decision for InstallDO (testable without Durable Object storage). */

export type TofuResult =
  | { kind: 'register' }
  | { kind: 'match'; reinstallCount: number }
  | { kind: 'rotate'; reinstallCount: number }

export function decideTofu(
  existingPubHex: string | undefined,
  publicKeyHex: string,
  reinstallCount: number,
): TofuResult {
  const pub = publicKeyHex.trim().toLowerCase()
  const existing = (existingPubHex || '').trim().toLowerCase()
  const count = Math.max(0, Math.floor(reinstallCount) || 0)
  if (!existing) return { kind: 'register' }
  if (existing === pub) return { kind: 'match', reinstallCount: count }
  return { kind: 'rotate', reinstallCount: count + 1 }
}
