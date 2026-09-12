import { useCallback, useEffect, useMemo, useState, type CSSProperties, type ReactNode } from 'react'
import { Navigate, useParams } from 'react-router-dom'
import {
  Banner,
  Button,
  Collapse,
  Form,
  Input,
  Modal,
  Select,
  Spin,
  Switch,
  Toast,
  Typography,
} from '@douyinfe/semi-ui-19'
import {
  adminSettings,
  templateDisplayName,
  templates,
  type AppSettings,
  type SettingsResponse,
  type Template,
} from '../api'
import { useAuth } from '../auth'
import { useT, type MessageKey } from '../i18n'
import { GitCredentialsPanel } from '../components/GitCredentialsPanel'
import { resolveSettingsSection } from '../lib/appNav'

const emptySettings: AppSettings = {
  allowPublicRegistration: false,
  defaultImage: 'host',
  defaultTtlSeconds: 1800,
  previewPublicUrl: '',
  previewTokenTtlSeconds: 900,
  templateBuilder: '',
  kanikoDestination: '',
  kanikoExecutor: '',
  kanikoRegistryMirrors: '',
  kanikoInsecure: false,
  kanikoSkipTlsVerify: false,
  kanikoExtraArgs: '',
  llmgwEnabled: false,
  llmgwPublicUrl: '',
  llmgwLogBodyMaxBytes: 0,
  llmgwEmbeddingModel: 'text-embedding-3-small',
  llmgwDefaultModel: '',
  llmgwOpenaiBaseUrl: '',
  llmgwOpenaiApiKey: '',
  llmgwAnthropicBaseUrl: '',
  llmgwAnthropicApiKey: '',
  llmgwVirtualKeys: '',
  cdpProvider: 'auto',
  cdpEndpoint: '',
  cdpToken: '',
  cdpPort: 9222,
}

const BUILDER_OPTIONS: { value: string; labelKey: MessageKey }[] = [
  { value: '', labelKey: 'settings.builder.disabled' },
  { value: 'kaniko', labelKey: 'settings.builder.kaniko' },
  { value: 'docker', labelKey: 'settings.builder.docker' },
  { value: 'ci', labelKey: 'settings.builder.ci' },
  { value: 'auto', labelKey: 'settings.builder.auto' },
]

const CDP_OPTIONS: { value: string; labelKey: MessageKey }[] = [
  { value: 'auto', labelKey: 'settings.cdp.auto' },
  { value: 'docker', labelKey: 'settings.cdp.docker' },
  { value: 'host', labelKey: 'settings.cdp.host' },
  { value: 'remote', labelKey: 'settings.cdp.remote' },
  { value: 'cloud', labelKey: 'settings.cdp.cloud' },
]

const SANDBOX_TTL_OPTIONS: { value: number; labelKey: MessageKey }[] = [
  { value: 600, labelKey: 'settings.ttl.10m' },
  { value: 900, labelKey: 'settings.ttl.15m' },
  { value: 1200, labelKey: 'settings.ttl.20m' },
  { value: 1800, labelKey: 'settings.ttl.30m' },
  { value: 3600, labelKey: 'settings.ttl.1h' },
  { value: 7200, labelKey: 'settings.ttl.2h' },
  { value: 14400, labelKey: 'settings.ttl.4h' },
]

const PREVIEW_TTL_OPTIONS: { value: number; labelKey: MessageKey }[] = [
  { value: 300, labelKey: 'settings.ttl.5m' },
  { value: 600, labelKey: 'settings.ttl.10m' },
  { value: 900, labelKey: 'settings.ttl.15m' },
  { value: 1800, labelKey: 'settings.ttl.30m' },
  { value: 3600, labelKey: 'settings.ttl.1h' },
]

