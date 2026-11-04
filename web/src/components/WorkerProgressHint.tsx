import { Tag, Typography } from '@douyinfe/semi-ui-19'
import type { ProjectWorker } from '../api'
import { useT } from '../i18n'
import {
  formatElapsed,
  workerAgeAnchor,
  workerLooksStuck,
} from '../lib/workerActivity'

type Props = {
  worker: ProjectWorker
  /** Compact single-line for board tags / dense lists. */
  compact?: boolean
}

/** Age + message activity + stuck hint for a project worker. */
export function WorkerProgressHint({ worker: w, compact }: Props) {
  const t = useT()
  const stuck = workerLooksStuck(w)
  const age = formatElapsed(workerAgeAnchor(w))
  const msgs = w.messageCount ?? 0
  const lastAge = formatElapsed(w.lastMessageAt)

  const stuckLabel = stuck
    ? msgs === 0
      ? t('projects.worker.progress.stuckEmpty')
      : t('projects.worker.progress.stuckQuiet', {
          age: lastAge || age || '—',
        })
    : null

  if (compact) {
    const bits: string[] = []
    if (w.status === 'running' && age) {
      bits.push(age)
    } else if (w.status === 'queued' && age) {
      bits.push(age)
    }
    if (w.status === 'running' || w.status === 'succeeded' || w.status === 'failed') {
      bits.push(
        msgs > 0
          ? t('projects.worker.progress.msgs', { count: msgs })
          : t('projects.worker.progress.noMsgs'),
      )
    }
    return (
      <span>
        {bits.join(' · ')}
        {stuckLabel ? (
          <Tag size="small" color="red" style={{ marginLeft: 4 }}>
            {stuckLabel}
          </Tag>
        ) : null}
      </span>
    )
  }

  const lines: string[] = []
  if (w.status === 'queued' && age) {
    lines.push(t('projects.worker.progress.queued', { age }))
  } else if (w.status === 'running' && age) {
    lines.push(t('projects.worker.progress.running', { age }))
  }
  if (w.sessionId) {
    lines.push(
      msgs > 0
        ? t('projects.worker.progress.msgs', { count: msgs })
        : t('projects.worker.progress.noMsgs'),
    )
    if (msgs > 0 && lastAge) {
      lines.push(t('projects.worker.progress.lastMsg', { age: lastAge }))
    }
  }

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 2 }}>
      {lines.map((line) => (
        <Typography.Text key={line} type="tertiary" size="small">
          {line}
        </Typography.Text>
      ))}
      {stuckLabel ? (
        <Tag size="small" color="red" style={{ width: 'fit-content' }}>
          {stuckLabel}
        </Tag>
      ) : null}
    </div>
  )
}
