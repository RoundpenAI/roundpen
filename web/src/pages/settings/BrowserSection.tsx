import { useCallback, useEffect, useState } from 'react'
import { Button, Tag, Typography } from '@douyinfe/semi-ui-19'
import { adminSettings, environments, type SettingItem } from '../../api'
import { Loading } from '../../components/Loading'
import { ItemListSection } from './items/ItemListSection'
import { BrowserEditorModal } from './BrowserEditorModal'
import type { SectionBase } from './helpers'

// Browser sources are setting items (kind "browser"): each item saves on its
// own, so this section ignores the surrounding settings form and save bar.
// The active source is the list default; 「已启动」marks it when the browser
// environment is running. Add/edit uses a two-mode form (local Docker vs
// remote URL) instead of the generic item editor.
export function BrowserSection({ isAdmin, loading, t }: SectionBase) {
  const [testResult, setTestResult] = useState<Record<string, string>>({})
  const [browserRunning, setBrowserRunning] = useState(false)

  const refreshStatus = useCallback(async () => {
    try {
      const res = await environments.list()
      const browser = (res.environments ?? []).find((e) => e.slot === 'browser')
      setBrowserRunning(browser?.status === 'running')
    } catch {
      setBrowserRunning(false)
    }
  }, [])

  useEffect(() => {
    if (!isAdmin || loading) return
    void refreshStatus()
    const timer = window.setInterval(() => void refreshStatus(), 15_000)
    return () => window.clearInterval(timer)
  }, [isAdmin, loading, refreshStatus])

  if (!isAdmin) return null
  if (loading) return <Loading tip={t('settings.loadingSystem')} />

  async function test(item: SettingItem) {
    const res = await adminSettings.browserTest(item.id)
    const detail = [res.result?.endpoint, res.result?.path, res.result?.version]
      .filter(Boolean)
      .join(' · ')
    setTestResult((prev) => ({
      ...prev,
      [item.id]: res.ok ? detail || t('settings.browser.testOk') : (res.error ?? ''),
    }))
    void refreshStatus()
  }

  return (
    <ItemListSection
      kind="browser"
      introKey="settings.browser.intro"
      noteKey="settings.browser.hint"
      t={t}
      defaultSlot="browser.default"
      renderEditor={({ t: tt, item, existingIds, onSave, onClose }) => (
        <BrowserEditorModal
          t={tt}
          item={item}
          existingIds={existingIds}
          onSave={async (saved) => {
            await onSave(saved)
            void refreshStatus()
          }}
          onClose={onClose}
        />
      )}
      renderItemTags={(_item, { isDefault }) =>
        isDefault && browserRunning ? (
          <Tag color="green" size="small">
            {t('settings.browser.started')}
          </Tag>
        ) : null
      }
      renderItemAction={(item) => (
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <Button size="small" theme="borderless" onClick={() => void test(item)}>
            {t('settings.browser.test')}
          </Button>
          {testResult[item.id] && (
            <Typography.Text type="tertiary" size="small">
              {testResult[item.id]}
            </Typography.Text>
          )}
        </div>
      )}
    />
  )
}
