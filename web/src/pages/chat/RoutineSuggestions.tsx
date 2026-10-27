import { Button, Typography } from '@douyinfe/semi-ui-19'
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

/** Example prompts above an empty composer, tailored to the current assistant.
 * Picking one fills the input; it does not send until the user confirms. */
export function RoutineSuggestions({
  assistant,
  onPick,
}: {
  assistant?: SuggestAssistant | null
  onPick: (text: string) => void
}) {
  const t = useT()
  const ctx = suggestContextFor(assistant, t('chat.suggest.defaultName'))
  const vars = { name: ctx.name, bio: ctx.bio }
  const hintKey: MessageKey = ctx.hasBio
    ? 'chat.suggest.hintBio'
    : 'chat.suggest.hint'

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
        {SUGGESTIONS.map((slot) => {
          const key = ctx.hasBio ? slot.withBio : slot.empty
          const text = t(key, vars)
          return (
            <Button
              key={key}
              theme="light"
              type="tertiary"
              onClick={() => onPick(text)}
              style={{
                height: 'auto',
                justifyContent: 'flex-start',
                textAlign: 'left',
                whiteSpace: 'normal',
                padding: '8px 12px',
              }}
            >
              {text}
            </Button>
          )
        })}
      </div>
    </div>
  )
}
