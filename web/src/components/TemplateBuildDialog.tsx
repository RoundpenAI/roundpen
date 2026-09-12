import { useEffect, useRef, useState, type FormEvent } from 'react'
import {
  Banner,
  Button,
  Checkbox,
  Input,
  Modal,
  Progress,
  Select,
  Tabs,
  TabPane,
  Tag,
  Typography,
} from '@douyinfe/semi-ui-19'
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

function statusTagColor(
  status: string,
): 'green' | 'blue' | 'red' | 'orange' | 'grey' {
  switch (status) {
    case 'ready':
      return 'green'
    case 'building':
      return 'blue'
    case 'error':
      return 'red'
    case 'waiting':
      return 'orange'
    default:
      return 'grey'
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

  const showErr = localError || error
  const status = buildStatus?.status ?? template?.buildStatus ?? ''
  const showVersionOpts = status === 'ready'
  const logLines =
    buildStatus?.logEntries?.map((e) => e.message) ??
    buildStatus?.logs ??
    []
  const title =
    watchBuild ||
    (template && canRetryBuild(template.buildStatus)) ||
    status === 'error' ||
    status === 'ready'
      ? 'Build'
      : 'Logs'
  const displayBuildID = activeBuildID || template?.buildID || ''

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
    <Modal
      title={
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, minWidth: 0 }}>
          <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {title} {template ? templateDisplayName(template) : ''}
          </span>
          <Tag color={statusTagColor(status || 'waiting')} size="small">
            {status || 'waiting'}
          </Tag>
        </div>
      }
      visible={open && template != null}
      onCancel={() => {
        if (!busy && !starting) onClose()
      }}
      footer={null}
      maskClosable={!busy && !starting}
      closeOnEsc={!busy && !starting}
      width={672}
      bodyStyle={{ maxHeight: 'min(90dvh, 800px)', overflowY: 'auto' }}
    >
      {template && (
        <>
          <Typography.Text
            type="tertiary"
            size="small"
            style={{
              display: 'block',
              marginBottom: 16,
              fontFamily: 'var(--semi-font-family-code)',
            }}
          >
            {displayBuildID.slice(0, 8)}…
          </Typography.Text>

          {status === 'building' && (
            <Progress
              percent={50}
              showInfo={false}
              style={{ marginBottom: 16 }}
            />
          )}

          {phase === 'form' ? (
            <form
              onSubmit={(e) => void submit(e)}
              style={{ display: 'flex', flexDirection: 'column', gap: 16 }}
            >
              <Tabs
                type="button"
                activeKey={baseMode}
                onChange={(key) => setBaseMode(key as BaseMode)}
              >
                <TabPane tab="From image" itemKey="image" />
                <TabPane tab="From template" itemKey="template" />
              </Tabs>

              {baseMode === 'image' ? (
                <div>
                  <Typography.Text size="small" type="tertiary">
                    Base image
                  </Typography.Text>
                  <Input
                    value={fromImage}
                    onChange={setFromImage}
                    placeholder="alpine:3.20"
                    style={{ fontFamily: 'var(--semi-font-family-code)' }}
                  />
                </div>
              ) : (
                <div>
                  <Typography.Text size="small" type="tertiary">
                    Base template
                  </Typography.Text>
                  <Select
                    value={fromTemplate}
                    onChange={(v) => setFromTemplate(String(v))}
                    optionList={baseOptions.map((tpl) => {
                      const name = templateDisplayName(tpl)
                      return { value: name, label: name }
                    })}
                    style={{ width: '100%' }}
                  />
                </div>
              )}

              <div>
                <Typography.Text size="small" type="tertiary">
                  RUN command (optional)
                </Typography.Text>
                <Input
                  value={runCmd}
                  onChange={setRunCmd}
                  placeholder="apk add --no-cache curl"
                  style={{ fontFamily: 'var(--semi-font-family-code)' }}
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
                    Start command
                  </Typography.Text>
                  <Input
                    value={startCmd}
                    onChange={setStartCmd}
                    placeholder="optional long-running cmd"
                    style={{ fontFamily: 'var(--semi-font-family-code)' }}
                  />
                </div>
                <div>
                  <Typography.Text size="small" type="tertiary">
                    Ready probe
                  </Typography.Text>
                  <Input
                    value={readyCmd}
                    onChange={setReadyCmd}
                    placeholder="waitForPort(8080)"
                    style={{ fontFamily: 'var(--semi-font-family-code)' }}
                  />
                </div>
              </div>

              {showVersionOpts && (
                <div
                  style={{
                    display: 'flex',
                    flexDirection: 'column',
                    gap: 12,
                    padding: 12,
                    borderRadius: 8,
                    border: '1px solid var(--semi-color-border)',
                    background: 'var(--semi-color-fill-0)',
                  }}
                >
                  <div>
                    <Typography.Text size="small" type="tertiary">
                      Version tag (optional)
                    </Typography.Text>
                    <Input
                      value={versionTag}
                      onChange={setVersionTag}
                      placeholder="v2"
                      style={{ fontFamily: 'var(--semi-font-family-code)' }}
                    />
                    <Typography.Text
                      type="tertiary"
                      size="small"
                      style={{ display: 'block', marginTop: 6 }}
                    >
                      Same config rebuilds in place. Changing base image / RUN /
                      start / ready creates a new build ID automatically. Resolve as{' '}
                      <Typography.Text
                        size="small"
                        style={{ fontFamily: 'var(--semi-font-family-code)' }}
                      >
                        name:tag
                      </Typography.Text>
                      .
                    </Typography.Text>
                  </div>
                  <Checkbox
                    checked={assignDefault}
                    onChange={(e) => setAssignDefault(!!e.target.checked)}
                  >
                    Move default tag when a new build is created
                  </Checkbox>
                </div>
              )}

              <Typography.Text type="tertiary" size="small">
                Image builds require a configured builder: docker backend, or kaniko
                with{' '}
                <Typography.Text
                  size="small"
                  style={{ fontFamily: 'var(--semi-font-family-code)' }}
                >
                  ROUNDPEN_TEMPLATE_BUILDER=kaniko
                </Typography.Text>{' '}
                and{' '}
                <Typography.Text
                  size="small"
                  style={{ fontFamily: 'var(--semi-font-family-code)' }}
                >
                  ROUNDPEN_KANIKO_DESTINATION
                </Typography.Text>
                . T2 snapshot verification currently needs docker.
              </Typography.Text>

              {showErr && (
                <div role="alert">
                  <Banner
                    fullMode={false}
                    type="danger"
                    description={showErr}
                    closeIcon={null}
                  />
                </div>
              )}
              {buildStatus?.reason?.message && !showErr && (
                <div role="alert">
                  <Banner
                    fullMode={false}
                    type="danger"
                    description={`Previous build failed: ${buildStatus.reason.message}`}
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
                <Button
                  type="tertiary"
                  disabled={busy || starting}
                  onClick={onClose}
                >
                  Cancel
                </Button>
                <Button
                  htmlType="submit"
                  theme="solid"
                  type="primary"
                  loading={starting}
                  disabled={busy}
                >
                  {buildSubmitLabel()}
                </Button>
              </div>
            </form>
          ) : (
            <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
              {buildStatus?.reason?.message && (
                <Banner
                  fullMode={false}
                  type="danger"
                  description={buildStatus.reason.message}
                  closeIcon={null}
                />
              )}
              <pre
                ref={logRef}
                style={{
                  minHeight: '12rem',
                  maxHeight: '40vh',
                  overflow: 'auto',
                  borderRadius: 8,
                  border: '1px solid var(--semi-color-border)',
                  background: 'var(--semi-color-fill-0)',
                  padding: 12,
                  fontFamily: 'var(--semi-font-family-code)',
                  fontSize: 12,
                  lineHeight: 1.5,
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-all',
                  margin: 0,
                }}
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
                <div role="alert">
                  <Banner
                    fullMode={false}
                    type="danger"
                    description={showErr}
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
                <Button
                  type="tertiary"
                  disabled={busy || starting}
                  onClick={onClose}
                >
                  Close
                </Button>
                {(status === 'error' || status === 'ready') && (
                  <Button
                    theme="solid"
                    type="primary"
                    disabled={busy || starting}
                    onClick={goRetry}
                  >
                    {status === 'ready' ? 'Rebuild' : 'Retry'}
                  </Button>
                )}
              </div>
            </div>
          )}
        </>
      )}
    </Modal>
  )
}
