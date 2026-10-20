import { Button, Typography } from '@douyinfe/semi-ui-19'
import { useT } from '../i18n'
import type { DeliveryNeed } from '../lib/deliveryNeed'
import { hasDeliveryNeed } from '../lib/deliveryNeed'

/** Suggested prompts — host-neutral (GitHub / GitLab / Gitea via CreatePullRequest). */
export const DELIVERY_PROMPTS = {
  openPR:
    'Commit any pending changes with a conventional message, push the feature branch (never the default branch, never force-push), and open a pull request with CreatePullRequest. Always open a PR — do not push directly to the default branch. SoftDeny confirmations are expected for push/PR.',
  mergePR:
    'Merge the open pull request with MergePullRequest using squash. Do not force-push. Wait for SoftDeny confirmation.',
} as const

type Props = {
  disabled?: boolean
  need: DeliveryNeed
  onSend: (text: string) => void
}

/** Compact action bar: only the delivery steps the session currently needs. */
export function DeliveryActionBar({ disabled, need, onSend }: Props) {
  const t = useT()
  if (!hasDeliveryNeed(need)) return null

  return (
    <div className="rp-delivery-bar" role="group" aria-label={t('chat.delivery.label')}>
      <Typography.Text type="tertiary" size="small" className="rp-delivery-hint">
        {need.openPR ? t('chat.delivery.hintOpen') : t('chat.delivery.hintMerge')}
      </Typography.Text>
      <div className="rp-delivery-actions">
        {need.openPR && (
          <Button
            size="small"
            theme="solid"
            type="primary"
            disabled={disabled}
            onClick={() => onSend(DELIVERY_PROMPTS.openPR)}
          >
            {t('chat.delivery.openPR')}
          </Button>
        )}
        {need.mergePR && (
          <Button
            size="small"
            theme={need.openPR ? 'borderless' : 'solid'}
            type={need.openPR ? 'tertiary' : 'primary'}
            disabled={disabled}
            onClick={() => onSend(DELIVERY_PROMPTS.mergePR)}
          >
            {t('chat.delivery.mergePR')}
          </Button>
        )}
      </div>
    </div>
  )
}
