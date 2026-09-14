import { useEffect, useState } from 'react'
import { Spin } from '@douyinfe/semi-ui-19'
import { formatGroupStats } from '../lib/toolStats'

export type ToolCallData = {
  toolId: string
  title: string
  status: string
  kind?: string
  input?: unknown
  output?: unknown
}

type CardProps = {
  call: ToolCallData
}

function formatDetail(value: unknown): string {
  if (value == null) return ''
  if (typeof value === 'string') {
    const t = value.trim()
    if (
      (t.startsWith('{') && t.endsWith('}')) ||
      (t.startsWith('[') && t.endsWith(']'))
    ) {
      try {
        return JSON.stringify(JSON.parse(t), null, 2)
      } catch {
        return value
      }
    }
    return value
  }
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

function isRunning(status: string): boolean {
  return status === 'pending' || status === 'in_progress'
}

function clip(value: string, max = 48): string {
  return value.length > max ? `${value.slice(0, max)}…` : value
}

function summaryLine(call: ToolCallData): string {
  const title = call.title || call.toolId || 'tool'
  if (call.input && typeof call.input === 'object' && call.input !== null) {
    const entries = Object.entries(call.input as Record<string, unknown>)
    if (entries.length === 1) {
      const [k, v] = entries[0]
      let raw = ''
      if (typeof v === 'string') raw = v
      else {
        try {
          raw = JSON.stringify(v)
        } catch {
          raw = String(v)
        }
      }
      const short = clip(raw)
      if (short && short !== '{}' && short !== 'null') {
        return `${title} · ${k}=${short}`
      }
    }
  }
  return title
}

function tally(calls: ToolCallData[]) {
  let running = 0
  let failed = 0
  for (const c of calls) {
    if (isRunning(c.status)) running += 1
    else if (c.status === 'failed') failed += 1
  }
  return { running, failed }
}

export function ToolCallCard({ call }: CardProps) {
  const [open, setOpen] = useState(false)
  const running = isRunning(call.status)
  const failed = call.status === 'failed'
  const inputText = formatDetail(call.input)
  const outputText = formatDetail(call.output)
  const hasDetail = Boolean(inputText || outputText)

  return (
    <div className="chat-tool">
      <button
        type="button"
        className="chat-tool-summary"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        {running ? (
          <Spin size="small" />
        ) : (
          <span
            className={`chat-tool-dot ${failed ? 'fail' : 'ok'}`}
            aria-hidden
          />
        )}
        <span className="chat-tool-title">{summaryLine(call)}</span>
      </button>
      {open && (
        <div className="chat-tool-detail">
          {!hasDetail && (
            <p className="chat-tool-empty">No input/output details.</p>
          )}
          {inputText && (
            <div>
              <div className="chat-tool-detail-label">Input</div>
              <pre className="chat-tool-pre">{inputText}</pre>
            </div>
          )}
          {outputText && (
            <div>
              <div className="chat-tool-detail-label">Output</div>
              <pre className="chat-tool-pre">{outputText}</pre>
            </div>
          )}
        </div>
      )}
    </div>
  )
}

type GroupProps = {
  calls: ToolCallData[]
  alwaysStats?: boolean
}

export function ToolCallGroup({ calls, alwaysStats = false }: GroupProps) {
  const [open, setOpen] = useState(false)
  if (calls.length === 1 && !alwaysStats) {
    return <ToolCallCard call={calls[0]} />
  }

  const { running, failed } = tally(calls)
  const stats = formatGroupStats(calls)

  return (
    <div className="chat-tool-group">
      <button
        type="button"
        className="chat-tool-group-summary"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span className={`chat-tool-chevron ${open ? 'open' : ''}`} aria-hidden>
          ▸
        </span>
        {running > 0 ? (
          <Spin size="small" />
        ) : (
          <span
            className={`chat-tool-dot ${failed > 0 ? 'fail' : 'ok'}`}
            aria-hidden
          />
        )}
        <span className="chat-tool-title">
          {stats.label}
          {stats.plus > 0 ? (
            <span className="chat-tool-plus"> +{stats.plus}</span>
          ) : null}
          {stats.minus > 0 ? (
            <span className="chat-tool-minus"> -{stats.minus}</span>
          ) : null}
        </span>
      </button>
      {open && (
        <div className="chat-tool-group-list">
          {calls.map((call) => (
            <ToolCallCard key={call.toolId} call={call} />
          ))}
        </div>
      )}
    </div>
  )
}

const THOUGHT_PREVIEW_MAX = 44

/** "当前在干嘛" one-liner: the reasoning after the last sentence break. */
function thoughtPreview(text: string): string {
  const t = (text ?? '').trim().replace(/\s+/g, ' ')
  if (!t) return ''
  const parts = t
    .split(/[。.!?！？]/)
    .map((s) => s.trim())
    .filter(Boolean)
  const tail = (parts.length ? parts[parts.length - 1] : '') || t
  return tail.length > THOUGHT_PREVIEW_MAX
    ? `${tail.slice(0, THOUGHT_PREVIEW_MAX)}…`
    : tail
}

type ThoughtProps = {
  text: string
  streaming?: boolean
  startedAtMs?: number
}

export function ThoughtBlock({ text, streaming, startedAtMs }: ThoughtProps) {
  const [open, setOpen] = useState(false)
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    if (!streaming) return
    const timer = window.setInterval(() => setNow(Date.now()), 1000)
    return () => window.clearInterval(timer)
  }, [streaming])

  const streamingSince =
    streaming && startedAtMs
      ? Math.max(0, Math.round((now - startedAtMs) / 1000))
      : null
  const label =
    streamingSince != null
      ? streamingSince < 1
        ? '思考中…'
        : `思考 ${streamingSince}s`
      : '思考'
  const preview = thoughtPreview(text)

  return (
    <div className="chat-thought">
      <button
        type="button"
        className="chat-thought-summary"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span className={`chat-tool-chevron ${open ? 'open' : ''}`} aria-hidden>
          ▸
        </span>
        {streaming ? <Spin size="small" /> : null}
        <span className="chat-thought-label">
          {label}
          {preview ? (
            <span className="chat-thought-preview"> · {preview}</span>
          ) : null}
        </span>
      </button>
      {open && <div className="chat-thought-body">{text}</div>}
    </div>
  )
}
