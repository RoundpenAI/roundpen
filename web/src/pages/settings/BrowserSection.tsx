import { useState } from 'react'
import { Button, Typography } from '@douyinfe/semi-ui-19'
import { adminSettings, type SettingItem } from '../../api'
import { Loading } from '../../components/Loading'
import { ItemListSection } from './items/ItemListSection'
import { SlotBindings } from './items/SlotBindings'
import type { SectionBase } from './helpers'

// Browser sources are setting items (kind "browser"): each item saves on its
// own, so this section ignores the surrounding settings form and save bar.
export function BrowserSection({ isAdmin, loading, t }: SectionBase) {
  const [testResult, setTestResult] = useState<Record<string, string>>({})

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
  }

  return (
    <>
      <ItemListSection
        kind="browser"
        introKey="settings.browser.intro"
        noteKey="settings.browser.hint"
        t={t}
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
      <SlotBindings kind="browser" t={t} />
    </>
  )
}
