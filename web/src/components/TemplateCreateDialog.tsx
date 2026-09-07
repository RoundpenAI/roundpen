import { useEffect, useId, useState, type FormEvent } from 'react'

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
  const titleId = useId()
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

  if (!open) return null

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
    <dialog className="modal modal-bottom sm:modal-middle modal-open" aria-labelledby={titleId}>
      <div className="modal-box max-w-md">
        <h3 id={titleId} className="font-display text-lg font-semibold">
          New image
        </h3>
        <p className="mt-1 text-sm opacity-55">
          Creates an environment image template and pending build. Agent slots
          build OCI images; Browser slots target qcow2 disks.
        </p>

        <form onSubmit={(e) => void submit(e)} className="mt-5 flex flex-col gap-4">
          <label className="form-control w-full gap-1.5">
            <span className="text-xs font-medium opacity-60">Slot</span>
            <select
              className="select select-bordered select-sm w-full"
              value={slot}
              onChange={(e) => setSlot(e.target.value as TemplateCreateValues['slot'])}
            >
              <option value="agent">Agent (OCI)</option>
              <option value="browser">Browser (qcow2)</option>
            </select>
          </label>

          <label className="form-control w-full gap-1.5">
            <span className="text-xs font-medium opacity-60">Name</span>
            <input
              className="input input-bordered input-sm w-full"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="my-agent-image"
              pattern="[a-zA-Z0-9][a-zA-Z0-9._-]*"
              maxLength={64}
              autoFocus
              required
            />
          </label>

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
            <label className="form-control w-full gap-1.5">
              <span className="text-xs font-medium opacity-60">CPU</span>
              <input
                type="number"
                min={1}
                className="input input-bordered input-sm w-full"
                value={cpuCount}
                onChange={(e) => setCpuCount(Number(e.target.value) || 1)}
              />
            </label>
            <label className="form-control w-full gap-1.5">
              <span className="text-xs font-medium opacity-60">Memory (MiB)</span>
              <input
                type="number"
                min={128}
                step={128}
                className="input input-bordered input-sm w-full"
                value={memoryMB}
                onChange={(e) => setMemoryMB(Number(e.target.value) || 512)}
              />
            </label>
          </div>

          <label className="flex cursor-pointer items-center gap-2.5">
            <input
              type="checkbox"
              className="checkbox checkbox-sm"
              checked={isPublic}
              onChange={(e) => setIsPublic(e.target.checked)}
            />
            <span className="text-sm">Public template</span>
          </label>

          {error && (
            <p className="text-sm text-error" role="alert">
              {error}
            </p>
          )}

          <div className="modal-action mt-1">
            <button
              type="button"
              className="btn btn-ghost btn-sm"
              disabled={busy}
              onClick={onClose}
            >
              Cancel
            </button>
            <button type="submit" className="btn btn-primary btn-sm" disabled={busy}>
              {busy ? 'Creating…' : 'Create'}
            </button>
          </div>
        </form>
      </div>
      <form method="dialog" className="modal-backdrop">
        <button type="button" disabled={busy} onClick={onClose}>
          close
        </button>
      </form>
    </dialog>
  )
}
