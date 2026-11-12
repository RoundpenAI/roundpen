import { UiChromeStrip } from './UiChromeStrip'

/** @deprecated Prefer UiChromeStrip; kept for call sites that only need locale+theme. */
export function LocaleToggle({ block }: { block?: boolean }) {
  void block
  return <UiChromeStrip showFeedback={false} compact />
}
