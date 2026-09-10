import { useCallback, useEffect, useState } from 'react'
import {
  ApiError,
  environments,
  runtime,
  type EngineStatus,
  type RuntimeSnapshot,
  type SetupStep,
} from '../api'

type Props = {
  /** When set, only render this engine (e.g. qemu on the Browser page). */
  engineId?: string
  showPicker?: boolean
}

export function EngineSetupList({ steps }: { steps: SetupStep[] }) {
  if (!steps.length) return null
  return (
    <ol className="m-0 list-decimal space-y-3 pl-5 text-sm">
      {steps.map((step) => (
        <li key={step.title}>
          <div className="font-medium">{step.title}</div>
          {step.detail ? (
            <p className="mt-0.5 text-[0.8rem] leading-relaxed opacity-60">{step.detail}</p>
          ) : null}
          {step.command ? (
            <pre className="mt-1 overflow-x-auto rounded-md bg-base-200 px-2 py-1.5 font-mono text-[0.75rem]">
              {step.command}
            </pre>
          ) : null}
        </li>
      ))}
    </ol>
  )
}

export function RuntimePanel({ engineId, showPicker = true }: Props) {
  const [snap, setSnap] = useState<RuntimeSnapshot | nil>(null)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [starting, setStarting] = useState(false)

  const load = useCallback(async () => {
    setError(null)
    try {
      setSnap(await runtime.get())
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load runtime')
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const engines = (snap?.engines ?? []).filter((e) => !engineId || e.id === engineId)
  const selected = snap?.agentEngine || snap?.defaultAgentEngine || 'qemu'
  const current = engines.find((e) => e.id === selected) ?? engines[0]

  async function pick(id: string) {
    setSaving(true)
    setError(null)
    try {
      setSnap(await runtime.setAgentEngine(id))
    } catch (e) {
      setError(e instanceof Error ? e.message : 'save failed')
    } finally {
      setSaving(false)
    }
  }

  async function startAgent() {
    if (!current) return
    setStarting(true)
    setError(null)
    try {
      await environments.ensureAgent(current.id)
      await load()
    } catch (e) {
      if (e instanceof ApiError && e.setup?.length) {
        setError(e.message)
        setSnap((prev) => {
          if (!prev) return prev
          return {
            ...prev,
            engines: prev.engines.map((eng) =>
              eng.id === (e.engine || current.id) ? { ...eng, setup: e.setup } : eng,
            ),
          }
        })
      } else {
        setError(e instanceof Error ? e.message : 'start failed')
      }
    } finally {
      setStarting(false)
    }
  }

  return (
    <section className="space-y-4">
      <h2 className="text-sm font-medium">
        {engineId === 'qemu' ? 'Browser / QEMU' : 'Agent runtime'}
      </h2>
      <p className="m-0 text-[0.8rem] leading-relaxed opacity-55">
        {engineId
          ? 'This slot needs a QEMU VM image on the host. If anything is missing, install it here then retry.'
          : 'Pick how Cloud Agent runs on this machine. Browser desktops always use QEMU. If the host is missing binaries or images, follow the setup steps — no process restart is required after they are installed.'}
      </p>
      {error ? (
        <p className="m-0 text-sm text-error" role="alert">
          {error}
        </p>
      ) : null}
      {!snap ? (
        <p className="text-sm opacity-50">Loading runtimes…</p>
      ) : (
        <div className="space-y-3">
          {engines.map((eng) => (
            <EngineCard
              key={eng.id}
              engine={eng}
              selected={showPicker && selected === eng.id}
              selectable={showPicker}
              disabled={saving}
              onSelect={() => void pick(eng.id)}
            />
          ))}
          {showPicker ? (
            <div className="flex flex-wrap items-center gap-2">
              <button
                type="button"
                className="btn btn-primary btn-sm"
                disabled={starting || saving || !current}
                onClick={() => void startAgent()}
              >
                {starting ? 'Starting…' : current?.agentReady ? 'Start / resume Agent' : 'Retry after setup'}
              </button>
              <button type="button" className="btn btn-ghost btn-sm" onClick={() => void load()}>
                Recheck host
              </button>
            </div>
          ) : null}
        </div>
      )}
    </section>
  )
}

function EngineCard({
  engine,
  selected,
  selectable,
  disabled,
  onSelect,
}: {
  engine: EngineStatus
  selected: boolean
  selectable: boolean
  disabled: boolean
  onSelect: () => void
}) {
  const ready = selectable ? engine.agentReady : engine.browserReady || engine.ready
  return (
    <div
      className={`rounded-lg border p-4 ${selected ? 'border-primary' : 'border-base-300'}`}
    >
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div>
          <div className="font-medium">{engine.label}</div>
          <p className="mt-1 text-[0.8rem] leading-relaxed opacity-55">{engine.summary}</p>
        </div>
        <div className="flex items-center gap-2">
          <span className={`badge badge-sm ${ready ? 'badge-success' : 'badge-warning'}`}>
            {ready ? 'Ready' : 'Needs setup'}
          </span>
          {selectable ? (
            <button
              type="button"
              className={`btn btn-sm ${selected ? 'btn-primary' : 'btn-outline'}`}
              disabled={disabled}
              onClick={onSelect}
            >
              {selected ? 'Selected' : 'Use this'}
            </button>
          ) : null}
        </div>
      </div>
      {!ready && engine.setup?.length ? (
        <div className="mt-3">
          <EngineSetupList steps={engine.setup} />
        </div>
      ) : null}
    </div>
  )
}
