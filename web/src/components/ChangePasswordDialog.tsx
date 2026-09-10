import { useEffect, useId, useState, type FormEvent } from 'react'
import { ApiError, auth } from '../api'

type Props = {
  open: boolean
  onClose: () => void
}

export function ChangePasswordDialog({ open, onClose }: Props) {
  const titleId = useId()
  const [currentPassword, setCurrentPassword] = useState('')
  const [newPassword, setNewPassword] = useState('')
  const [confirmPassword, setConfirmPassword] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)

  useEffect(() => {
    if (!open) return
    setCurrentPassword('')
    setNewPassword('')
    setConfirmPassword('')
    setError(null)
    setBusy(false)
  }, [open])

  if (!open) return null

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
    <dialog
      className="modal modal-bottom sm:modal-middle modal-open"
      aria-labelledby={titleId}
    >
      <div className="modal-box max-w-md">
        <h3 id={titleId} className="font-display text-lg font-semibold">
          Change password
        </h3>
        <p className="mt-1 text-sm opacity-55">
          Other sessions will be signed out after you save.
        </p>

        <form
          onSubmit={(e) => void submit(e)}
          className="mt-5 flex flex-col gap-4"
        >
          <label className="form-control w-full gap-1.5">
            <span className="text-xs font-medium opacity-60">Current password</span>
            <input
              type="password"
              className="input input-bordered input-sm min-h-11 w-full sm:min-h-0"
              autoComplete="current-password"
              value={currentPassword}
              onChange={(e) => setCurrentPassword(e.target.value)}
              required
              autoFocus
            />
          </label>
          <label className="form-control w-full gap-1.5">
            <span className="text-xs font-medium opacity-60">New password</span>
            <input
              type="password"
              className="input input-bordered input-sm min-h-11 w-full sm:min-h-0"
              autoComplete="new-password"
              value={newPassword}
              onChange={(e) => setNewPassword(e.target.value)}
              minLength={8}
              required
            />
          </label>
          <label className="form-control w-full gap-1.5">
            <span className="text-xs font-medium opacity-60">Confirm new password</span>
            <input
              type="password"
              className="input input-bordered input-sm min-h-11 w-full sm:min-h-0"
              autoComplete="new-password"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              minLength={8}
              required
            />
          </label>

          {error && (
            <div className="text-sm text-error" role="alert">
              {error}
            </div>
          )}

          <div className="modal-action mt-2">
            <button
              type="button"
              className="btn btn-ghost btn-sm min-h-11 sm:min-h-0"
              onClick={onClose}
              disabled={busy}
            >
              Cancel
            </button>
            <button
              type="submit"
              className="btn btn-primary btn-sm min-h-11 sm:min-h-0"
              disabled={busy}
            >
              {busy ? 'Saving…' : 'Save password'}
            </button>
          </div>
        </form>
      </div>
      <form method="dialog" className="modal-backdrop">
        <button type="button" onClick={onClose} disabled={busy}>
          close
        </button>
      </form>
    </dialog>
  )
}
