import { useCallback, useEffect, useMemo, useState } from 'react'
import { Navigate, useParams } from 'react-router-dom'
import { Banner, Button, Modal, Toast } from '@douyinfe/semi-ui-19'
import {
  adminSettings,
  templateDisplayName,
  templates,
  type AppSettings,
  type AutoModeDefaults,
  type AutoModeSettings,
  type ProxyProfile,
  type SettingsResponse,
  type Template,
} from '../api'
import { useAuth } from '../auth'
import { useT } from '../i18n'
import { GitCredentialsPanel } from '../components/GitCredentialsPanel'
import { LinkedAccountsPanel } from '../components/LinkedAccountsPanel'
import { OAuthProvidersPanel } from '../components/OAuthProvidersPanel'
import { AgentEnvironmentPanel } from '../components/AgentEnvironmentPanel'
import { Loading } from '../components/Loading'
import { resolveSettingsSection } from '../lib/appNav'
import {
  AUTOMODE_DEFAULTS_TOKEN,
  BUILDER_OPTIONS,
  emptySettings,
  LOG_BODY_OPTIONS,
  PREVIEW_TTL_OPTIONS,
  SANDBOX_TTL_OPTIONS,
  type AutoModeListKey,
} from './settings/constants'
import {
  numberOptionsWithCurrent,
  optionsWithCurrentValue,
  templateRef,
  ttlOptionsWithCurrent,
} from './settings/helpers'
import {
  AutoModeDefaultsModal,
  AutoModeSection,
} from './settings/AutoModeSection'
import { BrowserSection } from './settings/BrowserSection'
import { BuildsSection } from './settings/BuildsSection'
import { GeneralSection } from './settings/GeneralSection'
import { LlmgwSection } from './settings/LlmgwSection'
import { PreviewSection } from './settings/PreviewSection'
import { ProxySection } from './settings/ProxySection'
import { SystemSection } from './settings/SystemSection'
import { WebtoolsSection } from './settings/WebtoolsSection'

