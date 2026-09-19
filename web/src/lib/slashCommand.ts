/** Composer content nodes as Semi's AIChatInput reports them on send. */
export type SendContent = {
  type?: string
  text?: string
  value?: string
  label?: string
}

/** Catalog entry shape the composer needs to resolve a chip into a command. */
export type SlashCommandOption = {
  name: string
  label?: string
  description?: string
  kind?: string
  source?: string
  args?: boolean
}

export type SendPayload =
  | { kind: 'command'; name: string; args: string }
  | { kind: 'text'; text: string }

// Chips render back to "/name" so a fallback to plain text never drops them.
function renderContent(parts: SendContent[]): string {
  return parts
    .map((c) => {
      if (typeof c.text === 'string') return c.text
      const name = chipName(c)
      return name ? `/${name}` : ''
    })
    .join('')
}

function chipName(c: SendContent | undefined): string {
  if (!c || c.type !== 'skillSlot') return ''
  const raw = c.value ?? c.label ?? ''
  return typeof raw === 'string' ? raw.trim().toLowerCase() : ''
}

export function messageContentToPlainText(parts: SendContent[] | undefined): string {
  return renderContent(parts ?? []).trim()
}

export function contentsHaveSendableText(
  contents: Array<Record<string, unknown>> | undefined,
): boolean {
  if (!contents?.length) return false
  return contents.some((c) => {
    if (typeof c.text === 'string' && c.text.trim().length > 0) return true
    return (
      c.type === 'skillSlot' &&
      typeof c.value === 'string' &&
      c.value.trim().length > 0
    )
  })
}

/**
 * Decide what the composer should send. A skill chip resolving against the
 * catalog becomes a command frame; everything else stays plain text. Text
 * typed before the chip disqualifies the command (the user is not invoking it
 * at the start of the message).
 */
export function buildSendPayload(
  contents: SendContent[] | undefined,
  commands: SlashCommandOption[],
): SendPayload | null {
  const parts = contents ?? []
  const chipIdx = parts.findIndex((c) => chipName(c) !== '')

  if (chipIdx < 0) {
    const text = messageContentToPlainText(parts)
    return text ? { kind: 'text', text } : null
  }

  const before = renderContent(parts.slice(0, chipIdx)).trim()
  if (before) {
    const text = messageContentToPlainText(parts)
    return text ? { kind: 'text', text } : null
  }

  const name = chipName(parts[chipIdx])
  const args = renderContent(parts.slice(chipIdx + 1)).trim()
  if (!commands.some((c) => c.name.toLowerCase() === name)) {
    // The catalog moved on (skill removed); keep the literal text so nothing
    // the user typed is silently dropped.
    const text = (`/${name}` + (args ? ` ${args}` : '')).trim()
    return text ? { kind: 'text', text } : null
  }
  return { kind: 'command', name, args }
}
