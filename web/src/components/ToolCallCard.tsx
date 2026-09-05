import { useState } from 'react'

export type ToolCallData = {
  toolId: string
  title: string
  status: string
  kind?: string
  input?: unknown
  output?: unknown
}

type Props = {
  call: ToolCallData
}

function formatDetail(value: unknown): string {
  if (value == null) return ''
  if (typeof value === 'string') {
    const t = value.trim()
    if ((t.startsWith('{') && t.endsWith('}')) || (t.startsWith('[') && t.endsWith(']'))) {
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

function statusLabel(status: string): string {
  switch (status) {
    case 'pending':
      return 'Running'
    case 'in_progress':
      return 'Running'
    case 'completed':
      return 'Done'
    case 'failed':
      return 'Failed'
    default:
      return status || 'Tool'
  }
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

/** Collapsed-by-default tool call row, Cursor-style. */
export function ToolCallCard({ call }: Props) {
  const [open, setOpen] = useState(false)
  const running = call.status === 'pending' || call.status === 'in_progress'
  const failed = call.status === 'failed'
  const inputText = formatDetail(call.input)
  const outputText = formatDetail(call.output)
  const hasDetail = Boolean(inputText || outputText)

  return (
    <div
      className={`chat-tool ${failed ? 'chat-tool-failed' : ''} ${running ? 'chat-tool-running' : ''}`}
    >
      <button
        type="button"
        className="chat-tool-summary"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        <span className={`chat-tool-chevron ${open ? 'open' : ''}`} aria-hidden>
          ▸
        </span>
        {running ? (
          <span className="loading loading-spinner loading-xs shrink-0 opacity-70" />
        ) : failed ? (
          <span className="chat-tool-dot fail" aria-hidden />
        ) : (
          <span className="chat-tool-dot ok" aria-hidden />
        )}
        <span className="chat-tool-title text-left">
          {summaryLine(call)}
        </span>
        <span className="chat-tool-status shrink-0">{statusLabel(call.status)}</span>
      </button>
      {open && (
        <div className="chat-tool-detail">
          {!hasDetail && (
            <p className="opacity-50">No input/output details.</p>
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
