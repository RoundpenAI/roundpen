import { useEffect, useState } from 'react'
import { Button } from '@douyinfe/semi-ui-19'
import {
  IconComment,
  IconMoon,
  IconSun,
} from '@douyinfe/semi-icons'
import { useUiPreference } from './SemiAppProvider'
import { useT } from '../i18n'
import { api } from '../api/client'
import type { UiLocale } from '../lib/uiPreference'

const FLAG: Record<UiLocale, string> = {
  zh_CN: '🇨🇳',
  en: '🇺🇸',
}

type FeedbackResp = { url?: string }

/** Compact locale / theme / feedback controls sharing uiPreference. */
export function UiChromeStrip({
  showFeedback = true,
  compact = false,
}: {
  showFeedback?: boolean
  /** Icon-only row (sidebar footer / login). */
  compact?: boolean
}) {
  const t = useT()
  const { preference, setLocale, setTheme } = useUiPreference()
  const [feedbackURL, setFeedbackURL] = useState('')

  useEffect(() => {
    if (!showFeedback) return
    let cancelled = false
    void api<FeedbackResp>('/v1/feedback-form')
      .then((res) => {
        if (!cancelled) setFeedbackURL((res.url ?? '').trim())
      })
      .catch(() => {
        if (!cancelled) setFeedbackURL('')
      })
    return () => {
      cancelled = true
    }
  }, [showFeedback])

  const dark = preference.theme === 'dark'
  const nextLocale: UiLocale = preference.locale === 'zh_CN' ? 'en' : 'zh_CN'

  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: compact ? 2 : 4,
        flexWrap: 'wrap',
        justifyContent: compact ? 'center' : 'flex-start',
      }}
    >
      <Button
        theme="borderless"
        type="tertiary"
        size="small"
        aria-label={t('nav.language')}
        title={t('nav.language')}
        onClick={() => setLocale(nextLocale)}
        style={{ minWidth: 32, padding: '4px 6px', fontSize: 16, lineHeight: 1 }}
      >
        <span aria-hidden>{FLAG[preference.locale] ?? '🌐'}</span>
      </Button>
      <Button
        theme="borderless"
        type="tertiary"
        size="small"
        icon={dark ? <IconMoon /> : <IconSun />}
        aria-label={t('nav.theme')}
        title={t('nav.theme')}
        onClick={() => setTheme(dark ? 'light' : 'dark')}
      />
      {showFeedback && feedbackURL ? (
        <Button
          theme="borderless"
          type="tertiary"
          size="small"
          icon={<IconComment />}
          aria-label={t('nav.feedback')}
          title={t('nav.feedback')}
          onClick={() => {
            window.open(feedbackURL, '_blank', 'noopener,noreferrer')
          }}
        />
      ) : null}
    </div>
  )
}
