import { useEffect, useLayoutEffect, useRef, useState } from 'react'

export type ContextMenuItem = {
  key: string
  label: string
  onClick: () => void
  danger?: boolean
  disabled?: boolean
}

export type ContextMenuState = {
  x: number
  y: number
  items: ContextMenuItem[]
} | null

/**
 * Hand-drawn context menu: Semi's Dropdown anchors to an element, but a
 * right-click menu must open at the pointer. Closes on any outside click,
 * another right-click, scroll, resize or Escape.
 */
export function ContextMenu({
  state,
  onClose,
}: {
  state: ContextMenuState
  onClose: () => void
}) {
  const ref = useRef<HTMLDivElement | null>(null)
  const [pos, setPos] = useState({ left: -9999, top: -9999 })

  useLayoutEffect(() => {
    if (!state || !ref.current) return
    const rect = ref.current.getBoundingClientRect()
    const left = Math.max(8, Math.min(state.x, window.innerWidth - rect.width - 8))
    const top = Math.max(8, Math.min(state.y, window.innerHeight - rect.height - 8))
    setPos({ left, top })
  }, [state])

  useEffect(() => {
    if (!state) return
    const close = () => onClose()
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
    }
    // An anchored menu is opened by the click that is still bubbling out to
    // the window when this effect runs (React flushes passive effects inside
    // discrete events), so the outside-click close only arms once that same
    // task is over — otherwise the opening click closes the menu again.
    let armed = false
    const arm = window.setTimeout(() => {
      armed = true
    }, 0)
    const onOutside = () => {
      if (armed) close()
    }
    window.addEventListener('click', onOutside)
    window.addEventListener('contextmenu', onOutside, true)
    window.addEventListener('scroll', onOutside, true)
    window.addEventListener('resize', onOutside)
    window.addEventListener('keydown', onKey)
    return () => {
      window.clearTimeout(arm)
      window.removeEventListener('click', onOutside)
      window.removeEventListener('contextmenu', onOutside, true)
      window.removeEventListener('scroll', onOutside, true)
      window.removeEventListener('resize', onOutside)
      window.removeEventListener('keydown', onKey)
    }
  }, [state, onClose])

  if (!state) return null
  return (
    <div
      ref={ref}
      role="menu"
      className="rp-ws-menu"
      style={{ left: pos.left, top: pos.top }}
      onClick={(e) => e.stopPropagation()}
      onContextMenu={(e) => e.preventDefault()}
    >
      {state.items.map((item) => (
        <button
          key={item.key}
          type="button"
          role="menuitem"
          className={item.danger ? 'rp-ws-menu-item is-danger' : 'rp-ws-menu-item'}
          disabled={item.disabled}
          onClick={() => {
            onClose()
            item.onClick()
          }}
        >
          {item.label}
        </button>
      ))}
    </div>
  )
}
