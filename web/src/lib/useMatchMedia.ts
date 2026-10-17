import { useEffect, useState } from 'react'

/** Subscribe to a CSS media query; SSR / first paint use `false`. */
export function useMatchMedia(query: string): boolean {
  const [matches, setMatches] = useState(false)

  useEffect(() => {
    const mq = window.matchMedia(query)
    const sync = () => setMatches(mq.matches)
    sync()
    mq.addEventListener('change', sync)
    return () => mq.removeEventListener('change', sync)
  }, [query])

  return matches
}
