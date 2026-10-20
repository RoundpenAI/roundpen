import { useCallback, useEffect, useState } from 'react'
import {
  Banner,
  Button,
  Checkbox,
  Form,
  Input,
  TextArea,
  Toast,
  Typography,
} from '@douyinfe/semi-ui-19'
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

const GPG_MASK = '●●●●●●●●'

export function ProfilePanel() {
  const t = useT()
  const authCtx = useAuth()
  const [fullname, setFullname] = useState('')
  const [orgName, setOrgName] = useState('')
  const [about, setAbout] = useState('')
  const [timezone, setTimezone] = useState('')
  const [gitName, setGitName] = useState('')
  const [gitEmail, setGitEmail] = useState('')
  const [gpgPrivateKey, setGpgPrivateKey] = useState('')
  const [gpgKeyId, setGpgKeyId] = useState('')
  const [gpgSign, setGpgSign] = useState(false)
  const [hasGpgKey, setHasGpgKey] = useState(false)
  const [clearGpgKey, setClearGpgKey] = useState(false)
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
      setGitName(user.gitName ?? '')
      setGitEmail(user.gitEmail ?? '')
      setGpgKeyId(user.gpgKeyId ?? '')
      setGpgSign(Boolean(user.gpgSign))
      setHasGpgKey(Boolean(user.hasGpgKey))
      setGpgPrivateKey(user.hasGpgKey ? GPG_MASK : '')
      setClearGpgKey(false)
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
      const body: Parameters<typeof auth.updateProfile>[0] = {
        fullname,
        orgName,
        about,
        timezone,
        gitName,
        gitEmail,
        gpgKeyId,
        gpgSign: clearGpgKey ? false : gpgSign,
      }
      if (clearGpgKey) {
        body.clearGpgKey = true
      } else if (gpgPrivateKey && gpgPrivateKey !== GPG_MASK) {
        body.gpgPrivateKey = gpgPrivateKey
      }
      const user = await auth.updateProfile(body)
      setFullname(user.fullname ?? '')
      setOrgName(user.orgName ?? '')
      setAbout(user.about ?? '')
      setTimezone(user.timezone ?? '')
      setGitName(user.gitName ?? '')
      setGitEmail(user.gitEmail ?? '')
      setGpgKeyId(user.gpgKeyId ?? '')
      setGpgSign(Boolean(user.gpgSign))
      setHasGpgKey(Boolean(user.hasGpgKey))
      setGpgPrivateKey(user.hasGpgKey ? GPG_MASK : '')
      setClearGpgKey(false)
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
  const accountEmail = authCtx.status === 'ok' ? authCtx.user.email : ''

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

        <Typography.Title heading={6} style={{ margin: '8px 0 0' }}>
          {t('profile.gitTitle')}
        </Typography.Title>
        <p style={{ margin: 0, color: 'var(--semi-color-text-2)', fontSize: 13 }}>
          {t('profile.gitIntro')}
        </p>
        <Field label={t('profile.gitName')} hint={t('profile.gitNameHint')}>
          <Input
            value={gitName}
            maxLength={80}
            onChange={(v) => {
              setGitName(v)
              setDirty(true)
            }}
            placeholder={fullname || username || t('profile.gitNamePlaceholder')}
          />
        </Field>
        <Field label={t('profile.gitEmail')} hint={t('profile.gitEmailHint')}>
          <Input
            value={gitEmail}
            maxLength={254}
            onChange={(v) => {
              setGitEmail(v)
              setDirty(true)
            }}
            placeholder={accountEmail || t('profile.gitEmailPlaceholder')}
          />
        </Field>
        <Field label={t('profile.gpgKey')} hint={t('profile.gpgKeyHint')}>
          <TextArea
            value={clearGpgKey ? '' : gpgPrivateKey}
            disabled={clearGpgKey}
            autosize={{ minRows: 3, maxRows: 8 }}
            onChange={(v) => {
              setGpgPrivateKey(v)
              setClearGpgKey(false)
              setDirty(true)
            }}
            placeholder={t('profile.gpgKeyPlaceholder')}
          />
        </Field>
        <Field label={t('profile.gpgKeyId')} hint={t('profile.gpgKeyIdHint')}>
          <Input
            value={gpgKeyId}
            maxLength={64}
            disabled={clearGpgKey}
            onChange={(v) => {
              setGpgKeyId(v)
              setDirty(true)
            }}
            placeholder={t('profile.gpgKeyIdPlaceholder')}
          />
        </Field>
        <Checkbox
          checked={gpgSign && !clearGpgKey}
          disabled={clearGpgKey || (!hasGpgKey && (!gpgPrivateKey || gpgPrivateKey === GPG_MASK))}
          onChange={(e) => {
            setGpgSign(Boolean(e.target.checked))
            setDirty(true)
          }}
        >
          {t('profile.gpgSign')}
        </Checkbox>
        {(hasGpgKey || (gpgPrivateKey && gpgPrivateKey !== GPG_MASK)) && (
          <Checkbox
            checked={clearGpgKey}
            onChange={(e) => {
              const on = Boolean(e.target.checked)
              setClearGpgKey(on)
              if (on) setGpgSign(false)
              setDirty(true)
            }}
          >
            {t('profile.gpgClear')}
          </Checkbox>
        )}

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
