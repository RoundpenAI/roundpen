import { useEffect, useState } from 'react'
import {
  Banner,
  Button,
  Checkbox,
  Input,
  Modal,
  Select,
  Typography,
} from '@douyinfe/semi-ui-19'
import { SUGGESTED_CATEGORIES, type Sandbox } from '../api'

export type SandboxEditValues = {
  name: string
  category: string
  isDefault: boolean
}

type Props = {
  open: boolean
  sandbox: Sandbox | null
  categoryOptions?: string[]
  busy?: boolean
  error?: string | null
  onClose: () => void
  onSave: (values: SandboxEditValues) => void | Promise<void>
}

export function SandboxEditDialog({
  open,
  sandbox,
  categoryOptions = [...SUGGESTED_CATEGORIES],
  busy = false,
  error = null,
  onClose,
  onSave,
}: Props) {
  const [name, setName] = useState('')
  const [category, setCategory] = useState('')
  const [isDefault, setIsDefault] = useState(false)

  useEffect(() => {
    if (!open || !sandbox) return
    setName(sandbox.name || '')
    setCategory(sandbox.category || '')
    setIsDefault(Boolean(sandbox.isDefault))
  }, [open, sandbox])

  async function submit() {
    const trimmed = name.trim()
    if (!trimmed) return
    const cat = category.trim()
    await onSave({
      name: trimmed,
      category: cat,
      isDefault: Boolean(cat && isDefault),
    })
  }

  const categorySelectOptions = categoryOptions.map((c) => ({
    label: c,
    value: c,
  }))

  return (
    <Modal
      title="Edit sandbox"
      visible={open && sandbox != null}
      onCancel={() => {
        if (!busy) onClose()
      }}
      footer={null}
      maskClosable={!busy}
      closeOnEsc={!busy}
      width={448}
    >
      {sandbox && (
        <>
          <Typography.Text
            type="tertiary"
            size="small"
            style={{
              display: 'block',
              marginBottom: 16,
              fontFamily: 'var(--semi-font-family-regular), monospace',
              overflow: 'hidden',
              textOverflow: 'ellipsis',
              whiteSpace: 'nowrap',
            }}
          >
            {sandbox.sandboxID}
          </Typography.Text>

          <form
            onSubmit={(e) => {
              e.preventDefault()
              void submit()
            }}
            style={{ display: 'flex', flexDirection: 'column', gap: 16 }}
          >
            <div>
              <Typography.Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 4 }}>
                Name
              </Typography.Text>
              <Input
                value={name}
                onChange={setName}
                maxLength={64}
                required
                autoFocus
              />
            </div>

            <div>
              <Typography.Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 4 }}>
                Category
              </Typography.Text>
              <Select
                filter
                allowCreate
                showClear
                style={{ width: '100%' }}
                value={category || undefined}
                onChange={(v) => setCategory(typeof v === 'string' ? v : '')}
                optionList={categorySelectOptions}
                placeholder="e.g. Browser"
              />
            </div>

            <Checkbox
              checked={isDefault}
              disabled={!category.trim()}
              onChange={(e) => setIsDefault(Boolean(e.target.checked))}
              extra="Agents resolve by category (e.g. Browser → this sandbox)."
            >
              Default for this category
            </Checkbox>

            {error && (
              <div role="alert">
                <Banner fullMode={false} type="danger" description={error} closeIcon={null} />
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
              <Button
                htmlType="submit"
                theme="solid"
                type="primary"
                loading={busy}
                disabled={!name.trim()}
              >
                Save
              </Button>
            </div>
          </form>
        </>
      )}
    </Modal>
  )
}
