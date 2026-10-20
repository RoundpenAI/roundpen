import { useCallback, useEffect, useState } from 'react'
import { Banner, Button, Form, Input, TextArea, Toast } from '@douyinfe/semi-ui-19'
import { auth } from '../api'
import { refreshAuth, useAuth } from '../auth'
import { useT } from '../i18n'
import { Field } from '../pages/settings/parts'
import { Loading } from './Loading'

const sectionGap = { display: 'flex', flexDirection: 'column' as const, gap: 16 }

/** Common timezone shortcuts; free-text IANA names are also accepted. */
const TZ_OPTIONS = [
  'Asia/Shanghai',
  'Asia/Hong_Kong',
  'Asia/Tokyo',
  'Asia/Singapore',
  'America/Los_Angeles',
  'America/New_York',
  'Europe/London',
  'Europe/Berlin',
  'UTC',
]

export function ProfilePanel() {
  const t = useT()
  const authCtx = useAuth()
  const [fullname, setFullname] = useState('')
  const [orgName, setOrgName] = useState('')
  const [about, setAbout] = useState('')
  const [timezone, setTimezone] = useState('')
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [dirty, setDirty] = useState(false)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const user = await auth.me()
      setFullname(user.fullname ?? '')
      setOrgName(user.orgName ?? '')
      setAbout(user.about ?? '')
      setTimezone(user.timezone ?? '')
      setDirty(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('profile.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  async function save() {
    setSaving(true)
    setError(null)
    try {
      const user = await auth.updateProfile({ fullname, orgName, about, timezone })
      setFullname(user.fullname ?? '')
      setOrgName(user.orgName ?? '')
      setAbout(user.about ?? '')
      setTimezone(user.timezone ?? '')
      setDirty(false)
      await refreshAuth()
      Toast.success(t('profile.saved'))
    } catch (e) {
      setError(e instanceof Error ? e.message : t('profile.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  if (loading) return <Loading tip={t('profile.loading')} />

  const username = authCtx.status === 'ok' ? authCtx.user.username : ''

  return (
    <Form labelPosition="top" labelAlign="left" style={sectionGap}>
      <div style={{ ...sectionGap, paddingTop: 8 }}>
        <p style={{ margin: 0, color: 'var(--semi-color-text-2)', fontSize: 13 }}>
          {t('profile.intro')}
        </p>
        {error && (
          <Banner fullMode={false} type="danger" closeIcon={null} description={error} />
        )}
        <Field label={t('profile.username')}>
          <Input value={username} disabled />
        </Field>
        <Field label={t('profile.fullname')} hint={t('profile.fullnameHint')}>
          <Input
            value={fullname}
            maxLength={80}
            onChange={(v) => {
              setFullname(v)
              setDirty(true)
            }}
            placeholder={t('profile.fullnamePlaceholder')}
          />
        </Field>
        <Field label={t('profile.orgName')}>
          <Input
            value={orgName}
            maxLength={80}
            onChange={(v) => {
              setOrgName(v)
              setDirty(true)
            }}
            placeholder={t('profile.orgPlaceholder')}
          />
        </Field>
        <Field label={t('profile.timezone')} hint={t('profile.timezoneHint')}>
          <Input
            value={timezone}
            list="roundpen-tz-options"
            onChange={(v) => {
              setTimezone(v)
              setDirty(true)
            }}
            placeholder="Asia/Shanghai"
          />
          <datalist id="roundpen-tz-options">
            {TZ_OPTIONS.map((tz) => (
              <option key={tz} value={tz} />
            ))}
          </datalist>
        </Field>
        <Field label={t('profile.about')} hint={t('profile.aboutHint')}>
          <TextArea
            value={about}
            maxCount={2000}
            autosize={{ minRows: 4, maxRows: 10 }}
            onChange={(v) => {
              setAbout(v)
              setDirty(true)
            }}
            placeholder={t('profile.aboutPlaceholder')}
          />
        </Field>
        <div>
          <Button
            theme="solid"
            type="primary"
            loading={saving}
            disabled={!dirty}
            onClick={() => void save()}
          >
            {t('profile.save')}
          </Button>
        </div>
      </div>
    </Form>
  )
}
