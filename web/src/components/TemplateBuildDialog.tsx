import { useEffect, useId, useRef, useState, type FormEvent } from 'react'
import {
  templates,
  templateDisplayName,
  type BuildSpec,
  type BuildStatus,
  type Template,
} from '../api'
import { AnsiText } from './AnsiText'

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

function canRetryBuild(status: string): boolean {
  return status === 'waiting' || status === 'error'
}

function applySpecToForm(
  spec: BuildSpec | undefined,
  setters: {
    setBaseMode: (m: BaseMode) => void
    setFromImage: (v: string) => void
    setFromTemplate: (v: string) => void
    setRunCmd: (v: string) => void
    setStartCmd: (v: string) => void
    setReadyCmd: (v: string) => void
  },
  fallbackTemplate: string,
) {
  if (!spec) return
  if (spec.fromTemplate) {
    setters.setBaseMode('template')
    setters.setFromTemplate(spec.fromTemplate)
  } else {
    setters.setBaseMode('image')
    setters.setFromImage(spec.fromImage?.trim() || 'alpine:3.20')
  }
  const run = spec.steps?.find(
    (s) => s.type.toUpperCase() === 'RUN' || s.type.toUpperCase() === 'RUNCMD',
  )
  setters.setRunCmd(run?.args?.[0] ?? '')
  setters.setStartCmd(spec.startCmd ?? '')
  setters.setReadyCmd(spec.readyCmd ?? '')
  if (!spec.fromTemplate && !spec.fromImage) {
    setters.setFromTemplate(fallbackTemplate)
  }
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
  const [activeBuildID, setActiveBuildID] = useState('')
  const [versionTag, setVersionTag] = useState('')
  const [assignDefault, setAssignDefault] = useState(true)

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
    setActiveBuildID(template.buildID)
    setVersionTag('')
    setAssignDefault(true)
    notifiedDoneRef.current = false

    if (template.buildStatus === 'building') {
      setPhase('monitor')
      setWatchBuild(true)
      return
    }
    // ready: logs only. error: logs + Retry. waiting: configure form.
    if (template.buildStatus === 'ready' || template.buildStatus === 'error') {
      setPhase('monitor')
      setWatchBuild(false)
      return
    }
    setPhase('form')
    setWatchBuild(false)
  }, [open, template?.templateID, template?.buildID])

  useEffect(() => {
    if (!open || !template || !activeBuildID || phase !== 'monitor') return
    let cancelled = false
    // logsOffset is "seq > N"; seq is 1..n contiguous, so N == accumulated count.
    let offset = 0
    const buildID = activeBuildID

    async function fetchOnce() {
      try {
        const st = await templates.buildStatus(template!.templateID, buildID, 0)
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
            buildID,
            offset,
          )
          if (cancelled) return
          const batch = st.logEntries ?? []
          setBuildStatus((prev) => {
            if (!prev || offset === 0) {
              return st
            }
            if (batch.length === 0) {
              return {
                ...prev,
                status: st.status,
                reason: st.reason,
              }
            }
            const logEntries = [...(prev.logEntries ?? []), ...batch]
            return {
              ...st,
              logEntries,
              logs: logEntries.map((e) => e.message),
            }
          })
          if (batch.length > 0) {
            offset += batch.length
          }
          if (st.status === 'ready') {
            if (!notifiedDoneRef.current) {
              notifiedDoneRef.current = true
              onDoneRef.current?.()
            }
            return
          }
          if (st.status === 'error') {
            onDoneRef.current?.()
            return
          }
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
  }, [open, template?.templateID, activeBuildID, phase, watchBuild])

  useEffect(() => {
    logRef.current?.scrollTo(0, logRef.current.scrollHeight)
  }, [buildStatus?.logs, buildStatus?.logEntries])

  if (!open || !template) return null

  const showErr = localError || error
  const status = buildStatus?.status ?? template.buildStatus
  const showVersionOpts = status === 'ready'
  const logLines =
    buildStatus?.logEntries?.map((e) => e.message) ??
    buildStatus?.logs ??
    []
  const title =
    watchBuild ||
    canRetryBuild(template.buildStatus) ||
    status === 'error' ||
    status === 'ready'
      ? 'Build'
      : 'Logs'
  const displayBuildID = activeBuildID || template.buildID

  function goRetry() {
    applySpecToForm(
      buildStatus?.spec,
      {
        setBaseMode,
        setFromImage,
        setFromTemplate,
        setRunCmd,
        setStartCmd,
        setReadyCmd,
      },
      baseOptions[0] ? templateDisplayName(baseOptions[0]) : 'base',
    )
    setPhase('form')
    setWatchBuild(false)
    setLocalError(null)
    setVersionTag('')
    setAssignDefault(true)
    notifiedDoneRef.current = false
  }

  function buildSubmitLabel(): string {
    if (starting) return 'Starting…'
    if (status === 'ready') return 'Rebuild'
    if (status === 'error') return 'Retry'
    return 'Start build'
  }

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (!template) return
    setStarting(true)
    setLocalError(null)
    notifiedDoneRef.current = false

    const spec: BuildSpec & { tags?: string[]; assignDefault?: boolean } = {
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
    const tag = versionTag.trim().toLowerCase()
    if (tag) spec.tags = [tag]
    if (showVersionOpts) spec.assignDefault = assignDefault

    try {
      const started = await templates.startBuild(
        template.templateID,
        activeBuildID || template.buildID,
        spec,
      )
      const buildID = started.buildID
      setActiveBuildID(buildID)
      setPhase('monitor')
      setWatchBuild(true)
      setBuildStatus({
        templateID: template.templateID,
        buildID,
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

  return (
    <dialog className="modal modal-bottom sm:modal-middle modal-open" aria-labelledby={titleId}>
      <div className="modal-box flex max-h-[90dvh] max-w-2xl flex-col">
        <div className="flex items-start justify-between gap-3">
          <div>
            <h3 id={titleId} className="font-display text-lg font-semibold">
              {title} {templateDisplayName(template)}
            </h3>
            <p className="mt-1 font-mono text-xs opacity-50">
              {displayBuildID.slice(0, 8)}…
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
                className={`btn btn-xs min-h-11 sm:min-h-6 ${baseMode === 'image' ? 'btn-primary' : 'btn-ghost'}`}
                onClick={() => setBaseMode('image')}
              >
                From image
              </button>
              <button
                type="button"
                className={`btn btn-xs min-h-11 sm:min-h-6 ${baseMode === 'template' ? 'btn-primary' : 'btn-ghost'}`}
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

            {showVersionOpts && (
              <div className="flex flex-col gap-3 rounded-lg border border-base-300 bg-base-200/40 p-3">
                <label className="form-control w-full gap-1.5">
                  <span className="text-xs font-medium opacity-60">
                    Version tag (optional)
                  </span>
                  <input
                    className="input input-bordered input-sm w-full font-mono"
                    value={versionTag}
                    onChange={(e) => setVersionTag(e.target.value)}
                    placeholder="v2"
                  />
                  <span className="text-xs opacity-45">
                    Same config rebuilds in place. Changing base image / RUN /
                    start / ready creates a new build ID automatically. Resolve as{' '}
                    <code className="font-mono">name:tag</code>.
                  </span>
                </label>
                <label className="flex cursor-pointer items-center gap-2.5">
                  <input
                    type="checkbox"
                    className="checkbox checkbox-sm"
                    checked={assignDefault}
                    onChange={(e) => setAssignDefault(e.target.checked)}
                  />
                  <span className="text-sm">
                    Move default tag when a new build is created
                  </span>
                </label>
              </div>
            )}

            <p className="text-xs opacity-45">
              Image builds require a configured builder: docker backend, or kaniko
              with <code className="font-mono">ROUNDPEN_TEMPLATE_BUILDER=kaniko</code>{' '}
              and <code className="font-mono">ROUNDPEN_KANIKO_DESTINATION</code>. T2
              snapshot verification currently needs docker.
            </p>

            {showErr && (
              <p className="text-sm text-error" role="alert">
                {showErr}
              </p>
            )}
            {buildStatus?.reason?.message && !showErr && (
              <p className="text-sm text-error" role="alert">
                Previous build failed: {buildStatus.reason.message}
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
                {buildSubmitLabel()}
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
              className="min-h-[12rem] flex-1 overflow-auto rounded-lg border border-base-300 bg-base-200/50 p-3 font-mono text-xs leading-relaxed whitespace-pre-wrap break-all"
            >
              {logLines.length > 0 ? (
                logLines.map((line, i) => (
                  <div key={i}>
                    <AnsiText text={line} />
                  </div>
                ))
              ) : status === 'building' ? (
                'Building…'
              ) : status === 'ready' ? (
                'Build ready.'
              ) : status === 'error' ? (
                'Build failed.'
              ) : (
                'No logs yet.'
              )}
            </pre>
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
                Close
              </button>
              {(status === 'error' || status === 'ready') && (
                <button
                  type="button"
                  className="btn btn-primary btn-sm"
                  disabled={busy || starting}
                  onClick={goRetry}
                >
                  {status === 'ready' ? 'Rebuild' : 'Retry'}
                </button>
              )}
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
