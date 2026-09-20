import { useState } from 'react'
import { Modal } from '@douyinfe/semi-ui-19'
import { ChangePasswordForm } from './ChangePasswordForm'
import { useT } from '../i18n'

type Props = {
  open: boolean
  onClose: () => void
}

export function ChangePasswordDialog({ open, onClose }: Props) {
  const t = useT()
  // Remounts the form on every open so stale input never survives a reopen.
  const [formKey, setFormKey] = useState(0)

  return (
    <Modal
      title={t('settings.password.title')}
      visible={open}
      onCancel={onClose}
      footer={null}
      afterClose={() => setFormKey((n) => n + 1)}
    >
      <ChangePasswordForm key={formKey} onDone={onClose} onCancel={onClose} />
    </Modal>
  )
}
