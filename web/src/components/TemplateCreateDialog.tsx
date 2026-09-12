import { useEffect, useState, type FormEvent } from 'react'
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

export type TemplateCreateValues = {
  name: string
  cpuCount: number
  memoryMB: number
  public: boolean
  slot: 'agent' | 'browser'
}

type Props = {
  open: boolean
  busy?: boolean
  error?: string | null
  onClose: () => void
  onCreate: (values: TemplateCreateValues) => void | Promise<void>
}

const defaults: TemplateCreateValues = {
  name: '',
  cpuCount: 1,
  memoryMB: 512,
  public: true,
  slot: 'agent',
}

export function TemplateCreateDialog({
  open,
  busy = false,
  error = null,
  onClose,
  onCreate,
}: Props) {
  const [name, setName] = useState(defaults.name)
  const [cpuCount, setCpuCount] = useState(defaults.cpuCount)
  const [memoryMB, setMemoryMB] = useState(defaults.memoryMB)
  const [isPublic, setIsPublic] = useState(defaults.public)
  const [slot, setSlot] = useState<TemplateCreateValues['slot']>(defaults.slot)

  useEffect(() => {
    if (!open) return
    setName(defaults.name)
    setCpuCount(defaults.cpuCount)
    setMemoryMB(defaults.memoryMB)
    setIsPublic(defaults.public)
    setSlot(defaults.slot)
  }, [open])

  async function submit(e: FormEvent) {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) return
    await onCreate({
      name: trimmed,
      cpuCount: cpuCount > 0 ? cpuCount : 1,
      memoryMB: memoryMB > 0 ? memoryMB : 512,
      public: isPublic,
      slot,
    })
  }

  return (
    <Modal
      title="New image"
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
        Creates an environment image template and pending build. Agent slots
        build OCI images; Browser slots target qcow2 disks.
      </Typography.Text>

      <form
        onSubmit={(e) => void submit(e)}
        style={{ display: 'flex', flexDirection: 'column', gap: 16 }}
      >
        <div>
          <Typography.Text size="small" type="tertiary">
            Slot
          </Typography.Text>
          <Select
            value={slot}
            onChange={(v) => setSlot(v as TemplateCreateValues['slot'])}
            optionList={[
              { value: 'agent', label: 'Agent (OCI)' },
              { value: 'browser', label: 'Browser (qcow2)' },
            ]}
            style={{ width: '100%' }}
          />
        </div>

        <div>
          <Typography.Text size="small" type="tertiary">
            Name
          </Typography.Text>
          <Input
            value={name}
            onChange={setName}
            placeholder="my-agent-image"
            maxLength={64}
            autoFocus
            required
            pattern="[a-zA-Z0-9][a-zA-Z0-9._-]*"
          />
        </div>

        <div
          style={{
            display: 'grid',
            gridTemplateColumns: '1fr 1fr',
            gap: 12,
          }}
        >
          <div>
            <Typography.Text size="small" type="tertiary">
              CPU
            </Typography.Text>
            <InputNumber
              min={1}
              value={cpuCount}
              onChange={(v) => setCpuCount(typeof v === 'number' ? v : 1)}
              style={{ width: '100%' }}
            />
          </div>
          <div>
            <Typography.Text size="small" type="tertiary">
              Memory (MiB)
            </Typography.Text>
            <InputNumber
              min={128}
              step={128}
              value={memoryMB}
              onChange={(v) => setMemoryMB(typeof v === 'number' ? v : 512)}
              style={{ width: '100%' }}
            />
          </div>
        </div>

        <Checkbox
          checked={isPublic}
          onChange={(e) => setIsPublic(!!e.target.checked)}
        >
          Public template
        </Checkbox>

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
            marginTop: 4,
          }}
        >
          <Button type="tertiary" disabled={busy} onClick={onClose}>
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
