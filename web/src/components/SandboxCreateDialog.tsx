import { useEffect, useId, useState, type FormEvent } from 'react'
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
  const titleId = useId()
  const listId = useId()
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

  if (!open) return null

  async function submit(e: FormEvent) {
    e.preventDefault()
    const cat = category.trim()
    await onCreate({
      name: name.trim(),
      category: cat,
      isDefault: Boolean(cat && isDefault),
      templateID: templateID.trim() || 'host',
      timeoutSec: timeoutSec > 0 ? timeoutSec : 3600,
    })
  }

  return (
    <dialog className="modal modal-open" aria-labelledby={titleId}>
      <div className="modal-box max-w-md">
        <h3 id={titleId} className="font-display text-lg font-semibold">
          New sandbox
        </h3>
        <p className="mt-1 text-sm opacity-55">
          Name and category help agents find this sandbox later.
        </p>

        <form onSubmit={(e) => void submit(e)} className="mt-5 flex flex-col gap-5">
          <label className="form-control w-full gap-1.5">
            <span className="text-xs font-medium opacity-60">Name</span>
            <input
              className="input input-bordered input-sm w-full"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="optional — auto if empty"
              maxLength={64}
              autoFocus
            />
          </label>

          <div className="flex flex-col gap-3">
            <label className="form-control w-full gap-1.5">
              <span className="text-xs font-medium opacity-60">Category</span>
              <input
                className="input input-bordered input-sm w-full"
                list={listId}
                value={category}
                onChange={(e) => setCategory(e.target.value)}
                placeholder="e.g. Browser"
                maxLength={32}
              />
              <datalist id={listId}>
                {categoryOptions.map((c) => (
                  <option key={c} value={c} />
                ))}
              </datalist>
            </label>

            <div className="rounded-lg border border-base-300/80 bg-base-200/40 px-3 py-2.5">
              <label className="flex cursor-pointer items-start gap-2.5">
                <input
                  type="checkbox"
                  className="checkbox checkbox-sm mt-0.5 shrink-0"
                  checked={isDefault}
                  disabled={!category.trim()}
                  onChange={(e) => setIsDefault(e.target.checked)}
                />
                <span className="min-w-0">
                  <span className="block text-sm leading-snug">
                    Default for this category
                  </span>
                  <span className="mt-1 block text-xs leading-relaxed opacity-50">
                    Agents resolve by category (e.g. Browser → this sandbox).
                  </span>
                </span>
              </label>
            </div>
          </div>

          <div className="grid grid-cols-2 gap-3">
            <label className="form-control w-full gap-1.5">
              <span className="text-xs font-medium opacity-60">Template</span>
              {templateOptions.length > 0 ? (
                <select
                  className="select select-bordered select-sm w-full"
                  value={templateID}
                  onChange={(e) => setTemplateID(e.target.value)}
                  disabled={templatesLoading}
                >
                  {templateOptions.flatMap((tpl) =>
                    tpl.names.map((name) => {
                      const value = name.includes('/') ? name.split('/').pop() ?? name : name
                      return (
                        <option key={`${tpl.templateID}-${name}`} value={value}>
                          {name} ({tpl.cpuCount}c / {tpl.memoryMB}MiB)
                        </option>
                      )
                    }),
                  )}
                </select>
              ) : (
                <input
                  className="input input-bordered input-sm w-full"
                  value={templateID}
                  onChange={(e) => setTemplateID(e.target.value)}
                  placeholder="host"
                  disabled={templatesLoading}
                />
              )}
            </label>
            <label className="form-control w-full gap-1.5">
              <span className="text-xs font-medium opacity-60">TTL (sec)</span>
              <input
                type="number"
                min={60}
                className="input input-bordered input-sm w-full"
                value={timeoutSec}
                onChange={(e) => setTimeoutSec(Number(e.target.value) || 3600)}
              />
            </label>
          </div>

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
            <button
              type="submit"
              className="btn btn-primary btn-sm"
              disabled={busy}
            >
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
