import { useEffect, useId, useRef, useState, type FormEvent } from 'react'
import {
  templates,
  templateDisplayName,
  type BuildSpec,
  type BuildStatus,
  type Template,
} from '../api'

type Props = {
  open: boolean
  template: Template | null
  baseOptions?: Template[]
  busy?: boolean
  error?: string | null
  onClose: () => void
  onDone?: () => void
}

type BaseMode = 'image' | 'template'

function statusBadge(status: string): string {
  switch (status) {
    case 'ready':
      return 'badge badge-success badge-sm'
    case 'building':
      return 'badge badge-info badge-sm'
    case 'error':
      return 'badge badge-error badge-sm'
    default:
      return 'badge badge-ghost badge-sm'
  }
}

function isTerminalStatus(status: string): boolean {
  return status === 'ready' || status === 'error'
}

export function TemplateBuildDialog({
  open,
  template,
  baseOptions = [],
  busy = false,
  error = null,
  onClose,
  onDone,
}: Props) {
  const titleId = useId()
  const logRef = useRef<HTMLPreElement>(null)
  const onDoneRef = useRef(onDone)
  const notifiedDoneRef = useRef(false)
  const [baseMode, setBaseMode] = useState<BaseMode>('image')
  const [fromImage, setFromImage] = useState('alpine:3.20')
  const [fromTemplate, setFromTemplate] = useState('base')
  const [runCmd, setRunCmd] = useState('')
  const [startCmd, setStartCmd] = useState('')
  const [readyCmd, setReadyCmd] = useState('')
  const [phase, setPhase] = useState<'form' | 'monitor'>('form')
  const [buildStatus, setBuildStatus] = useState<BuildStatus | null>(null)
  const [localError, setLocalError] = useState<string | null>(null)
  const [starting, setStarting] = useState(false)
  const [watchBuild, setWatchBuild] = useState(false)

  onDoneRef.current = onDone

  useEffect(() => {
    if (!open || !template) return
    setBaseMode('image')
    setFromImage('alpine:3.20')
    setFromTemplate(baseOptions[0] ? templateDisplayName(baseOptions[0]) : 'base')
    setRunCmd('')
    setStartCmd('')
    setReadyCmd('')
    setLocalError(null)
    setStarting(false)
    setBuildStatus(null)
    notifiedDoneRef.current = false

    if (template.buildStatus === 'building') {
      setPhase('monitor')
      setWatchBuild(true)
      return
    }
    if (isTerminalStatus(template.buildStatus)) {
      setPhase('monitor')
      setWatchBuild(false)
      return
    }
    setPhase('form')
    setWatchBuild(false)
  }, [open, template, baseOptions])

  useEffect(() => {
    if (!open || !template || phase !== 'monitor') return
    let cancelled = false
    let offset = 0

    async function fetchOnce() {
      try {
        const st = await templates.buildStatus(
          template!.templateID,
          template!.buildID,
          0,
        )
        if (!cancelled) setBuildStatus(st)
      } catch (err) {
        if (!cancelled) {
          setLocalError(err instanceof Error ? err.message : 'status failed')
        }
      }
    }

    if (!watchBuild) {
      void fetchOnce()
      return () => {
        cancelled = true
      }
    }

    async function poll() {
      while (!cancelled) {
        try {
          const st = await templates.buildStatus(
            template!.templateID,
            template!.buildID,
            offset,
          )
          if (cancelled) return
          setBuildStatus(st)
          offset = st.logEntries?.length ?? st.logs.length
          if (st.status === 'ready') {
            if (!notifiedDoneRef.current) {
              notifiedDoneRef.current = true
              onDoneRef.current?.()
            }
            return
          }
          if (st.status === 'error') return
        } catch (err) {
          if (!cancelled) {
            setLocalError(err instanceof Error ? err.message : 'status failed')
          }
          return
        }
        await new Promise((r) => setTimeout(r, 2000))
      }
    }

    void poll()
    return () => {
      cancelled = true
    }
  }, [open, template, phase, watchBuild])

  useEffect(() => {
    logRef.current?.scrollTo(0, logRef.current.scrollHeight)
  }, [buildStatus?.logs, buildStatus?.logEntries])

  if (!open || !template) return null

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (!template) return
    setStarting(true)
    setLocalError(null)
    notifiedDoneRef.current = false

    const spec: BuildSpec = {
      cpuCount: template.cpuCount,
      memoryMB: template.memoryMB,
    }
    if (baseMode === 'image') {
      spec.fromImage = fromImage.trim() || 'alpine:3.20'
    } else {
      spec.fromTemplate = fromTemplate.trim()
    }
    const cmd = runCmd.trim()
    if (cmd) {
      spec.steps = [{ type: 'RUN', args: [cmd] }]
    }
    if (startCmd.trim()) spec.startCmd = startCmd.trim()
    if (readyCmd.trim()) spec.readyCmd = readyCmd.trim()

    try {
      await templates.startBuild(template.templateID, template.buildID, spec)
      setPhase('monitor')
      setWatchBuild(true)
      setBuildStatus({
        templateID: template.templateID,
        buildID: template.buildID,
        status: 'building',
        logs: [],
        logEntries: [],
      })
    } catch (err) {
      setLocalError(err instanceof Error ? err.message : 'build failed')
    } finally {
      setStarting(false)
    }
  }

  const showErr = localError || error
  const status = buildStatus?.status ?? template.buildStatus
  const logLines =
    buildStatus?.logEntries?.map((e) => e.message) ??
    buildStatus?.logs ??
    []
  const title = watchBuild || canBuild(template) ? 'Build' : 'Logs'

  return (
    <dialog className="modal modal-open" aria-labelledby={titleId}>
      <div className="modal-box flex max-h-[85vh] max-w-2xl flex-col">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h3 id={titleId} className="font-display text-lg font-semibold">
              {title} {templateDisplayName(template)}
            </h3>
            <p className="mt-1 font-mono text-xs opacity-50">
              {template.buildID.slice(0, 8)}…
            </p>
          </div>
          <span className={statusBadge(status)}>{status || 'waiting'}</span>
        </div>

        {phase === 'form' ? (
          <form
            onSubmit={(e) => void submit(e)}
            className="mt-5 flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto"
          >
            <div className="flex flex-wrap gap-2">
              <button
                type="button"
                className={`btn btn-xs ${baseMode === 'image' ? 'btn-primary' : 'btn-ghost'}`}
                onClick={() => setBaseMode('image')}
              >
                From image
              </button>
              <button
                type="button"
                className={`btn btn-xs ${baseMode === 'template' ? 'btn-primary' : 'btn-ghost'}`}
                onClick={() => setBaseMode('template')}
              >
                From template
              </button>
            </div>

            {baseMode === 'image' ? (
              <label className="form-control w-full gap-1.5">
                <span className="text-xs font-medium opacity-60">Base image</span>
                <input
                  className="input input-bordered input-sm w-full font-mono"
                  value={fromImage}
                  onChange={(e) => setFromImage(e.target.value)}
                  placeholder="alpine:3.20"
                />
              </label>
            ) : (
              <label className="form-control w-full gap-1.5">
                <span className="text-xs font-medium opacity-60">Base template</span>
                <select
                  className="select select-bordered select-sm w-full"
                  value={fromTemplate}
                  onChange={(e) => setFromTemplate(e.target.value)}
                >
                  {baseOptions.map((tpl) => {
                    const name = templateDisplayName(tpl)
                    return (
                      <option key={tpl.templateID} value={name}>
                        {name}
                      </option>
                    )
                  })}
                </select>
              </label>
            )}

            <label className="form-control w-full gap-1.5">
              <span className="text-xs font-medium opacity-60">RUN command (optional)</span>
              <input
                className="input input-bordered input-sm w-full font-mono"
                value={runCmd}
                onChange={(e) => setRunCmd(e.target.value)}
                placeholder="apk add --no-cache curl"
              />
            </label>

            <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
              <label className="form-control w-full gap-1.5">
                <span className="text-xs font-medium opacity-60">Start command</span>
                <input
                  className="input input-bordered input-sm w-full font-mono"
                  value={startCmd}
                  onChange={(e) => setStartCmd(e.target.value)}
                  placeholder="optional long-running cmd"
                />
              </label>
              <label className="form-control w-full gap-1.5">
                <span className="text-xs font-medium opacity-60">Ready probe</span>
                <input
                  className="input input-bordered input-sm w-full font-mono"
                  value={readyCmd}
                  onChange={(e) => setReadyCmd(e.target.value)}
                  placeholder="waitForPort(8080)"
                />
              </label>
            </div>

            <p className="text-xs opacity-45">
              Image builds require the docker backend on roundpend. Kern dev uses
              built-in templates only.
            </p>

            {showErr && (
              <p className="text-sm text-error" role="alert">
                {showErr}
              </p>
            )}

            <div className="modal-action mt-1">
              <button
                type="button"
                className="btn btn-ghost btn-sm"
                disabled={busy || starting}
                onClick={onClose}
              >
                Cancel
              </button>
              <button
                type="submit"
                className="btn btn-primary btn-sm"
                disabled={busy || starting}
              >
                {starting ? 'Starting…' : 'Start build'}
              </button>
            </div>
          </form>
        ) : (
          <div className="mt-5 flex min-h-0 flex-1 flex-col gap-3">
            {buildStatus?.reason?.message && (
              <p className="text-sm text-error">{buildStatus.reason.message}</p>
            )}
            <pre
              ref={logRef}
              className="min-h-[12rem] flex-1 overflow-auto rounded-lg border border-base-300 bg-base-200/50 p-3 font-mono text-xs leading-relaxed"
            >
              {logLines.length > 0
                ? logLines.join('\n')
                : status === 'building'
                  ? 'Building…'
                  : status === 'ready'
                    ? 'Build ready.'
                    : 'No logs yet.'}
            </pre>
            {showErr && (
              <p className="text-sm text-error" role="alert">
                {showErr}
              </p>
            )}
            <div className="modal-action mt-1">
              <button
                type="button"
                className="btn btn-primary btn-sm"
                disabled={busy}
                onClick={onClose}
              >
                Close
              </button>
            </div>
          </div>
        )}
      </div>
      <form method="dialog" className="modal-backdrop">
        <button type="button" disabled={busy || starting} onClick={onClose}>
          close
        </button>
      </form>
    </dialog>
  )
}

function canBuild(t: Template): boolean {
  return t.buildStatus === 'waiting' || t.buildStatus === 'error'
}
