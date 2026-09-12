export type AssistantKind = 'user' | 'system'

export type AssistantLike = {
  id: string
  kind?: AssistantKind | string
}

/** Prefer the system assistant for /a index redirect. */
export function pickHomeAssistant<T extends AssistantLike>(
  assistants: T[],
): T | undefined {
  if (!assistants.length) return undefined
  return assistants.find((a) => a.kind === 'system') ?? assistants[0]
}

export function isSystemAssistant(a: { kind?: string } | null | undefined): boolean {
  return a?.kind === 'system'
}
