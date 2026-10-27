/** Context used to tailor empty-session starter prompts. */
export type SuggestAssistant = {
  name?: string
  bio?: string
}

export type SuggestContext = {
  name: string
  /** Truncated bio for chip text; empty when the assistant has none. */
  bio: string
  hasBio: boolean
}

const BIO_CHIP_MAX = 36

/** Collapse whitespace and trim; empty string when missing. */
export function cleanSuggestText(value: string | null | undefined): string {
  return (value ?? '').replace(/\s+/g, ' ').trim()
}

/** Shorten for chip labels without cutting mid-word when possible. */
export function truncateSuggestBio(bio: string, max = BIO_CHIP_MAX): string {
  const cleaned = cleanSuggestText(bio)
  if (cleaned.length <= max) return cleaned
  const slice = cleaned.slice(0, max - 1)
  const sp = slice.lastIndexOf(' ')
  const cut = sp > max * 0.5 ? slice.slice(0, sp) : slice
  return `${cut}…`
}

/**
 * Normalize assistant name/bio for starter chips.
 * Falls back to `fallbackName` when name is blank; bio may stay empty.
 */
export function suggestContextFor(
  assistant: SuggestAssistant | null | undefined,
  fallbackName: string,
): SuggestContext {
  const rawName = cleanSuggestText(assistant?.name)
  const name = rawName || cleanSuggestText(fallbackName) || 'Assistant'
  const bio = truncateSuggestBio(assistant?.bio ?? '')
  return { name, bio, hasBio: bio.length > 0 }
}
