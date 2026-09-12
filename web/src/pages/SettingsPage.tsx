import { useCallback, useEffect, useMemo, useState, type CSSProperties, type ReactNode } from 'react'
import { Navigate } from 'react-router-dom'
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
  TabPane,
  Tabs,
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
import { GitCredentialsPanel } from '../components/GitCredentialsPanel'
import { RuntimePanel } from '../components/RuntimePanel'
import { PageShell } from '../components/PageShell'

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

const BUILDER_OPTIONS = [
  { value: '', label: 'Disabled (no local builds)' },
  { value: 'kaniko', label: 'Local Kaniko' },
  { value: 'docker', label: 'Local Docker' },
  { value: 'ci', label: 'Remote CI (build elsewhere)' },
  { value: 'auto', label: 'Auto (detect from backend / Kaniko config)' },
] as const

const CDP_OPTIONS = [
  {
    value: 'auto',
    label: 'Auto (Docker engine → Docker Chrome; else host Chrome if present)',
  },
  { value: 'docker', label: 'Docker Chrome (sandbox Dial to guest CDP)' },
  { value: 'host', label: 'Host Chrome / debugging port on this machine' },
  { value: 'remote', label: 'Remote CDP (Browserless or self-hosted)' },
  { value: 'cloud', label: 'Cloud browser (paste session CDP URL)' },
] as const

const SANDBOX_TTL_OPTIONS = [
  { value: 600, label: '10 minutes' },
  { value: 900, label: '15 minutes' },
  { value: 1200, label: '20 minutes' },
  { value: 1800, label: '30 minutes' },
  { value: 3600, label: '1 hour' },
  { value: 7200, label: '2 hours' },
  { value: 14400, label: '4 hours' },
] as const

const PREVIEW_TTL_OPTIONS = [
  { value: 300, label: '5 minutes' },
  { value: 600, label: '10 minutes' },
  { value: 900, label: '15 minutes' },
  { value: 1800, label: '30 minutes' },
  { value: 3600, label: '1 hour' },
] as const

const LOG_BODY_OPTIONS = [
  { value: 0, label: 'Off (default)' },
  { value: -1, label: 'Legacy default (off)' },
  { value: 4096, label: '4 KiB' },
  { value: 16384, label: '16 KiB' },
  { value: 65536, label: '64 KiB' },
  { value: 262144, label: '256 KiB' },
] as const

const sectionGap: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 16,
}

function optionsWithCurrentValue<T extends { value: string; label: string }>(
  options: readonly T[],
  current: string,
): T[] {
  if (options.some((o) => o.value === current)) return [...options]
  return [...options, { value: current, label: `${current} (current)` } as T]
}

function formatDurationSeconds(seconds: number): string {
  if (!Number.isFinite(seconds)) return String(seconds)
  if (seconds < 0) return String(seconds)
  if (seconds % 3600 === 0) {
    const h = seconds / 3600
    return h === 1 ? '1 hour' : `${h} hours`
  }
  if (seconds % 60 === 0) {
    const m = seconds / 60
    return m === 1 ? '1 minute' : `${m} minutes`
  }
  return `${seconds}s`
}

function ttlOptionsWithCurrent(
  options: readonly { value: number; label: string }[],
  current: number,
) {
  if (options.some((o) => o.value === current)) return [...options]
  return [
    ...options,
    { value: current, label: `${formatDurationSeconds(current)} (current)` },
  ]
}

