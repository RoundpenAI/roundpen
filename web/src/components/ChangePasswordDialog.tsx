import { useState, type FormEvent } from 'react'
import { Banner, Button, Input, Modal, Typography } from '@douyinfe/semi-ui-19'
import { ApiError, auth } from '../api'

type Props = {
  open: boolean
  onClose: () => void
}

export function ChangePasswordDialog({ open, onClose }: Props) {
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
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
    if (newPassword.length < 8) {
      setError('New password must be at least 8 characters')
      return
    }
    if (newPassword !== confirmPassword) {
      setError('New passwords do not match')
      return
    }
    setBusy(true)
    setError(null)
    try {
      await auth.changePassword(currentPassword, newPassword)
      reset()
      onClose()
    } catch (err) {
      setError(
        err instanceof ApiError
          ? err.message
          : err instanceof Error
            ? err.message
            : 'failed to change password',
      )
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      title="Change password"
      visible={open}
      onCancel={() => {
        if (!busy) {
          reset()
          onClose()
        }
      }}
      footer={null}
      maskClosable={!busy}
      closeOnEsc={!busy}
      afterClose={reset}
    >
      <Typography.Text type="tertiary" style={{ display: 'block', marginBottom: 16 }}>
        Other sessions will be signed out after you save.
      </Typography.Text>
      <form
        onSubmit={(e) => void submit(e)}
        style={{ display: 'flex', flexDirection: 'column', gap: 12 }}
      >
        <div>
          <Typography.Text size="small" type="tertiary">
            Current password
          </Typography.Text>
          <Input
            mode="password"
            value={currentPassword}
            onChange={setCurrentPassword}
            autoComplete="current-password"
            aria-label="Current password"
            required
            autoFocus
          />
        </div>
        <div>
          <Typography.Text size="small" type="tertiary">
            New password
          </Typography.Text>
          <Input
            mode="password"
            value={newPassword}
            onChange={setNewPassword}
            autoComplete="new-password"
            aria-label="New password"
            minLength={8}
            required
          />
        </div>
        <div>
          <Typography.Text size="small" type="tertiary">
            Confirm new password
          </Typography.Text>
          <Input
            mode="password"
            value={confirmPassword}
            onChange={setConfirmPassword}
            autoComplete="new-password"
            aria-label="Confirm new password"
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
        <div
          style={{
            display: 'flex',
            justifyContent: 'flex-end',
            gap: 8,
            marginTop: 8,
          }}
        >
          <Button type="tertiary" onClick={onClose} disabled={busy}>
            Cancel
          </Button>
          <Button htmlType="submit" theme="solid" type="primary" loading={busy}>
            Save password
          </Button>
        </div>
      </form>
    </Modal>
  )
}
