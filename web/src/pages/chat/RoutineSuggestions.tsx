import { Button, Typography } from '@douyinfe/semi-ui-19'
import { useT, type MessageKey } from '../../i18n'

const SUGGESTIONS: MessageKey[] = [
  'chat.suggest.papers',
  'chat.suggest.invest',
  'chat.suggest.tax',
]

/** Example standing tasks shown above an empty composer. Picking one fills the
 * input; it does not create the routine until the user sends it. */
export function RoutineSuggestions({
  onPick,
}: {
  onPick: (text: string) => void
}) {
  const t = useT()
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
        {t('chat.suggest.hint')}
      </Typography.Text>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
        {SUGGESTIONS.map((key) => (
          <Button
            key={key}
            theme="light"
            type="tertiary"
            onClick={() => onPick(t(key))}
            style={{
              height: 'auto',
              justifyContent: 'flex-start',
              textAlign: 'left',
              whiteSpace: 'normal',
              padding: '8px 12px',
            }}
          >
            {t(key)}
          </Button>
        ))}
      </div>
    </div>
  )
}
