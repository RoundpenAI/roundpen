import { useEffect, useState } from 'react'
import {
  Banner,
  Button,
  Checkbox,
  Input,
  InputNumber,
  Modal,
  Select,
  Typography,
} from '@douyinfe/semi-ui-19'
import { SUGGESTED_CATEGORIES, templates as templatesApi, type Template } from '../api'

export type SandboxCreateValues = {
  name: string
  category: string
  isDefault: boolean
  templateID: string
  timeoutSec: number
}

type Props = {
  open: boolean
  categoryOptions?: string[]
  busy?: boolean
  error?: string | null
  onClose: () => void
  onCreate: (values: SandboxCreateValues) => void | Promise<void>
}

const defaults: SandboxCreateValues = {
  name: '',
  category: '',
  isDefault: false,
  templateID: 'host',
  timeoutSec: 3600,
}

export function SandboxCreateDialog({
  open,
  categoryOptions = [...SUGGESTED_CATEGORIES],
  busy = false,
  error = null,
  onClose,
  onCreate,
}: Props) {
  const [name, setName] = useState(defaults.name)
  const [category, setCategory] = useState(defaults.category)
  const [isDefault, setIsDefault] = useState(defaults.isDefault)
  const [templateID, setTemplateID] = useState(defaults.templateID)
  const [timeoutSec, setTimeoutSec] = useState(defaults.timeoutSec)
  const [templateOptions, setTemplateOptions] = useState<Template[]>([])
  const [templatesLoading, setTemplatesLoading] = useState(false)

  useEffect(() => {
    if (!open) return
    setName(defaults.name)
    setCategory(defaults.category)
    setIsDefault(defaults.isDefault)
    setTemplateID(defaults.templateID)
    setTimeoutSec(defaults.timeoutSec)
    setTemplatesLoading(true)
    templatesApi
      .list()
      .then((list) => {
        setTemplateOptions(list)
        if (list.length > 0 && !list.some((t) => t.names.includes(defaults.templateID))) {
          const first = list[0].names[0] ?? 'host'
          setTemplateID(first.includes('/') ? first.split('/').pop() ?? first : first)
        }
      })
      .catch(() => setTemplateOptions([]))
      .finally(() => setTemplatesLoading(false))
  }, [open])

  async function submit() {
    const cat = category.trim()
    await onCreate({
      name: name.trim(),
      category: cat,
      isDefault: Boolean(cat && isDefault),
      templateID: templateID.trim() || 'host',
      timeoutSec: timeoutSec > 0 ? timeoutSec : 3600,
    })
  }

  const categorySelectOptions = categoryOptions.map((c) => ({
    label: c,
    value: c,
  }))

  const templateSelectOptions = templateOptions.flatMap((tpl) =>
    tpl.names.map((n) => {
      const value = n.includes('/') ? n.split('/').pop() ?? n : n
      return {
        label: `${n} (${tpl.cpuCount}c / ${tpl.memoryMB}MiB)`,
        value,
        key: `${tpl.templateID}-${n}`,
      }
    }),
  )

  return (
    <Modal
      title="New sandbox"
      visible={open}
      onCancel={() => {
        if (!busy) onClose()
      }}
      footer={null}
      maskClosable={!busy}
      closeOnEsc={!busy}
      width={448}
    >
      <Typography.Text type="tertiary" style={{ display: 'block', marginBottom: 16 }}>
        Name and category help agents find this sandbox later.
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
            placeholder="optional — auto if empty"
            maxLength={64}
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

        <div
          style={{
            display: 'grid',
            gridTemplateColumns: '1fr 1fr',
            gap: 12,
          }}
        >
          <div>
            <Typography.Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 4 }}>
              Template
            </Typography.Text>
            {templateOptions.length > 0 ? (
              <Select
                style={{ width: '100%' }}
                value={templateID}
                onChange={(v) => setTemplateID(String(v))}
                disabled={templatesLoading}
                optionList={templateSelectOptions}
              />
            ) : (
              <Input
                value={templateID}
                onChange={setTemplateID}
                placeholder="host"
                disabled={templatesLoading}
              />
            )}
          </div>
          <div>
            <Typography.Text size="small" type="tertiary" style={{ display: 'block', marginBottom: 4 }}>
              TTL (sec)
            </Typography.Text>
            <InputNumber
              style={{ width: '100%' }}
              min={60}
              value={timeoutSec}
              onChange={(v) => setTimeoutSec(typeof v === 'number' ? v : 3600)}
            />
          </div>
        </div>

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
          <Button htmlType="submit" theme="solid" type="primary" loading={busy}>
            Create
          </Button>
        </div>
      </form>
    </Modal>
  )
}