const LOG_BODY_OPTIONS: { value: number; labelKey: MessageKey }[] = [
  { value: 0, labelKey: 'settings.logBody.off' },
  { value: -1, labelKey: 'settings.logBody.legacy' },
  { value: 4096, labelKey: 'settings.logBody.4k' },
  { value: 16384, labelKey: 'settings.logBody.16k' },
  { value: 65536, labelKey: 'settings.logBody.64k' },
  { value: 262144, labelKey: 'settings.logBody.256k' },
]

const sectionGap: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 16,
}

function optionsWithCurrentValue(
  options: { value: string; label: string }[],
  current: string,
  currentSuffix: (value: string) => string,
): { value: string; label: string }[] {
  if (options.some((o) => o.value === current)) return options
  return [...options, { value: current, label: currentSuffix(current) }]
}

function formatDurationSeconds(
  seconds: number,
  t: (key: MessageKey, vars?: Record<string, string | number>) => string,
): string {
  if (!Number.isFinite(seconds)) return String(seconds)
  if (seconds < 0) return String(seconds)
  if (seconds % 3600 === 0) {
    const h = seconds / 3600
    return h === 1 ? t('settings.duration.1h') : t('settings.duration.nh', { n: h })
  }
  if (seconds % 60 === 0) {
    const m = seconds / 60
    return m === 1 ? t('settings.duration.1m') : t('settings.duration.nm', { n: m })
  }
  return t('settings.duration.s', { n: seconds })
}

function ttlOptionsWithCurrent(
  options: { value: number; label: string }[],
  current: number,
  currentSuffix: (value: string) => string,
  t: (key: MessageKey, vars?: Record<string, string | number>) => string,
) {
  if (options.some((o) => o.value === current)) return options
  return [
    ...options,
    {
      value: current,
      label: currentSuffix(formatDurationSeconds(current, t)),
    },
  ]
}

function numberOptionsWithCurrent(
  options: { value: number; label: string }[],
  current: number,
  currentSuffix: (value: string) => string,
) {
  if (options.some((o) => o.value === current)) return options
  return [
    ...options,
    { value: current, label: currentSuffix(String(current)) },
  ]
}

function templateRef(t: Template): string {
  const name = templateDisplayName(t)
  return name.includes('/') ? (name.split('/').pop() ?? name) : name
}

function Field({
  label,
  hint,
  children,
}: {
  label: string
  hint?: string
  children: ReactNode
}) {
  return (
    <Form.Slot label={label}>
      {children}
      {hint ? (
        <Typography.Text
          type="tertiary"
          size="small"
          style={{ display: 'block', marginTop: 4 }}
        >
          {hint}
        </Typography.Text>
      ) : null}
    </Form.Slot>
  )
}

function Toggle({
  checked,
  onChange,
  children,
}: {
  checked: boolean
  onChange: (v: boolean) => void
  children: ReactNode
}) {
  return (
    <div
      style={{
        display: 'flex',
        alignItems: 'center',
        gap: 12,
        minHeight: 44,
      }}
    >
      <Switch checked={checked} onChange={onChange} />
      <Typography.Text>{children}</Typography.Text>
    </div>
  )
}

function SystemRow({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ minWidth: 0, display: 'contents' }}>
      <Typography.Text type="tertiary" size="small" component="dt">
        {label}
      </Typography.Text>
      <Typography.Text
        component="dd"
        style={{
          margin: 0,
          fontFamily: 'var(--semi-font-family-code)',
          fontSize: 12,
          wordBreak: 'break-all',
        }}
      >
        {value}
      </Typography.Text>
    </div>
  )
}

