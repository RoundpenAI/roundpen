import { useState, type FormEvent } from 'react'
import { Banner, Button, Input, Typography } from '@douyinfe/semi-ui-19'
import { ApiError, auth } from '../api'
import { useT } from '../i18n'

type Props = {
  // onDone runs after the password changed; onCancel is only rendered when set.
  onDone?: () => void
  onCancel?: () => void
}

export function ChangePasswordForm({ onDone, onCancel }: Props) {
  const t = useT()
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [changed, setChanged] = useState(false)
  const [busy, setBusy] = useState(false)

  function reset() {
    setCurrentPassword('')
    setNewPassword('')
    setConfirmPassword('')
    setError(null)
    setBusy(false)
  }

  async function submit(e: FormEvent) {
    e.preventDefault()
    setChanged(false)
    if (newPassword.length < 8) {
      setError(t('settings.password.tooShort'))
      return
    }
    if (newPassword !== confirmPassword) {
      setError(t('settings.password.mismatch'))
      return
    }
    setBusy(true)
    setError(null)
    try {
      await auth.changePassword(currentPassword, newPassword)
      reset()
      setChanged(true)
      onDone?.()
    } catch (err) {
      setError(
        err instanceof ApiError
          ? err.message
          : err instanceof Error
            ? err.message
            : t('settings.password.failed'),
      )
    } finally {
      setBusy(false)
    }
  }

  return (
    <form
      onSubmit={(e) => void submit(e)}
      style={{ display: 'flex', flexDirection: 'column', gap: 12 }}
    >
      <Typography.Text type="tertiary">
        {t('settings.password.intro')}
      </Typography.Text>
      <div>
        <Typography.Text size="small" type="tertiary">
          {t('settings.password.current')}
        </Typography.Text>
        <Input
          mode="password"
          value={currentPassword}
          onChange={setCurrentPassword}
          autoComplete="current-password"
          aria-label={t('settings.password.current')}
          required
          autoFocus
        />
      </div>
      <div>
        <Typography.Text size="small" type="tertiary">
          {t('settings.password.new')}
        </Typography.Text>
        <Input
          mode="password"
          value={newPassword}
          onChange={setNewPassword}
          autoComplete="new-password"
          aria-label={t('settings.password.new')}
          minLength={8}
          required
        />
      </div>
      <div>
        <Typography.Text size="small" type="tertiary">
          {t('settings.password.confirm')}
        </Typography.Text>
        <Input
          mode="password"
          value={confirmPassword}
          onChange={setConfirmPassword}
          autoComplete="new-password"
          aria-label={t('settings.password.confirm')}
          minLength={8}
          required
        />
      </div>
      {error && (
        <div role="alert">
          <Banner
            fullMode={false}
            type="danger"
            description={error}
            closeIcon={null}
          />
        </div>
      )}
      {changed && (
        <div role="status">
          <Banner
            fullMode={false}
            type="success"
            description={t('settings.password.done')}
            closeIcon={null}
          />
        </div>
      )}
      <div
        style={{
          display: 'flex',
          justifyContent: 'flex-end',
          gap: 8,
          marginTop: 8,
        }}
      >
        {onCancel && (
          <Button type="tertiary" onClick={onCancel} disabled={busy}>
            {t('settings.password.cancel')}
          </Button>
        )}
        <Button htmlType="submit" theme="solid" type="primary" loading={busy}>
          {t('settings.password.submit')}
        </Button>
      </div>
    </form>
  )
}