export function SettingsPage() {
  const auth = useAuth()
  const [data, setData] = useState<SettingsResponse | null>(null)
  const [form, setForm] = useState<AppSettings>(emptySettings)
  const [error, setError] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [dirty, setDirty] = useState(false)
  const [browserTest, setBrowserTest] = useState<string>('')
  const [templateList, setTemplateList] = useState<Template[]>([])
  const [defaultsOpen, setDefaultsOpen] = useState(false)
  const [defaultsLoading, setDefaultsLoading] = useState(false)
  const [defaultsView, setDefaultsView] = useState<AutoModeDefaults | null>(null)
  const { section: sectionParam } = useParams()
  const t = useT()
  const currentSuffix = useCallback(
    (value: string) => t('settings.currentSuffix', { value }),
    [t],
  )

  const isAdmin = auth.status === 'ok' && auth.user.role === 'admin'
  const section = resolveSettingsSection(sectionParam, isAdmin)

  const load = useCallback(async () => {
    if (!isAdmin) {
      setLoading(false)
      setError(null)
      return
    }
    setLoading(true)
    setError(null)
    try {
      const [res, tpls] = await Promise.all([
        adminSettings.get(),
        templates.list().catch(() => [] as Template[]),
      ])
      setData(res)
      setForm({ ...emptySettings, ...res.settings })
      setTemplateList(tpls.filter((t) => t.buildStatus === 'ready'))
      setDirty(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : t('settings.loadFailed'))
    } finally {
      setLoading(false)
    }
  }, [isAdmin, t])

  const defaultImageOptions = useMemo(() => {
    const fromTemplates = templateList.flatMap((tpl) => {
      const ref = templateRef(tpl)
      return [
        {
          value: ref,
          label: `${templateDisplayName(tpl)} (${tpl.cpuCount}c / ${tpl.memoryMB}MiB)`,
        },
      ]
    })
    const seen = new Set<string>()
    const unique = fromTemplates.filter((o) => {
      if (seen.has(o.value)) return false
      seen.add(o.value)
      return true
    })
    return optionsWithCurrentValue(unique, form.defaultImage, currentSuffix)
  }, [templateList, form.defaultImage, currentSuffix])

  const builderOptions = useMemo(
    () =>
      optionsWithCurrentValue(
        BUILDER_OPTIONS.map((o) => ({ value: o.value, label: t(o.labelKey) })),
        form.templateBuilder,
        currentSuffix,
      ),
    [form.templateBuilder, t, currentSuffix],
  )

  const sandboxTtlOptions = useMemo(
    () =>
      ttlOptionsWithCurrent(
        SANDBOX_TTL_OPTIONS.map((o) => ({ value: o.value, label: t(o.labelKey) })),
        form.defaultTtlSeconds,
        currentSuffix,
        t,
      ),
    [form.defaultTtlSeconds, t, currentSuffix],
  )

  const previewTtlOptions = useMemo(
    () =>
      ttlOptionsWithCurrent(
        PREVIEW_TTL_OPTIONS.map((o) => ({ value: o.value, label: t(o.labelKey) })),
        form.previewTokenTtlSeconds,
        currentSuffix,
        t,
      ),
    [form.previewTokenTtlSeconds, t, currentSuffix],
  )

  const logBodyOptions = useMemo(
    () =>
      numberOptionsWithCurrent(
        LOG_BODY_OPTIONS.map((o) => ({ value: o.value, label: t(o.labelKey) })),
        form.llmgwLogBodyMaxBytes,
        currentSuffix,
      ),
    [form.llmgwLogBodyMaxBytes, t, currentSuffix],
  )

  useEffect(() => {
    void load()
  }, [load])

  if (auth.status === 'loading') {
    return (
      <div
        style={{
          display: 'flex',
          height: '100%',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <Loading tip={t('settings.loading')} />
      </div>
    )
  }
  if (auth.status === 'anon') {
    return <Navigate to="/login" replace />
  }

  function patch(partial: Partial<AppSettings>) {
    setForm((prev) => ({ ...prev, ...partial }))
    setDirty(true)
  }

  function patchProxy(index: number, next: Partial<ProxyProfile>) {
    patch({ proxies: form.proxies.map((p, i) => (i === index ? { ...p, ...next } : p)) })
  }

  function patchAutoMode(next: Partial<AutoModeSettings>) {
    patch({ autoMode: { ...form.autoMode, ...next } })
  }

  // setAutoModeList writes the custom entries back while preserving whether
  // the "$defaults" token is in effect for that list.
  function setAutoModeList(key: AutoModeListKey, custom: string[]) {
    const withDefaults = form.autoMode[key].includes(AUTOMODE_DEFAULTS_TOKEN)
    patchAutoMode({
      [key]: withDefaults ? [...custom, AUTOMODE_DEFAULTS_TOKEN] : custom,
    } as Partial<AutoModeSettings>)
  }

  async function openDefaults() {
    setDefaultsOpen(true)
    if (defaultsView) return
    setDefaultsLoading(true)
    try {
      setDefaultsView(await adminSettings.automodeDefaults())
    } catch {
      setDefaultsView(null)
    } finally {
      setDefaultsLoading(false)
    }
  }

  async function onSave() {
    setSaving(true)
    setSaveError(null)
    try {
      const res = await adminSettings.update(form)
      setData(res)
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

  async function testBrowser() {
    setBrowserTest('…')
    try {
      const res = await adminSettings.browserTest()
      if (!res.ok) {
        setBrowserTest(res.error || 'connection failed')
        return
      }
      const r = res.result
      setBrowserTest(
        r?.provider === 'host'
          ? `host chrome: ${r.chromePath || 'not found'}`
          : `provider=${r?.provider}${r?.version ? ` browserless=${r.version}` : ''}${r?.path ? ` path=${r.path}` : ''}`,
      )
    } catch (e) {
      setBrowserTest(e instanceof Error ? e.message : 'connection failed')
    }
  }

  const sys = data?.system

  if (sectionParam && sectionParam !== section) {
    return <Navigate to={`/settings/${section}`} replace />
  }

  return (
    <>
      <div
        style={{
          padding: '16px 12px 96px',
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

        {section === 'git' && <GitCredentialsPanel />}

        {section === 'accounts' && <LinkedAccountsPanel />}

        {section === 'oauth' && isAdmin && <OAuthProvidersPanel />}

        {section === 'agent' && <AgentEnvironmentPanel />}

        {section === 'general' && (
          <GeneralSection
            isAdmin={isAdmin}
            loading={loading}
            t={t}
            form={form}
            patch={patch}
            defaultImageOptions={defaultImageOptions}
            sandboxTtlOptions={sandboxTtlOptions}
          />
        )}

        {section === 'preview' && (
          <PreviewSection
            isAdmin={isAdmin}
            loading={loading}
            t={t}
            form={form}
            patch={patch}
            previewTtlOptions={previewTtlOptions}
          />
        )}

        {section === 'builds' && (
          <BuildsSection
            isAdmin={isAdmin}
            loading={loading}
            t={t}
            form={form}
            patch={patch}
            builderOptions={builderOptions}
          />
        )}

        {section === 'browser' && (
          <BrowserSection
            isAdmin={isAdmin}
            loading={loading}
            t={t}
            form={form}
            patch={patch}
            currentSuffix={currentSuffix}
            browserTest={browserTest}
            testBrowser={testBrowser}
          />
        )}

        {section === 'llmgw' && (
          <LlmgwSection
            isAdmin={isAdmin}
            loading={loading}
            t={t}
            form={form}
            patch={patch}
            logBodyOptions={logBodyOptions}
          />
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

        {section === 'automode' && (
          <AutoModeSection
            isAdmin={isAdmin}
            loading={loading}
            t={t}
            form={form}
            patch={patch}
            patchAutoMode={patchAutoMode}
            setAutoModeList={setAutoModeList}
            openDefaults={openDefaults}
          />
        )}

        <AutoModeDefaultsModal
          t={t}
          defaultsOpen={defaultsOpen}
          setDefaultsOpen={setDefaultsOpen}
          defaultsLoading={defaultsLoading}
          defaultsView={defaultsView}
        />

        {section === 'proxy' && (
          <ProxySection
            isAdmin={isAdmin}
            loading={loading}
            t={t}
            form={form}
            patch={patch}
            patchProxy={patchProxy}
          />
        )}

        {section === 'system' && (
          <SystemSection isAdmin={isAdmin} loading={loading} t={t} sys={sys} />
        )}
      </div>

      {isAdmin && !loading && (
        <div
          style={{
            position: 'fixed',
            left: 0,
            right: 0,
            bottom: 0,
            zIndex: 20,
            borderTop: '1px solid var(--semi-color-border)',
            background: 'var(--semi-color-bg-1)',
            padding:
              '12px 16px max(12px, env(safe-area-inset-bottom))',
          }}
        >
          <div
            style={{
              margin: '0 auto',
              maxWidth: 768,
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
        </div>
      )}
    </>
  )
}
