import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  Banner,
  Button,
  Input,
  Modal,
  Radio,
  RadioGroup,
  Select,
  Switch,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui-19'
import {
  ApiError,
  assistantsApi,
  routines,
  type Assistant,
  type Routine,
  type RoutineAutonomy,
  type RoutineStatus,
} from '../api'
import { useT, type MessageKey } from '../i18n'

const WEEKLY = '0 8 * * 1'
const MONTHLY = '0 9 1 * *'

type Preset = 'weekly' | 'monthly' | 'custom'

function presetFor(cron: string): Preset {
  if (cron === WEEKLY) return 'weekly'
  if (cron === MONTHLY) return 'monthly'
  return 'custom'
}

function splitHosts(raw: string): string[] {
  return raw
    .split(/[\n,]/)
    .map((h) => h.trim())
    .filter(Boolean)
}

function statusKey(status: RoutineStatus): MessageKey {
  return `routines.status.${status}`
}

function formatWhen(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

function browserZone(): string {
  return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'
}

export function RoutinesSection({ assistantId }: { assistantId: string }) {
  const t = useT()
  const [items, setItems] = useState<Routine[] | null>(null)
  const [assistants, setAssistants] = useState<Assistant[]>([])
  const [error, setError] = useState<string | null>(null)
  const [open, setOpen] = useState(false)
  const [editing, setEditing] = useState<Routine | null>(null)
  const [saving, setSaving] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  const [title, setTitle] = useState('')
  const [brief, setBrief] = useState('')
  const [preset, setPreset] = useState<Preset>('weekly')
  const [cron, setCron] = useState(WEEKLY)
  const [timezone, setTimezone] = useState(browserZone)
  const [autonomy, setAutonomy] = useState<RoutineAutonomy>('read')
  const [hosts, setHosts] = useState('')
  const [deliverIm, setDeliverIm] = useState(true)
  const [assignee, setAssignee] = useState(assistantId)

  const load = useCallback(async () => {
    try {
      const res = await routines.list(assistantId)
      setItems(res.routines)
      setError(null)
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t('routines.loadFailed'))
    }
  }, [assistantId, t])

  useEffect(() => {
    void load()
  }, [load])

  useEffect(() => {
    let cancelled = false
    assistantsApi
      .list()
      .then((res) => {
        if (!cancelled) setAssistants(res.assistants)
      })
      .catch(() => {})
    return () => {
      cancelled = true
    }
  }, [])

  const openCreate = () => {
    setEditing(null)
    setTitle('')
    setBrief('')
    setPreset('weekly')
    setCron(WEEKLY)
    setTimezone(browserZone())
    setAutonomy('read')
    setHosts('')
    setDeliverIm(true)
    setAssignee(assistantId)
    setFormError(null)
    setOpen(true)
  }

  const openEdit = (rt: Routine) => {
    setEditing(rt)
    setTitle(rt.title)
    setBrief(rt.brief)
    setPreset(presetFor(rt.cron))
    setCron(rt.cron)
    setTimezone(rt.timezone)
    setAutonomy(rt.autonomy)
    setHosts((rt.hosts ?? []).join('\n'))
    setDeliverIm(rt.deliverIm)
    setAssignee(rt.assigneeAssistantId || assistantId)
    setFormError(null)
    setOpen(true)
  }

  const submit = async () => {
    setSaving(true)
    setFormError(null)
    const body = {
      title: title.trim(),
      brief: brief.trim(),
      cron: preset === 'weekly' ? WEEKLY : preset === 'monthly' ? MONTHLY : cron.trim(),
      timezone: timezone.trim(),
      autonomy,
      hosts: autonomy === 'browse' ? splitHosts(hosts) : [],
      deliverIm,
      assigneeAssistantId: assignee,
      assigneeConfirmed: true,
    }
    try {
      if (editing) {
        await routines.update(editing.key, body)
      } else {
        await routines.create(body)
      }
      setOpen(false)
      await load()
    } catch (err) {
      setFormError(
        err instanceof ApiError
          ? err.message
          : t(editing ? 'routines.saveFailed' : 'routines.createFailed'),
      )
    } finally {
      setSaving(false)
    }
  }

  const setStatus = async (rt: Routine, status: RoutineStatus) => {
    try {
      await routines.update(rt.key, { status })
      await load()
    } catch (err) {
      setError(err instanceof ApiError ? err.message : t('routines.saveFailed'))
    }
  }

  const archive = (rt: Routine) => {
    Modal.confirm({
      title: t('routines.archiveConfirm'),
      okText: t('routines.archive'),
      cancelText: t('routines.cancel'),
      okType: 'danger',
      onOk: () => setStatus(rt, 'archived'),
    })
  }

  return (
    <section style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
        <Typography.Title heading={5} style={{ margin: 0 }}>
          {t('routines.section')}
        </Typography.Title>
        <Button size="small" onClick={openCreate}>
          {t('routines.new')}
        </Button>
      </div>

      {error && (
        <div role="alert">
          <Banner fullMode={false} type="danger" description={error} closeIcon={null} />
        </div>
      )}

      {items && items.length === 0 && (
        <Typography.Text type="tertiary" size="small">
          {t('routines.empty')}
        </Typography.Text>
      )}

      {items?.map((rt) => (
        <div
          key={rt.id}
          style={{
            display: 'flex',
            flexDirection: 'column',
            gap: 4,
            paddingBottom: 8,
            borderBottom: '1px solid var(--semi-color-border)',
          }}
        >
          <div style={{ display: 'flex', gap: 8, alignItems: 'baseline', flexWrap: 'wrap' }}>
            <Link to={`/a/${assistantId}/routines/${rt.key}`}>{rt.key}</Link>
            <Typography.Text strong>{rt.title}</Typography.Text>
            <Typography.Text type="tertiary" size="small">
              {t(statusKey(rt.status))}
            </Typography.Text>
          </div>
          <Typography.Text type="tertiary" size="small">
            {t('routines.nextRun')} {formatWhen(rt.nextRunAt)}
            {' · '}
            {t('routines.lastSummary')}{' '}
            {rt.lastSummary || t('routines.noneYet')}
          </Typography.Text>
          <div style={{ display: 'flex', gap: 8 }}>
            <Button size="small" onClick={() => openEdit(rt)}>
              {t('routines.edit')}
            </Button>
            {rt.status === 'active' && (
              <Button size="small" onClick={() => void setStatus(rt, 'paused')}>
                {t('routines.pause')}
              </Button>
            )}
            {rt.status === 'paused' && (
              <Button size="small" onClick={() => void setStatus(rt, 'active')}>
                {t('routines.resume')}
              </Button>
            )}
            {rt.status !== 'archived' && (
              <Button size="small" type="danger" onClick={() => archive(rt)}>
                {t('routines.archive')}
              </Button>
            )}
          </div>
        </div>
      ))}

      <Modal
        title={editing ? t('routines.edit') : t('routines.new')}
        visible={open}
        onCancel={() => {
          if (!saving) setOpen(false)
        }}
        footer={null}
        maskClosable={!saving}
        closeOnEsc={!saving}
        width={480}
      >
        <form
          onSubmit={(e) => {
            e.preventDefault()
            void submit()
          }}
          style={{ display: 'flex', flexDirection: 'column', gap: 12 }}
        >
          <div>
            <Typography.Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 4 }}>
              {t('routines.field.title')}
            </Typography.Text>
            <Input
              value={title}
              onChange={setTitle}
              aria-label={t('routines.field.title')}
              autoFocus
            />
          </div>
          <div>
            <Typography.Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 4 }}>
              {t('routines.field.brief')}
            </Typography.Text>
            <TextArea
              value={brief}
              onChange={setBrief}
              aria-label={t('routines.field.brief')}
              autosize={{ minRows: 3, maxRows: 6 }}
            />
          </div>
          <div>
            <Typography.Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 4 }}>
              {t('routines.field.preset')}
            </Typography.Text>
            <RadioGroup
              direction="vertical"
              value={preset}
              onChange={(e) => {
                const next = e.target.value as Preset
                setPreset(next)
                if (next === 'weekly') setCron(WEEKLY)
                if (next === 'monthly') setCron(MONTHLY)
              }}
            >
              <Radio value="weekly">{t('routines.preset.weekly')}</Radio>
              <Radio value="monthly">{t('routines.preset.monthly')}</Radio>
              <Radio value="custom">{t('routines.preset.custom')}</Radio>
            </RadioGroup>
          </div>
          {preset === 'custom' && (
            <Input
              value={cron}
              onChange={setCron}
              aria-label={t('routines.field.cron')}
              placeholder="0 8 * * 1"
            />
          )}
          <div>
            <Typography.Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 4 }}>
              {t('routines.field.timezone')}
            </Typography.Text>
            <Input
              value={timezone}
              onChange={setTimezone}
              aria-label={t('routines.field.timezone')}
            />
          </div>
          <div>
            <Typography.Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 4 }}>
              {t('routines.field.autonomy')}
            </Typography.Text>
            <Select
              value={autonomy}
              aria-label={t('routines.field.autonomy')}
              onChange={(v) => setAutonomy(String(v) as RoutineAutonomy)}
              optionList={[
                { value: 'read', label: t('routines.autonomy.read') },
                { value: 'browse', label: t('routines.autonomy.browse') },
              ]}
              style={{ width: '100%' }}
            />
          </div>
          {autonomy === 'browse' && (
            <div>
              <Typography.Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 4 }}>
                {t('routines.field.hosts')}
              </Typography.Text>
              <TextArea
                value={hosts}
                onChange={setHosts}
                aria-label={t('routines.field.hosts')}
                placeholder={t('routines.hostsHint')}
                autosize={{ minRows: 2, maxRows: 4 }}
              />
            </div>
          )}
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <Switch
              checked={deliverIm}
              onChange={setDeliverIm}
              aria-label={t('routines.field.deliverIm')}
            />
            <Typography.Text size="small">{t('routines.field.deliverIm')}</Typography.Text>
          </div>
          <div>
            <Typography.Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 4 }}>
              {t('routines.field.assignee')}
            </Typography.Text>
            <Select
              value={assignee}
              aria-label={t('routines.field.assignee')}
              onChange={(v) => setAssignee(String(v))}
              optionList={assistants.map((a) => ({ value: a.id, label: a.name }))}
              style={{ width: '100%' }}
            />
          </div>
          {formError && (
            <div role="alert">
              <Banner fullMode={false} type="danger" description={formError} closeIcon={null} />
            </div>
          )}
          <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
            <Button type="tertiary" onClick={() => setOpen(false)} disabled={saving}>
              {t('routines.cancel')}
            </Button>
            <Button
              htmlType="submit"
              theme="solid"
              type="primary"
              loading={saving}
              disabled={!title.trim()}
            >
              {editing ? t('routines.save') : t('routines.create')}
            </Button>
          </div>
        </form>
      </Modal>
    </section>
  )
}
