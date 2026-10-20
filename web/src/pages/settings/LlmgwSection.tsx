import { useCallback, useState } from 'react'
import { Button, Dropdown } from '@douyinfe/semi-ui-19'
import { Loading } from '../../components/Loading'
import { setupApi, type SettingItem } from '../../api'
import { ItemListSection } from './items/ItemListSection'
import { sectionGap } from './constants'
import type { SectionBase } from './helpers'

const PRESET_VENDORS = [
  { id: 'deepseek', name: 'DeepSeek' },
  { id: 'kimi', name: 'Kimi' },
  { id: 'glm', name: 'GLM' },
  { id: 'openrouter', name: 'OpenRouter' },
  { id: 'custom', name: 'Custom' },
] as const

// Providers are setting items (kind "llm"). Picking one as default binds
// agent / sysagent slots automatically. Relay is always on; body logging is
// env-only (ROUNDPEN_LLMGW_LOG_BODY_MAX_BYTES).
export function LlmgwSection({ isAdmin, loading, t }: SectionBase) {
  const [freeBusy, setFreeBusy] = useState(false)
  const [listKey, setListKey] = useState(0)

  const enableFree = useCallback(async () => {
    setFreeBusy(true)
    try {
      await setupApi.enableFreeLlm()
      setListKey((k) => k + 1)
    } finally {
      setFreeBusy(false)
    }
  }, [])

  if (!isAdmin) return null
  if (loading) return <Loading tip={t('settings.loadingSystem')} />

  return (
    <div style={{ ...sectionGap, paddingTop: 16 }}>
      <ItemListSection
        key={listKey}
        kind="llm"
        introKey="settings.llmgw.intro"
        noteKey="settings.llmgw.secretsNote"
        t={t}
        defaultSlot="llm.default"
        alsoBindSlots={['llm.agent', 'llm.sysagent']}
        bindModelFromItem
        renderAdd={(openCreate) => (
          <Dropdown
            trigger="click"
            position="bottomLeft"
            render={
              <Dropdown.Menu>
                <Dropdown.Item
                  disabled={freeBusy}
                  onClick={() => void enableFree()}
                >
                  FREE
                </Dropdown.Item>
                {PRESET_VENDORS.map((v) => (
                  <Dropdown.Item
                    key={v.id}
                    onClick={() =>
                      openCreate({
                        // Leave id empty so the editor treats this as create;
                        // ItemEditorModal fills id/name from the vendor preset.
                        name: v.id === 'custom' ? '' : v.name,
                        config:
                          v.id === 'custom'
                            ? { vendor: 'custom', protocol: 'openai' }
                            : { vendor: v.id },
                      } satisfies Partial<SettingItem>)
                    }
                  >
                    {v.name}
                  </Dropdown.Item>
                ))}
              </Dropdown.Menu>
            }
          >
            <Button theme="solid">{t('settings.items.add')}</Button>
          </Dropdown>
        )}
      />
    </div>
  )
}
