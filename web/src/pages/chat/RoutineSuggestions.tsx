import { useEffect, useState } from 'react'
import { Button, Typography } from '@douyinfe/semi-ui-19'
import { assistantsApi } from '../../api'
import { useT, type MessageKey } from '../../i18n'
import {
  suggestContextFor,
  type SuggestAssistant,
} from '../../lib/sessionPromptSuggestions'

type SuggestSlot = {
  empty: MessageKey
  withBio: MessageKey
}

const SUGGESTIONS: SuggestSlot[] = [
  { empty: 'chat.suggest.intro', withBio: 'chat.suggest.introBio' },
  {
    empty: 'chat.suggest.prioritize',
    withBio: 'chat.suggest.prioritizeBio',
  },
  { empty: 'chat.suggest.digest', withBio: 'chat.suggest.digestBio' },
]

type AssistantWithID = SuggestAssistant & { id?: string }

/** Example prompts above an empty composer, tailored to the current assistant.
 * Prefers server/LLM prompts; falls back to i18n templates. */
export function RoutineSuggestions({
  assistant,
  sessionId,
  onPick,
}: {
  assistant?: AssistantWithID | null
  sessionId?: string
  onPick: (text: string) => void
}) {
  const t = useT()
  const ctx = suggestContextFor(assistant, t('chat.suggest.defaultName'))
  const vars = { name: ctx.name, bio: ctx.bio }
  const hintKey: MessageKey = ctx.hasBio
    ? 'chat.suggest.hintBio'
    : 'chat.suggest.hint'

  const [remote, setRemote] = useState<string[] | null>(null)

  useEffect(() => {
    const id = assistant?.id?.trim()
    if (!id) {
      setRemote(null)
      return
    }
    let cancelled = false
    void assistantsApi
      .suggestedPrompts(id, sessionId)
      .then((res) => {
        if (cancelled) return
        const prompts = (res.prompts ?? []).map((p) => p.trim()).filter(Boolean)
        setRemote(prompts.length > 0 ? prompts : [])
      })
      .catch(() => {
        if (!cancelled) setRemote([])
      })
    return () => {
      cancelled = true
    }
  }, [assistant?.id, sessionId])

  const i18nPrompts = SUGGESTIONS.map((slot) => {
    const key = ctx.hasBio ? slot.withBio : slot.empty
    return { key, text: t(key, vars) }
  })
  const prompts =
    remote && remote.length > 0
      ? remote.map((text, i) => ({ key: `remote-${i}`, text }))
      : i18nPrompts

  return (
    <div
      style={{
        display: 'flex',
        flexDirection: 'column',
        gap: 8,
        marginBottom: 10,
      }}
    >
      <Typography.Text type="tertiary" size="small">
        {t(hintKey, vars)}
      </Typography.Text>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        {prompts.map((p) => (
          <Button
            key={p.key}
            theme="light"
            type="tertiary"
            onClick={() => onPick(p.text)}
            style={{
              height: 'auto',
              justifyContent: 'flex-start',
              textAlign: 'left',
              whiteSpace: 'normal',
              padding: '8px 12px',
            }}
          >
            {p.text}
          </Button>
        ))}
      </div>
    </div>
  )
}
