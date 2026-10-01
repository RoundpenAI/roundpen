import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import { Banner, Button, Typography } from '@douyinfe/semi-ui-19'
import {
  ApiError,
  routines,
  type Routine,
  type RoutineRun,
  type RoutineRunStatus,
} from '../api'
import { Loading } from '../components/Loading'
import { useT, type MessageKey } from '../i18n'

function runStatusKey(status: RoutineRunStatus): MessageKey {
  return `routines.runStatus.${status}`
}

function formatWhen(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

const openRun = new Set<RoutineRunStatus>(['queued', 'running', 'waiting_user'])

export function RoutineDetailPage() {
  const t = useT()
  const { assistantId = '', key = '' } = useParams()
  const [routine, setRoutine] = useState<Routine | null>(null)
  const [runs, setRuns] = useState<RoutineRun[]>([])
  const [error, setError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    try {
      const res = await routines.get(key)
      setRoutine(res.routine)
      setRuns(res.runs ?? [])
      setError(null)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t('routines.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [key, t])

  useEffect(() => {
    void load()
  }, [load])

  const cancel = async (run: RoutineRun) => {
    try {
      await routines.cancelRun(key, run.key)
      await load()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t('routines.saveFailed'))
    }
  }

  if (loading) return <Loading />
  if (!routine) {
    return (
      <div role="alert">
        <Banner
          fullMode={false}
          type="danger"
          description={error || t('routines.loadFailed')}
          closeIcon={null}
        />
      </div>
    )
  }

  const stateText =
    routine.state == null
      ? '{}'
      : typeof routine.state === 'string'
        ? routine.state
        : JSON.stringify(routine.state, null, 2)

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 16, maxWidth: 720 }}>
      <Link to={`/a/${assistantId}`}>{t('routines.detail.back')}</Link>
      <div>
        <Typography.Text type="tertiary" size="small">
          {routine.key}
        </Typography.Text>
        <Typography.Title heading={4} style={{ margin: '4px 0' }}>
          {routine.title}
        </Typography.Title>
        <Typography.Paragraph>{routine.brief}</Typography.Paragraph>
      </div>
      {error && (
        <div role="alert">
          <Banner fullMode={false} type="danger" description={error} closeIcon={null} />
        </div>
      )}
      <Typography.Text size="small">
        {t('routines.detail.cron')} {routine.cron} ({routine.timezone})
        {' · '}
        {routine.autonomy === 'browse'
          ? t('routines.autonomy.browse')
          : t('routines.autonomy.read')}
        {' · '}
        {routine.status === 'paused'
          ? t('routines.status.paused')
          : routine.status === 'archived'
            ? t('routines.status.archived')
            : t('routines.status.active')}
      </Typography.Text>
      <Typography.Text size="small" type="tertiary">
        {t('routines.nextRun')} {formatWhen(routine.nextRunAt)}
      </Typography.Text>
      <div>
        <Typography.Title heading={6} style={{ margin: '0 0 4px' }}>
          {t('routines.detail.state')}
        </Typography.Title>
        <pre style={{ margin: 0, whiteSpace: 'pre-wrap' }}>{stateText}</pre>
      </div>
      <div>
        <Typography.Title heading={6} style={{ margin: '0 0 8px' }}>
          {t('routines.detail.runs')}
        </Typography.Title>
        {runs.length === 0 && (
          <Typography.Text type="tertiary" size="small">
            {t('routines.detail.emptyRuns')}
          </Typography.Text>
        )}
        {runs.map((run) => (
          <div key={run.id} style={{ display: 'flex', flexDirection: 'column', gap: 4, marginBottom: 12 }}>
            <Typography.Text size="small">
              {run.key} · {formatWhen(run.scheduledAt)} · {t(runStatusKey(run.status))}
            </Typography.Text>
            {run.summary && <Typography.Text size="small">{run.summary}</Typography.Text>}
            <div style={{ display: 'flex', gap: 8 }}>
              {run.sessionId && (run.status === 'running' || run.status === 'waiting_user') && (
                <Link to={`/a/${assistantId}/s/${run.sessionId}`}>
                  {t('routines.detail.openSession')}
                </Link>
              )}
              {run.status === 'waiting_user' && run.assistTicketId && (
                <Link to={`/a/${assistantId}#assist-tickets`}>
                  {t('routines.detail.openTicket')}
                </Link>
              )}
              {openRun.has(run.status) && (
                <Button size="small" onClick={() => void cancel(run)}>
                  {t('routines.detail.cancelRun')}
                </Button>
              )}
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}
