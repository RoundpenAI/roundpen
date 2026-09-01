import { Fragment, type CSSProperties, type ReactNode } from 'react'

type AnsiStyle = {
  color?: string
  bold?: boolean
}

const FG: Record<number, string> = {
  30: '#6b7280',
  31: '#ef4444',
  32: '#22c55e',
  33: '#eab308',
  34: '#3b82f6',
  35: '#a855f7',
  36: '#06b6d4',
  37: '#e5e7eb',
  90: '#9ca3af',
  91: '#f87171',
  92: '#4ade80',
  93: '#facc15',
  94: '#60a5fa',
  95: '#c084fc',
  96: '#22d3ee',
  97: '#ffffff',
}

const ANSI_RE = /\u001b\[([0-9;]*)m/g

function applyCodes(style: AnsiStyle, codes: number[]): AnsiStyle {
  let next = { ...style }
  for (const code of codes) {
    if (code === 0) {
      next = {}
      continue
    }
    if (code === 1) {
      next.bold = true
      continue
    }
    if (code === 22) {
      next.bold = false
      continue
    }
    if (code === 39) {
      delete next.color
      continue
    }
    const color = FG[code]
    if (color) next.color = color
  }
  return next
}

function styleToCSS(style: AnsiStyle): CSSProperties | undefined {
  if (!style.color && !style.bold) return undefined
  return {
    color: style.color,
    fontWeight: style.bold ? 600 : undefined,
  }
}

/** Render text that may contain ANSI SGR sequences (e.g. kaniko/logrus). */
export function AnsiText({ text }: { text: string }) {
  const nodes: ReactNode[] = []
  let style: AnsiStyle = {}
  let last = 0
  let key = 0
  ANSI_RE.lastIndex = 0

  for (const match of text.matchAll(ANSI_RE)) {
    const idx = match.index ?? 0
    if (idx > last) {
      const chunk = text.slice(last, idx)
      const css = styleToCSS(style)
      nodes.push(
        css ? (
          <span key={key++} style={css}>
            {chunk}
          </span>
        ) : (
          <Fragment key={key++}>{chunk}</Fragment>
        ),
      )
    }
    const raw = match[1] === '' ? '0' : match[1]
    const codes = raw.split(';').map((n) => Number.parseInt(n, 10) || 0)
    style = applyCodes(style, codes)
    last = idx + match[0].length
  }

  if (last < text.length) {
    const chunk = text.slice(last)
    const css = styleToCSS(style)
    nodes.push(
      css ? (
        <span key={key++} style={css}>
          {chunk}
        </span>
      ) : (
        <Fragment key={key++}>{chunk}</Fragment>
      ),
    )
  }

  return <>{nodes}</>
}