function numberOptionsWithCurrent(
  options: readonly { value: number; label: string }[],
  current: number,
) {
  if (options.some((o) => o.value === current)) return [...options]
  return [
    ...options,
    { value: current, label: `${current} (current)` },
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
  const [activeTab, setActiveTab] = useState('general')

  const isAdmin = auth.status === 'ok' && auth.user.role === 'admin'

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
      setError(e instanceof Error ? e.message : 'failed to load settings')
    } finally {
      setLoading(false)
    }
  }, [isAdmin])

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
    return optionsWithCurrentValue(unique, form.defaultImage)
  }, [templateList, form.defaultImage])

  const builderOptions = useMemo(
    () => optionsWithCurrentValue(BUILDER_OPTIONS, form.templateBuilder),
    [form.templateBuilder],
  )

  const sandboxTtlOptions = useMemo(
    () => ttlOptionsWithCurrent(SANDBOX_TTL_OPTIONS, form.defaultTtlSeconds),
    [form.defaultTtlSeconds],
  )

  const previewTtlOptions = useMemo(
    () => ttlOptionsWithCurrent(PREVIEW_TTL_OPTIONS, form.previewTokenTtlSeconds),
    [form.previewTokenTtlSeconds],
  )

  const logBodyOptions = useMemo(
    () => numberOptionsWithCurrent(LOG_BODY_OPTIONS, form.llmgwLogBodyMaxBytes),
    [form.llmgwLogBodyMaxBytes],
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
        <Spin tip="Loading…" />
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
      Toast.success('Settings saved.')
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : 'save failed')
    } finally {
      setSaving(false)
    }
  }

  function onReload() {
    if (dirty) {
      Modal.confirm({
        title: 'Discard unsaved changes?',
        onOk: () => {
          void load()
        },
      })
      return
    }
    void load()
  }

  const sys = data?.system

  return (
    <PageShell
      subtitle={isAdmin ? 'Account and system settings' : 'Account settings'}
      current="settings"
      style={{ paddingBottom: isAdmin && !loading ? 96 : undefined }}
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

      <div style={{ display: 'flex', flexDirection: 'column', gap: 32 }}>
        <RuntimePanel />
        <GitCredentialsPanel />

        {isAdmin && loading ? (
          <Spin tip="Loading system settings…" />
        ) : isAdmin ? (
          <Form labelPosition="top" labelAlign="left" style={sectionGap}>
            <Tabs
              type="line"
              activeKey={activeTab}
              onChange={setActiveTab}
            >
              <TabPane tab="General" itemKey="general">
                <div style={{ ...sectionGap, paddingTop: 16 }}>
                  <Toggle
                    checked={form.allowPublicRegistration}
                    onChange={(v) => patch({ allowPublicRegistration: v })}
                  >
                    Allow public registration
                  </Toggle>
                  <Field label="Default template / image">
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
                  <Field label="Default sandbox TTL">
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
              </TabPane>

              <TabPane tab="Preview" itemKey="preview">
                <div style={{ ...sectionGap, paddingTop: 16 }}>
                  <Field label="Public preview base URL">
                    <Input
                      inputMode="url"
                      autoComplete="url"
                      placeholder="http://127.0.0.1:19001"
                      value={form.previewPublicUrl}
                      onChange={(v) => patch({ previewPublicUrl: v })}
                    />
                  </Field>
                  <Field label="Preview token TTL">
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
              </TabPane>

              <TabPane tab="Builds" itemKey="builds">
                <div style={{ ...sectionGap, paddingTop: 16 }}>
                  <Field label="Template build engine">
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
                  <Field label="Kaniko destination prefix">
                    <Input
                      spellCheck={false}
                      placeholder="registry.example/roundpen"
                      value={form.kanikoDestination}
                      onChange={(v) => patch({ kanikoDestination: v })}
                    />
                  </Field>
                  <Field label="Kaniko executor binary">
                    <Input
                      spellCheck={false}
                      placeholder="executor"
                      value={form.kanikoExecutor}
                      onChange={(v) => patch({ kanikoExecutor: v })}
                    />
                  </Field>
                  <Field label="Kaniko registry mirrors">
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
                    Kaniko insecure registry
                  </Toggle>
                  <Toggle
                    checked={form.kanikoSkipTlsVerify}
                    onChange={(v) => patch({ kanikoSkipTlsVerify: v })}
                  >
                    Kaniko skip TLS verify
                  </Toggle>
                  <Field label="Kaniko extra args">
                    <Input
                      spellCheck={false}
                      placeholder="--snapshot-mode=redo"
                      value={form.kanikoExtraArgs}
                      onChange={(v) => patch({ kanikoExtraArgs: v })}
                    />
                  </Field>
                </div>
              </TabPane>

              <TabPane tab="Browser" itemKey="browser">
                <div style={{ ...sectionGap, paddingTop: 16 }}>
                  <Field label="CDP provider">
                    <Select
                      value={form.cdpProvider}
                      onChange={(v) => patch({ cdpProvider: String(v) })}
                      optionList={optionsWithCurrentValue(
                        CDP_OPTIONS,
                        form.cdpProvider,
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
                          ? 'Host CDP URL (optional; empty starts local Chrome)'
                          : 'CDP endpoint URL'
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
                    <Field label="CDP token (optional)">
                      <Input
                        mode="password"
                        autoComplete="new-password"
                        placeholder="Leave masked to keep current"
                        value={form.cdpToken}
                        onChange={(v) => patch({ cdpToken: v })}
                      />
                    </Field>
                  )}
                  {(form.cdpProvider === 'auto' ||
                    form.cdpProvider === 'docker') && (
                    <Field label="Guest CDP port">
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
                    Browser tools attach to a DevTools websocket. NAS and compose
                    should use Docker Chrome or a remote/cloud CDP — do not install
                    Chrome on the NAS OS. Host Chrome is for laptop debugging only.
                  </Typography.Text>
                </div>
              </TabPane>

              <TabPane tab="LLM gateway" itemKey="llmgw">
                <div style={{ ...sectionGap, paddingTop: 16 }}>
                  <Typography.Text type="tertiary" size="small">
                    Roundpen relays model calls so Agents never hold your real OpenAI /
                    Anthropic keys. Configure upstream credentials below; Agents and
                    Chats only receive a virtual key that calls /llmgw on this control
                    plane.
                  </Typography.Text>

                  <Toggle
                    checked={form.llmgwEnabled}
                    onChange={(v) => patch({ llmgwEnabled: v })}
                  >
                    Enable relay (required for Agent Chats & memory embeddings)
                  </Toggle>

                  <div style={sectionGap}>
                    <Typography.Text strong size="small">
                      1 · Upstream providers
                    </Typography.Text>
                    <Typography.Text type="tertiary" size="small">
                      Where Roundpen forwards requests. These API keys stay in the
                      control-plane database — they are never injected into sandboxes.
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
                        label="OpenAI-compatible base URL"
                        hint="Official OpenAI, Azure OpenAI, or any OpenAI-compatible proxy."
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
                        label="OpenAI-compatible API key"
                        hint="Leave masked to keep the stored secret."
                      >
                        <Input
                          mode="password"
                          autoComplete="new-password"
                          placeholder="Leave masked to keep current"
                          value={form.llmgwOpenaiApiKey}
                          onChange={(v) => patch({ llmgwOpenaiApiKey: v })}
                        />
                      </Field>
                      <Field
                        label="Anthropic base URL"
                        hint="Optional. Leave empty if you only use OpenAI-compatible models."
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
                        label="Anthropic API key"
                        hint="Leave masked to keep the stored secret."
                      >
                        <Input
                          mode="password"
                          autoComplete="new-password"
                          placeholder="Leave masked to keep current"
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
                      2 · What Agents use
                    </Typography.Text>
                    <Typography.Text type="tertiary" size="small">
                      Sandboxes get OPENAI_BASE_URL / ANTHROPIC_BASE_URL pointing at
                      this Roundpen, plus a virtual key as OPENAI_API_KEY.
                    </Typography.Text>
                    <Field
                      label="Control-plane public URL"
                      hint="URL Agents inside sandboxes can reach (e.g. http://host.docker.internal:9527 or your LAN IP). Not the upstream OpenAI URL."
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
                      label="Default model"
                      hint="Used for both OpenAI and Anthropic relays when the request model is not in the upstream model map (and is not already an upstream target name). Leave empty to pass unknown models through."
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
                      label="Virtual keys"
                      hint="Client credentials for /llmgw. Format: vk-name:label or vk-name (comma-separated). Example: vk-dev:dev,vk-prod:prod. Agents pick a non-internal key automatically."
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
                    <Collapse.Panel header="Advanced" itemKey="advanced">
                      <div style={sectionGap}>
                        <Field
                          label="Embedding model"
                          hint="Upstream model aliased as roundpen-embed for long-term memory search."
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
                          label="Request body logging"
                          hint="How much of each relayed request/response to store for audit. Off = metadata only."
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
                          Secrets are stored in PostgreSQL and shown masked. Leave a
                          masked field unchanged to keep the existing value. Saves apply
                          immediately — no restart.
                        </Typography.Text>
                      </div>
                    </Collapse.Panel>
                  </Collapse>
                </div>
              </TabPane>

              <TabPane tab="System" itemKey="system">
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
                        System (read-only)
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
                        <SystemRow label="Backend" value={sys.backend} />
                        <SystemRow label="HTTP addr" value={sys.httpAddr} />
                        <SystemRow label="Data root" value={sys.dataRoot} />
                        <SystemRow label="Docker host" value={sys.dockerHost} />
                        <SystemRow
                          label="Active builder"
                          value={sys.templateBuilderActive || 'disabled'}
                        />
                        <SystemRow
                          label="LLM gateway"
                          value={
                            sys.llmgwActive
                              ? 'active'
                              : sys.llmgwMounted
                                ? 'mounted (disabled)'
                                : 'not mounted'
                          }
                        />
                        <SystemRow
                          label="CDP provider"
                          value={sys.cdpProviderActive || 'auto'}
                        />
                        <SystemRow
                          label="Host Chrome"
                          value={sys.cdpHostChromeFound ? 'found' : 'not on PATH'}
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
                        Database and listen address require environment variables and a
                        process restart. The default Agent engine (`ROUNDPEN_BACKEND`)
                        is only a fallback — users pick QEMU, Docker, or Kern in Agent
                        runtime above. Template builds and LLM gateway settings apply at
                        runtime.
                      </Typography.Text>
                    </div>
                  ) : (
                    <Typography.Text type="tertiary" size="small">
                      System info unavailable.
                    </Typography.Text>
                  )}
                </div>
              </TabPane>
            </Tabs>
          </Form>
        ) : null}
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
                Save settings
              </Button>
              <Button
                type="tertiary"
                disabled={loading || saving}
                onClick={onReload}
              >
                Reload
              </Button>
            </div>
          </div>
        </div>
      )}
    </PageShell>
  )
}