export function SettingsPage() {
  const auth = useAuth()
  const [data, setData] = useState<SettingsResponse | null>(null)
  const [form, setForm] = useState<AppSettings>(emptySettings)
  const [error, setError] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [dirty, setDirty] = useState(false)
  const [templateList, setTemplateList] = useState<Template[]>([])
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
        <Spin tip={t('settings.loading')} />
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

        {section === 'general' && (
          isAdmin && loading ? (
            <Spin tip={t('settings.loadingSystem')} />
          ) : isAdmin ? (
            <Form labelPosition="top" labelAlign="left" style={sectionGap}>
            <div style={{ ...sectionGap, paddingTop: 16 }}>
              <Toggle
                checked={form.allowPublicRegistration}
                onChange={(v) => patch({ allowPublicRegistration: v })}
              >
                {t('settings.general.allowRegistration')}
              </Toggle>
              <Field label={t('settings.general.defaultImage')}>
                {defaultImageOptions.length > 0 ? (
                  <Select
                    value={form.defaultImage}
                    onChange={(v) => patch({ defaultImage: String(v) })}
                    optionList={defaultImageOptions}
                    style={{ width: '100%' }}
                  />
                ) : (
                  <Input
                    value={form.defaultImage}
                    onChange={(v) => patch({ defaultImage: v })}
                  />
                )}
              </Field>
              <Field label={t('settings.general.defaultTtl')}>
                <Select
                  value={form.defaultTtlSeconds}
                  onChange={(v) =>
                    patch({ defaultTtlSeconds: Number(v) })
                  }
                  optionList={sandboxTtlOptions.map((o) => ({
                    value: o.value,
                    label: o.label,
                  }))}
                  style={{ width: '100%' }}
                />
              </Field>
            </div>
            </Form>
          ) : null
        )}

        {section === 'preview' && (
          isAdmin && loading ? (
            <Spin tip={t('settings.loadingSystem')} />
          ) : isAdmin ? (
            <Form labelPosition="top" labelAlign="left" style={sectionGap}>
            <div style={{ ...sectionGap, paddingTop: 16 }}>
              <Field label={t('settings.preview.publicUrl')}>
                <Input
                  inputMode="url"
                  autoComplete="url"
                  placeholder="http://127.0.0.1:19001"
                  value={form.previewPublicUrl}
                  onChange={(v) => patch({ previewPublicUrl: v })}
                />
              </Field>
              <Field label={t('settings.preview.tokenTtl')}>
                <Select
                  value={form.previewTokenTtlSeconds}
                  onChange={(v) =>
                    patch({ previewTokenTtlSeconds: Number(v) })
                  }
                  optionList={previewTtlOptions.map((o) => ({
                    value: o.value,
                    label: o.label,
                  }))}
                  style={{ width: '100%' }}
                />
              </Field>
            </div>
            </Form>
          ) : null
        )}

        {section === 'builds' && (
          isAdmin && loading ? (
            <Spin tip={t('settings.loadingSystem')} />
          ) : isAdmin ? (
            <Form labelPosition="top" labelAlign="left" style={sectionGap}>
            <div style={{ ...sectionGap, paddingTop: 16 }}>
              <Field label={t('settings.builds.engine')}>
                <Select
                  value={form.templateBuilder}
                  onChange={(v) => patch({ templateBuilder: String(v) })}
                  optionList={builderOptions.map((o) => ({
                    value: o.value,
                    label: o.label,
                  }))}
                  style={{ width: '100%' }}
                />
              </Field>
              <Field label={t('settings.builds.kanikoDest')}>
                <Input
                  spellCheck={false}
                  placeholder="registry.example/roundpen"
                  value={form.kanikoDestination}
                  onChange={(v) => patch({ kanikoDestination: v })}
                />
              </Field>
              <Field label={t('settings.builds.kanikoExecutor')}>
                <Input
                  spellCheck={false}
                  placeholder="executor"
                  value={form.kanikoExecutor}
                  onChange={(v) => patch({ kanikoExecutor: v })}
                />
              </Field>
              <Field label={t('settings.builds.kanikoMirrors')}>
                <Input
                  spellCheck={false}
                  placeholder="docker.1ms.run mirror.example"
                  value={form.kanikoRegistryMirrors}
                  onChange={(v) => patch({ kanikoRegistryMirrors: v })}
                />
              </Field>
              <Toggle
                checked={form.kanikoInsecure}
                onChange={(v) => patch({ kanikoInsecure: v })}
              >
                {t('settings.builds.kanikoInsecure')}
              </Toggle>
              <Toggle
                checked={form.kanikoSkipTlsVerify}
                onChange={(v) => patch({ kanikoSkipTlsVerify: v })}
              >
                {t('settings.builds.kanikoSkipTls')}
              </Toggle>
              <Field label={t('settings.builds.kanikoExtra')}>
                <Input
                  spellCheck={false}
                  placeholder="--snapshot-mode=redo"
                  value={form.kanikoExtraArgs}
                  onChange={(v) => patch({ kanikoExtraArgs: v })}
                />
              </Field>
            </div>
            </Form>
          ) : null
        )}

        {section === 'browser' && (
          isAdmin && loading ? (
            <Spin tip={t('settings.loadingSystem')} />
          ) : isAdmin ? (
            <Form labelPosition="top" labelAlign="left" style={sectionGap}>
            <div style={{ ...sectionGap, paddingTop: 16 }}>
              <Field label={t('settings.browser.cdpProvider')}>
                <Select
                  value={form.cdpProvider}
                  onChange={(v) => patch({ cdpProvider: String(v) })}
                  optionList={optionsWithCurrentValue(
                    CDP_OPTIONS.map((o) => ({
                      value: o.value,
                      label: t(o.labelKey),
                    })),
                    form.cdpProvider,
                    currentSuffix,
                  )}
                  style={{ width: '100%' }}
                />
              </Field>
              {(form.cdpProvider === 'remote' ||
                form.cdpProvider === 'cloud' ||
                form.cdpProvider === 'host') && (
                <Field
                  label={
                    form.cdpProvider === 'host'
                      ? t('settings.browser.hostCdpUrl')
                          : t('settings.browser.cdpEndpoint')
                  }
                >
                  <Input
                    inputMode="url"
                    autoComplete="off"
                    spellCheck={false}
                    placeholder={
                      form.cdpProvider === 'host'
                        ? 'http://127.0.0.1:9222'
                        : 'wss://browser.example/devtools/browser/…'
                    }
                    value={form.cdpEndpoint}
                    onChange={(v) => patch({ cdpEndpoint: v })}
                  />
                </Field>
              )}
              {(form.cdpProvider === 'remote' ||
                form.cdpProvider === 'cloud') && (
                <Field label={t('settings.browser.cdpToken')}>
                  <Input
                    mode="password"
                    autoComplete="new-password"
                    placeholder={t('settings.browser.keepMasked')}
                    value={form.cdpToken}
                    onChange={(v) => patch({ cdpToken: v })}
                  />
                </Field>
              )}
              {(form.cdpProvider === 'auto' ||
                form.cdpProvider === 'docker') && (
                <Field label={t('settings.browser.guestPort')}>
                  <Input
                    inputMode="numeric"
                    spellCheck={false}
                    value={String(form.cdpPort || 9222)}
                    onChange={(v) =>
                      patch({ cdpPort: Number(v) || 9222 })
                    }
                  />
                </Field>
              )}
              <Typography.Text type="tertiary" size="small">
                {t('settings.browser.hint')}
              </Typography.Text>
            </div>
            </Form>
          ) : null
        )}

        {section === 'llmgw' && (
          isAdmin && loading ? (
            <Spin tip={t('settings.loadingSystem')} />
          ) : isAdmin ? (
            <Form labelPosition="top" labelAlign="left" style={sectionGap}>
            <div style={{ ...sectionGap, paddingTop: 16 }}>
              <Typography.Text type="tertiary" size="small">
                {t('settings.llmgw.intro')}
              </Typography.Text>

              <Toggle
                checked={form.llmgwEnabled}
                onChange={(v) => patch({ llmgwEnabled: v })}
              >
                {t('settings.llmgw.enable')}
              </Toggle>

              <div style={sectionGap}>
                <Typography.Text strong size="small">
                  {t('settings.llmgw.upstreamTitle')}
                </Typography.Text>
                <Typography.Text type="tertiary" size="small">
                  {t('settings.llmgw.upstreamHint')}
                </Typography.Text>
                <div
                  style={{
                    display: 'grid',
                    gap: 16,
                    gridTemplateColumns:
                      'repeat(auto-fit, minmax(220px, 1fr))',
                  }}
                >
                  <Field
                    label={t('settings.llmgw.openaiBase')}
                    hint={t('settings.llmgw.openaiBaseHint')}
                  >
                    <Input
                      inputMode="url"
                      autoComplete="off"
                      placeholder="https://api.openai.com"
                      value={form.llmgwOpenaiBaseUrl}
                      onChange={(v) => patch({ llmgwOpenaiBaseUrl: v })}
                    />
                  </Field>
                  <Field
                    label={t('settings.llmgw.openaiKey')}
                    hint={t('settings.llmgw.keepSecret')}
                  >
                    <Input
                      mode="password"
                      autoComplete="new-password"
                      placeholder={t('settings.browser.keepMasked')}
                      value={form.llmgwOpenaiApiKey}
                      onChange={(v) => patch({ llmgwOpenaiApiKey: v })}
                    />
                  </Field>
                  <Field
                    label={t('settings.llmgw.anthropicBase')}
                    hint={t('settings.llmgw.anthropicBaseHint')}
                  >
                    <Input
                      inputMode="url"
                      autoComplete="off"
                      placeholder="https://api.anthropic.com"
                      value={form.llmgwAnthropicBaseUrl}
                      onChange={(v) => patch({ llmgwAnthropicBaseUrl: v })}
                    />
                  </Field>
                  <Field
                    label={t('settings.llmgw.anthropicKey')}
                    hint={t('settings.llmgw.keepSecret')}
                  >
                    <Input
                      mode="password"
                      autoComplete="new-password"
                      placeholder={t('settings.browser.keepMasked')}
                      value={form.llmgwAnthropicApiKey}
                      onChange={(v) => patch({ llmgwAnthropicApiKey: v })}
                    />
                  </Field>
                </div>
              </div>

              <div
                style={{
                  ...sectionGap,
                  borderTop: '1px solid var(--semi-color-border)',
                  paddingTop: 16,
                }}
              >
                <Typography.Text strong size="small">
                  {t('settings.llmgw.agentsTitle')}
                </Typography.Text>
                <Typography.Text type="tertiary" size="small">
                  {t('settings.llmgw.agentsHint')}
                </Typography.Text>
                <Field
                  label={t('settings.llmgw.publicUrl')}
                  hint={t('settings.llmgw.publicUrlHint')}
                >
                  <Input
                    inputMode="url"
                    autoComplete="url"
                    placeholder="http://127.0.0.1:9527"
                    value={form.llmgwPublicUrl}
                    onChange={(v) => patch({ llmgwPublicUrl: v })}
                  />
                </Field>
                <Field
                  label={t('settings.llmgw.defaultModel')}
                  hint={t('settings.llmgw.defaultModelHint')}
                >
                  <Input
                    spellCheck={false}
                    autoComplete="off"
                    placeholder="e.g. gpt-4o-mini or claude-sonnet-4"
                    value={form.llmgwDefaultModel}
                    onChange={(v) => patch({ llmgwDefaultModel: v })}
                  />
                </Field>
                <Field
                  label={t('settings.llmgw.virtualKeys')}
                  hint={t('settings.llmgw.virtualKeysHint')}
                >
                  <Input
                    spellCheck={false}
                    autoComplete="off"
                    placeholder="vk-dev:dev"
                    value={form.llmgwVirtualKeys}
                    onChange={(v) => patch({ llmgwVirtualKeys: v })}
                  />
                </Field>
              </div>

              <Collapse>
                <Collapse.Panel header={t('settings.llmgw.advanced')} itemKey="advanced">
                  <div style={sectionGap}>
                    <Field
                      label={t('settings.llmgw.embedModel')}
                      hint={t('settings.llmgw.embedModelHint')}
                    >
                      <Input
                        spellCheck={false}
                        placeholder="text-embedding-3-small"
                        value={form.llmgwEmbeddingModel}
                        onChange={(v) =>
                          patch({ llmgwEmbeddingModel: v })
                        }
                      />
                    </Field>
                    <Field
                      label={t('settings.llmgw.logBody')}
                      hint={t('settings.llmgw.logBodyHint')}
                    >
                      <Select
                        value={form.llmgwLogBodyMaxBytes}
                        onChange={(v) =>
                          patch({ llmgwLogBodyMaxBytes: Number(v) })
                        }
                        optionList={logBodyOptions.map((o) => ({
                          value: o.value,
                          label: o.label,
                        }))}
                        style={{ width: '100%' }}
                      />
                    </Field>
                    <Typography.Text type="tertiary" size="small">
                          {t('settings.llmgw.secretsNote')}
                        </Typography.Text>
                  </div>
                </Collapse.Panel>
              </Collapse>
            </div>
            </Form>
          ) : null
        )}

        {section === 'system' && (
          isAdmin && loading ? (
            <Spin tip={t('settings.loadingSystem')} />
          ) : isAdmin ? (
            <Form labelPosition="top" labelAlign="left" style={sectionGap}>
            <div style={{ paddingTop: 16 }}>
              {sys ? (
                <div
                  style={{
                    border: '1px solid var(--semi-color-border)',
                    borderRadius: 8,
                    padding: 16,
                  }}
                >
                  <Typography.Title heading={5} style={{ margin: '0 0 12px' }}>
                    {t('settings.system.title')}
                  </Typography.Title>
                  <dl
                    style={{
                      margin: 0,
                      display: 'grid',
                      gridTemplateColumns: '7.5rem 1fr',
                      columnGap: 16,
                      rowGap: 8,
                    }}
                  >
                    <SystemRow label={t('settings.system.backend')} value={sys.backend} />
                    <SystemRow label={t('settings.system.httpAddr')} value={sys.httpAddr} />
                    <SystemRow label={t('settings.system.dataRoot')} value={sys.dataRoot} />
                    <SystemRow label={t('settings.system.dockerHost')} value={sys.dockerHost} />
                    <SystemRow
                      label={t('settings.system.activeBuilder')}
                      value={sys.templateBuilderActive || t('settings.system.disabled')}
                    />
                    <SystemRow
                      label={t('settings.system.llmgw')}
                      value={
                        sys.llmgwActive
                          ? t('settings.system.llmgwActive')
                          : sys.llmgwMounted
                            ? t('settings.system.llmgwMounted')
                            : t('settings.system.llmgwOff')
                      }
                    />
                    <SystemRow
                      label={t('settings.system.cdpProvider')}
                      value={sys.cdpProviderActive || 'auto'}
                    />
                    <SystemRow
                      label={t('settings.system.hostChrome')}
                      value={sys.cdpHostChromeFound ? t('settings.system.hostChromeFound') : t('settings.system.hostChromeMissing')}
                    />
                  </dl>
                  {sys.templateBuilderHint && (
                    <Typography.Text
                      type="tertiary"
                      size="small"
                      style={{ display: 'block', marginTop: 12 }}
                    >
                      {sys.templateBuilderHint}
                    </Typography.Text>
                  )}
                  {sys.cdpHint && (
                    <Typography.Text
                      type="tertiary"
                      size="small"
                      style={{ display: 'block', marginTop: 12 }}
                    >
                      {sys.cdpHint}
                    </Typography.Text>
                  )}
                  <Typography.Text
                        type="tertiary"
                        size="small"
                        style={{ display: 'block', marginTop: 12 }}
                      >
                        {t('settings.system.footer')}
                      </Typography.Text>
                </div>
              ) : (
                <Typography.Text type="tertiary" size="small">
                  {t('settings.system.unavailable')}
                </Typography.Text>
              )}
            </div>
            </Form>
          ) : null
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
