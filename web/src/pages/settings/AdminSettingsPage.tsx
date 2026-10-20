import { useCallback, useEffect, useState } from 'react'
import { Navigate, useParams } from 'react-router-dom'
import { Banner, Button, Modal, Toast } from '@douyinfe/semi-ui-19'
import {
  adminSettings,
  type AppSettings,
} from '../../api'
import { useAuth } from '../../auth'
import { useT } from '../../i18n'
import { OAuthProvidersPanel } from '../../components/OAuthProvidersPanel'
import { PluginsPanel } from '../../components/PluginsPanel'
import { SETTINGS_AREAS } from '../../lib/appNav'
import { emptySettings } from './constants'
import { BrowserSection } from './BrowserSection'
import { LlmgwSection } from './LlmgwSection'
import { EnvSection } from './EnvSection'
import { ProxySection } from './ProxySection'
import { WebtoolsSection } from './WebtoolsSection'

export function AdminSettingsPage() {
  const auth = useAuth()
  const [form, setForm] = useState<AppSettings>(emptySettings)
  const [error, setError] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [dirty, setDirty] = useState(false)
  const { section: sectionParam } = useParams()
  const t = useT()

  const isAdmin = auth.status === 'ok' && auth.user.role === 'admin'
  const section = SETTINGS_AREAS.admin.resolve(sectionParam)

  const load = useCallback(async () => {
    setLoading(true)
    setError(null)
    try {
      const res = await adminSettings.get()
      setForm({ ...emptySettings, ...res.settings })
      setDirty(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('settings.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  function patch(partial: Partial<AppSettings>) {
    setForm((prev) => ({ ...prev, ...partial }))
    setDirty(true)
  }

  async function onSave() {
    setSaving(true)
    setSaveError(null)
    try {
      const res = await adminSettings.update(form)
      setForm({ ...emptySettings, ...res.settings })
      setDirty(false)
      Toast.success(t('settings.saveSuccess'))
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : t('settings.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  function onReload() {
    if (dirty) {
      Modal.confirm({
        title: t('settings.discardTitle'),
        onOk: () => {
          void load()
        },
      })
      return
    }
    void load()
  }

  if (sectionParam && sectionParam !== section) {
    return <Navigate to={`/admin/settings/${section}`} replace />
  }

  // Item-only sections save themselves; hide the document save bar.
  const itemOnly =
    section === 'browser' ||
    section === 'webtools' ||
    section === 'proxy' ||
    section === 'llmgw' ||
    section === 'env'

  return (
    <div
      style={{
        padding: '16px 12px 24px',
        maxWidth: 768,
        margin: '0 auto',
        width: '100%',
        boxSizing: 'border-box',
      }}
    >
      {error && (
        <div role="alert" style={{ marginBottom: 16 }}>
          <Banner
            fullMode={false}
            type="danger"
            description={error}
            closeIcon={null}
          />
        </div>
      )}

      {section === 'oauth' && <OAuthProvidersPanel />}

      {section === 'plugins' && <PluginsPanel />}

      {section === 'browser' && (
        <BrowserSection isAdmin={isAdmin} loading={loading} t={t} form={form} patch={patch} />
      )}

      {section === 'llmgw' && (
        <LlmgwSection isAdmin={isAdmin} loading={loading} t={t} form={form} patch={patch} />
      )}

      {section === 'webtools' && (
        <WebtoolsSection
          isAdmin={isAdmin}
          loading={loading}
          t={t}
          form={form}
          patch={patch}
        />
      )}

      {section === 'proxy' && (
        <ProxySection isAdmin={isAdmin} loading={loading} t={t} form={form} patch={patch} />
      )}

      {section === 'env' && <EnvSection isAdmin={isAdmin} t={t} />}

      {!loading && section !== 'oauth' && section !== 'plugins' && !itemOnly && (
        <div
          style={{
            marginTop: 24,
            borderTop: '1px solid var(--semi-color-border)',
            paddingTop: 16,
            display: 'flex',
            flexWrap: 'wrap',
            alignItems: 'center',
            gap: 8,
          }}
        >
          {saveError && (
            <div role="alert" style={{ flex: 1, minWidth: 160 }}>
              <Banner
                fullMode={false}
                type="danger"
                description={saveError}
                closeIcon={null}
              />
            </div>
          )}
          <div
            style={{
              display: 'flex',
              gap: 8,
              marginLeft: 'auto',
            }}
          >
            <Button
              theme="solid"
              type="primary"
              loading={saving}
              disabled={!dirty}
              onClick={() => void onSave()}
            >
              {t('settings.save')}
            </Button>
            <Button
              type="tertiary"
              disabled={loading || saving}
              onClick={onReload}
            >
              {t('settings.reload')}
            </Button>
          </div>
        </div>
      )}
    </div>
  )
}
