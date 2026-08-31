import { useEffect, useId, useState, type FormEvent } from 'react'
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
  const titleId = useId()
  const listId = useId()
  const [name, setName] = useState('')
  const [category, setCategory] = useState('')
  const [isDefault, setIsDefault] = useState(false)

  useEffect(() => {
    if (!open || !sandbox) return
    setName(sandbox.name || '')
    setCategory(sandbox.category || '')
    setIsDefault(Boolean(sandbox.isDefault))
  }, [open, sandbox])

  if (!open || !sandbox) return null

  async function submit(e: FormEvent) {
    e.preventDefault()
    const trimmed = name.trim()
    if (!trimmed) return
    const cat = category.trim()
    await onSave({
      name: trimmed,
      category: cat,
      isDefault: Boolean(cat && isDefault),
    })
  }

  return (
    <dialog className="modal modal-bottom sm:modal-middle modal-open" aria-labelledby={titleId}>
      <div className="modal-box max-w-md">
        <h3 id={titleId} className="font-display text-lg font-semibold">
          Edit sandbox
        </h3>
        <p className="mt-1 font-mono text-xs opacity-50 truncate">
          {sandbox.sandboxID}
        </p>

        <form onSubmit={(e) => void submit(e)} className="mt-5 flex flex-col gap-5">
          <label className="form-control w-full gap-1.5">
            <span className="text-xs font-medium opacity-60">Name</span>
            <input
              className="input input-bordered input-sm w-full"
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={64}
              required
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
              disabled={busy || !name.trim()}
            >
              {busy ? 'Saving…' : 'Save'}
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
