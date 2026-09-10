import {
  useLayoutEffect,
  useEffect,
  useRef,
  useState,
  type FocusEvent,
  type KeyboardEvent,
  type ReactNode,
} from 'react'

type Props = {
  value: string
  onChange: (value: string) => void
  onSend: () => void
  busy?: boolean
  disabled?: boolean
  lockInput?: boolean
  hint?: string
  placeholder?: string
  leading?: ReactNode
  onCancel?: () => void
}

const COLLAPSED_PX = 40
const EXPANDED_MIN_PX = 84
const EXPANDED_MAX_PX = 192

export function ChatComposerDock({ children }: { children: ReactNode }) {
  const ref = useRef<HTMLDivElement | null>(null)

  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    const pane = el.closest('.chat-pane')
    if (!(pane instanceof HTMLElement)) return
    const apply = () => {
      pane.style.setProperty('--chat-dock-h', `${el.offsetHeight}px`)
    }
    const ro = new ResizeObserver(apply)
    ro.observe(el)
    apply()
    return () => {
      ro.disconnect()
      pane.style.removeProperty('--chat-dock-h')
    }
  }, [])

  return (
    <div ref={ref} className="chat-composer-dock">
      {children}
    </div>
  )
}

export function ChatComposer({
  value,
  onChange,
  onSend,
  busy = false,
  disabled = false,
  lockInput,
  hint = 'Enter to send · Shift+Enter for newline',
  placeholder = 'Message the agent…',
  leading,
  onCancel,
}: Props) {
  const inputLocked = busy || (lockInput ?? disabled)
  const ref = useRef<HTMLTextAreaElement | null>(null)
  const blurTimer = useRef<number | null>(null)
  const [focused, setFocused] = useState(false)
  const [held, setHeld] = useState(false)
  const expanded = focused || held || value.trim().length > 0

  const clearBlurTimer = () => {
    if (blurTimer.current == null) return
    window.clearTimeout(blurTimer.current)
    blurTimer.current = null
  }

  useEffect(() => () => clearBlurTimer(), [])

  useEffect(() => {
    const el = ref.current
    if (!el) return
    if (!expanded) {
      el.style.height = `${COLLAPSED_PX}px`
      return
    }
    el.style.height = 'auto'
    el.style.height = `${Math.min(
      Math.max(el.scrollHeight, EXPANDED_MIN_PX),
      EXPANDED_MAX_PX,
    )}px`
  }, [value, expanded])

  const send = () => {
    if (busy || disabled || !value.trim()) return
    onSend()
  }

  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      send()
    }
  }

  const onComposerFocus = () => {
    clearBlurTimer()
    setFocused(true)
  }

  const onComposerBlur = (e: FocusEvent<HTMLDivElement>) => {
    const next = e.relatedTarget as Node | null
    if (next && e.currentTarget.contains(next)) return
    clearBlurTimer()
    blurTimer.current = window.setTimeout(() => {
      setFocused(false)
      blurTimer.current = null
    }, 200)
  }

  const onHoldStart = () => {
    clearBlurTimer()
    setHeld(true)
  }

  const onHoldEnd = () => setHeld(false)

  return (
    <div
      className={`chat-composer${expanded ? ' is-expanded' : ''}`}
      onFocusCapture={onComposerFocus}
      onBlurCapture={onComposerBlur}
      onPointerDown={onHoldStart}
      onPointerUp={onHoldEnd}
      onPointerCancel={onHoldEnd}
    >
      {expanded ? leading : null}
      <textarea
        ref={ref}
        className="chat-composer-input"
        rows={expanded ? 3 : 1}
        placeholder={placeholder}
        value={value}
        disabled={inputLocked}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={onKeyDown}
      />
      <div className="chat-composer-bar">
        <span className="chat-composer-hint">{hint}</span>
        <div className="chat-composer-actions">
          {onCancel && (
            <button
              type="button"
              className="btn btn-ghost btn-sm"
              disabled={!busy}
              onClick={onCancel}
            >
              Cancel
            </button>
          )}
          <button
            type="button"
            className="btn btn-primary btn-sm px-4"
            disabled={busy || disabled || !value.trim()}
            onClick={send}
          >
            {busy ? (
              <>
                <span className="loading loading-spinner loading-xs" />
                Working
              </>
            ) : (
              'Send'
            )}
          </button>
        </div>
      </div>
    </div>
  )
}
