import { Button, Typography } from '@douyinfe/semi-ui-19'
import { useT } from '../i18n'

/** Suggested prompts for the always-open-PR delivery path (GitHub). */
export const DELIVERY_PROMPTS = {
  openPR:
    'Commit any pending changes with a conventional message, push the feature branch (never the default branch, never force-push), and create a GitHub pull request with CreatePullRequest. Always open a PR — do not push directly to the default branch. SoftDeny confirmations are expected for push/PR.',
  mergePR:
    'Merge the open GitHub pull request with MergePullRequest using squash. Do not force-push. Wait for SoftDeny confirmation.',
} as const

type Props = {
  disabled?: boolean
  onSend: (text: string) => void
}

/** Compact action bar: always create PR (GitHub). Merge is secondary. */
export function DeliveryActionBar({ disabled, onSend }: Props) {
  const t = useT()
  return (
    <div className="rp-delivery-bar" role="group" aria-label={t('chat.delivery.label')}>
      <Typography.Text type="tertiary" size="small" className="rp-delivery-hint">
        {t('chat.delivery.hint')}
      </Typography.Text>
      <div className="rp-delivery-actions">
        <Button
          size="small"
          theme="solid"
          type="primary"
          disabled={disabled}
          onClick={() => onSend(DELIVERY_PROMPTS.openPR)}
        >
          {t('chat.delivery.openPR')}
        </Button>
        <Button
          size="small"
          theme="borderless"
          type="tertiary"
          disabled={disabled}
          onClick={() => onSend(DELIVERY_PROMPTS.mergePR)}
        >
          {t('chat.delivery.mergePR')}
        </Button>
      </div>
    </div>
  )
}
