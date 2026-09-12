import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Navigate } from 'react-router-dom'
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

const controlClass =
  'input input-bordered w-full min-w-0 min-h-11 text-base sm:input-sm sm:min-h-0 sm:text-sm'
const selectClass =
  'select select-bordered w-full min-w-0 min-h-11 text-base sm:select-sm sm:min-h-0 sm:text-sm'

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
    <div className="form-control w-full min-w-0 gap-1.5">
      <span className="label-text block text-xs leading-snug opacity-60">
        {label}
      </span>
      {children}
      {hint ? (
        <p className="m-0 text-[0.7rem] leading-relaxed opacity-45">{hint}</p>
      ) : null}
    </div>
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
    <label className="flex min-h-11 cursor-pointer items-center gap-3 text-sm">
      <input
        type="checkbox"
        className="checkbox checkbox-sm shrink-0"
        checked={checked}
        onChange={(e) => onChange(e.target.checked)}
      />
      <span className="leading-snug">{children}</span>
    </label>
  )
}

function Section({ title, children }: { title: string; children: ReactNode }) {
  return (
    <section className="space-y-4">
      <h2 className="text-sm font-medium">{title}</h2>
      {children}
    </section>
  )
}

function SystemRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0 sm:contents">
      <dt className="text-[0.65rem] font-medium uppercase tracking-wide opacity-50 sm:text-xs sm:normal-case sm:tracking-normal sm:opacity-70">
        {label}
      </dt>
      <dd className="mt-0.5 break-all font-mono text-xs sm:mt-0">{value}</dd>
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
      <div className="flex h-full items-center justify-center text-sm opacity-60">
        Loading…
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
    } catch (e) {
      setSaveError(e instanceof Error ? e.message : 'save failed')
    } finally {
      setSaving(false)
    }
  }

  function onReload() {
    if (dirty && !window.confirm('Discard unsaved changes?')) return
    void load()
  }

  const sys = data?.system

  return (
    <PageShell
      subtitle={isAdmin ? 'Account and system settings' : 'Account settings'}
      current="settings"
      className="rp-settings !pb-0"
    >

      {error && (
        <div className="mb-4 text-sm text-error" role="alert">
          {error}
        </div>
      )}

      <div className="flex flex-col gap-8 pb-28 sm:pb-24">
        <RuntimePanel />
        <GitCredentialsPanel />

      {isAdmin && loading ? (
        <p className="text-sm opacity-50">Loading system settings…</p>
      ) : isAdmin ? (
        <>
          <Section title="General">
            <Toggle
              checked={form.allowPublicRegistration}
              onChange={(v) => patch({ allowPublicRegistration: v })}
            >
              Allow public registration
            </Toggle>
            <Field label="Default template / image">
              {defaultImageOptions.length > 0 ? (
                <select
                  className={selectClass}
                  value={form.defaultImage}
                  onChange={(e) => patch({ defaultImage: e.target.value })}
                >
                  {defaultImageOptions.map((o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ))}
                </select>
              ) : (
                <input
                  className={controlClass}
                  value={form.defaultImage}
                  onChange={(e) => patch({ defaultImage: e.target.value })}
                />
              )}
            </Field>
            <Field label="Default sandbox TTL">
              <select
                className={selectClass}
                value={form.defaultTtlSeconds}
                onChange={(e) =>
                  patch({ defaultTtlSeconds: Number(e.target.value) })
                }
              >
                {sandboxTtlOptions.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </Field>
          </Section>

          <Section title="Preview">
            <Field label="Public preview base URL">
              <input
                className={controlClass}
                inputMode="url"
                autoComplete="url"
                placeholder="http://127.0.0.1:19001"
                value={form.previewPublicUrl}
                onChange={(e) => patch({ previewPublicUrl: e.target.value })}
              />
            </Field>
            <Field label="Preview token TTL">
              <select
                className={selectClass}
                value={form.previewTokenTtlSeconds}
                onChange={(e) =>
                  patch({ previewTokenTtlSeconds: Number(e.target.value) })
                }
              >
                {previewTtlOptions.map((o) => (
                  <option key={o.value} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </Field>
          </Section>

          <Section title="Template builds">
            <Field label="Template build engine">
              <select
                className={selectClass}
                value={form.templateBuilder}
                onChange={(e) => patch({ templateBuilder: e.target.value })}
              >
                {builderOptions.map((o) => (
                  <option key={o.value || '__disabled'} value={o.value}>
                    {o.label}
                  </option>
                ))}
              </select>
            </Field>
            <Field label="Kaniko destination prefix">
              <input
                className={controlClass}
                spellCheck={false}
                placeholder="registry.example/roundpen"
                value={form.kanikoDestination}
                onChange={(e) => patch({ kanikoDestination: e.target.value })}
              />
            </Field>
            <Field label="Kaniko executor binary">
              <input
                className={controlClass}
                spellCheck={false}
                placeholder="executor"
                value={form.kanikoExecutor}
                onChange={(e) => patch({ kanikoExecutor: e.target.value })}
              />
            </Field>
            <Field label="Kaniko registry mirrors">
              <input
                className={controlClass}
                spellCheck={false}
                placeholder="docker.1ms.run mirror.example"
                value={form.kanikoRegistryMirrors}
                onChange={(e) =>
                  patch({ kanikoRegistryMirrors: e.target.value })
                }
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
              <input
                className={controlClass}
                spellCheck={false}
                placeholder="--snapshot-mode=redo"
                value={form.kanikoExtraArgs}
                onChange={(e) => patch({ kanikoExtraArgs: e.target.value })}
              />
            </Field>
          </Section>

          <Section title="Browser (CDP)">
            <Field label="CDP provider">
              <select
                className={selectClass}
                value={form.cdpProvider}
                onChange={(e) => patch({ cdpProvider: e.target.value })}
              >
                {optionsWithCurrentValue(CDP_OPTIONS, form.cdpProvider).map(
                  (o) => (
                    <option key={o.value} value={o.value}>
                      {o.label}
                    </option>
                  ),
                )}
              </select>
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
                <input
                  className={controlClass}
                  inputMode="url"
                  autoComplete="off"
                  spellCheck={false}
                  placeholder={
                    form.cdpProvider === 'host'
                      ? 'http://127.0.0.1:9222'
                      : 'wss://browser.example/devtools/browser/…'
                  }
                  value={form.cdpEndpoint}
                  onChange={(e) => patch({ cdpEndpoint: e.target.value })}
                />
              </Field>
            )}
            {(form.cdpProvider === 'remote' || form.cdpProvider === 'cloud') && (
              <Field label="CDP token (optional)">
                <input
                  type="password"
                  className={controlClass}
                  autoComplete="new-password"
                  placeholder="Leave masked to keep current"
                  value={form.cdpToken}
                  onChange={(e) => patch({ cdpToken: e.target.value })}
                />
              </Field>
            )}
            {(form.cdpProvider === 'auto' || form.cdpProvider === 'docker') && (
              <Field label="Guest CDP port">
                <input
                  className={controlClass}
                  inputMode="numeric"
                  spellCheck={false}
                  value={form.cdpPort || 9222}
                  onChange={(e) =>
                    patch({ cdpPort: Number(e.target.value) || 9222 })
                  }
                />
              </Field>
            )}
            <p className="text-xs leading-relaxed opacity-50">
              Browser tools attach to a DevTools websocket. NAS and compose
              should use Docker Chrome or a remote/cloud CDP — do not install
              Chrome on the NAS OS. Host Chrome is for laptop debugging only.
            </p>
          </Section>

          <Section title="LLM gateway">
            <p className="text-xs leading-relaxed opacity-55">
              Roundpen relays model calls so Agents never hold your real OpenAI /
              Anthropic keys. Configure upstream credentials below; Agents and
              Chats only receive a virtual key that calls /llmgw on this control
              plane.
            </p>

            <Toggle
              checked={form.llmgwEnabled}
              onChange={(v) => patch({ llmgwEnabled: v })}
            >
              Enable relay (required for Agent Chats &amp; memory embeddings)
            </Toggle>

            <div className="space-y-3 pt-1">
              <h3 className="text-xs font-medium tracking-wide opacity-70">
                1 · Upstream providers
              </h3>
              <p className="text-[0.7rem] leading-relaxed opacity-45">
                Where Roundpen forwards requests. These API keys stay in the
                control-plane database — they are never injected into sandboxes.
              </p>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field
                  label="OpenAI-compatible base URL"
                  hint="Official OpenAI, Azure OpenAI, or any OpenAI-compatible proxy."
                >
                  <input
                    className={controlClass}
                    inputMode="url"
                    autoComplete="off"
                    placeholder="https://api.openai.com"
                    value={form.llmgwOpenaiBaseUrl}
                    onChange={(e) =>
                      patch({ llmgwOpenaiBaseUrl: e.target.value })
                    }
                  />
                </Field>
                <Field
                  label="OpenAI-compatible API key"
                  hint="Leave masked to keep the stored secret."
                >
                  <input
                    type="password"
                    className={controlClass}
                    autoComplete="new-password"
                    placeholder="Leave masked to keep current"
                    value={form.llmgwOpenaiApiKey}
                    onChange={(e) =>
                      patch({ llmgwOpenaiApiKey: e.target.value })
                    }
                  />
                </Field>
              </div>
              <div className="grid gap-4 sm:grid-cols-2">
                <Field
                  label="Anthropic base URL"
                  hint="Optional. Leave empty if you only use OpenAI-compatible models."
                >
                  <input
                    className={controlClass}
                    inputMode="url"
                    autoComplete="off"
                    placeholder="https://api.anthropic.com"
                    value={form.llmgwAnthropicBaseUrl}
                    onChange={(e) =>
                      patch({ llmgwAnthropicBaseUrl: e.target.value })
                    }
                  />
                </Field>
                <Field
                  label="Anthropic API key"
                  hint="Leave masked to keep the stored secret."
                >
                  <input
                    type="password"
                    className={controlClass}
                    autoComplete="new-password"
                    placeholder="Leave masked to keep current"
                    value={form.llmgwAnthropicApiKey}
                    onChange={(e) =>
                      patch({ llmgwAnthropicApiKey: e.target.value })
                    }
                  />
                </Field>
              </div>
            </div>

            <div className="space-y-4 border-t border-base-300 pt-4">
              <h3 className="text-xs font-medium tracking-wide opacity-70">
                2 · What Agents use
              </h3>
              <p className="text-[0.7rem] leading-relaxed opacity-45">
                Sandboxes get OPENAI_BASE_URL / ANTHROPIC_BASE_URL pointing at
                this Roundpen, plus a virtual key as OPENAI_API_KEY.
              </p>
              <Field
                label="Control-plane public URL"
                hint="URL Agents inside sandboxes can reach (e.g. http://host.docker.internal:9527 or your LAN IP). Not the upstream OpenAI URL."
              >
                <input
                  className={controlClass}
                  inputMode="url"
                  autoComplete="url"
                  placeholder="http://127.0.0.1:9527"
                  value={form.llmgwPublicUrl}
                  onChange={(e) => patch({ llmgwPublicUrl: e.target.value })}
                />
              </Field>
              <Field
                label="Default model"
                hint="Used for both OpenAI and Anthropic relays when the request model is not in the upstream model map (and is not already an upstream target name). Leave empty to pass unknown models through."
              >
                <input
                  className={controlClass}
                  spellCheck={false}
                  autoComplete="off"
                  placeholder="e.g. gpt-4o-mini or claude-sonnet-4"
                  value={form.llmgwDefaultModel}
                  onChange={(e) => patch({ llmgwDefaultModel: e.target.value })}
                />
              </Field>
              <Field
                label="Virtual keys"
                hint="Client credentials for /llmgw. Format: vk-name:label or vk-name (comma-separated). Example: vk-dev:dev,vk-prod:prod. Agents pick a non-internal key automatically."
              >
                <input
                  className={controlClass}
                  spellCheck={false}
                  autoComplete="off"
                  placeholder="vk-dev:dev"
                  value={form.llmgwVirtualKeys}
                  onChange={(e) => patch({ llmgwVirtualKeys: e.target.value })}
                />
              </Field>
            </div>

            <details className="border-t border-base-300 pt-4">
              <summary className="cursor-pointer text-xs font-medium opacity-70">
                Advanced
              </summary>
              <div className="mt-3 space-y-4">
                <Field
                  label="Embedding model"
                  hint="Upstream model aliased as roundpen-embed for long-term memory search."
                >
                  <input
                    className={controlClass}
                    spellCheck={false}
                    placeholder="text-embedding-3-small"
                    value={form.llmgwEmbeddingModel}
                    onChange={(e) =>
                      patch({ llmgwEmbeddingModel: e.target.value })
                    }
                  />
                </Field>
                <Field
                  label="Request body logging"
                  hint="How much of each relayed request/response to store for audit. Off = metadata only."
                >
                  <select
                    className={selectClass}
                    value={form.llmgwLogBodyMaxBytes}
                    onChange={(e) =>
                      patch({ llmgwLogBodyMaxBytes: Number(e.target.value) })
                    }
                  >
                    {logBodyOptions.map((o) => (
                      <option key={o.value} value={o.value}>
                        {o.label}
                      </option>
                    ))}
                  </select>
                </Field>
                <p className="text-[0.7rem] leading-relaxed opacity-45">
                  Secrets are stored in PostgreSQL and shown masked. Leave a
                  masked field unchanged to keep the existing value. Saves apply
                  immediately — no restart.
                </p>
              </div>
            </details>
          </Section>

          {sys && (
            <section className="rounded-lg border border-base-300 p-4 text-sm">
              <h2 className="mb-3 font-medium">System (read-only)</h2>
              <dl className="space-y-3 sm:grid sm:grid-cols-[7.5rem_1fr] sm:gap-x-4 sm:gap-y-2 sm:space-y-0">
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
                <p className="mt-3 text-xs leading-relaxed opacity-55">
                  {sys.templateBuilderHint}
                </p>
              )}
              {sys.cdpHint && (
                <p className="mt-3 text-xs leading-relaxed opacity-55">
                  {sys.cdpHint}
                </p>
              )}
              <p className="mt-3 text-xs leading-relaxed opacity-45">
                Database and listen address require environment variables and a
                process restart. The default Agent engine (`ROUNDPEN_BACKEND`)
                is only a fallback — users pick QEMU, Docker, or Kern in Agent
                runtime above. Template builds and LLM gateway settings apply at
                runtime.
              </p>
            </section>
          )}
        </>
      ) : null}
      </div>

      {isAdmin && !loading && (
        <div className="fixed inset-x-0 bottom-0 z-20 border-t border-base-300 bg-base-100/95 px-4 pt-3 backdrop-blur pb-[max(0.75rem,env(safe-area-inset-bottom))]">
          <div className="mx-auto flex max-w-3xl flex-col gap-2 sm:flex-row sm:items-center">
            {saveError && (
              <p className="text-sm text-error sm:mr-auto" role="alert">
                {saveError}
              </p>
            )}
            <div className="flex gap-3 sm:ml-auto">
              <button
                type="button"
                className="btn btn-primary min-h-11 flex-1 sm:btn-sm sm:min-h-0 sm:flex-none"
                disabled={saving || !dirty}
                onClick={() => void onSave()}
              >
                {saving ? 'Saving…' : 'Save settings'}
              </button>
              <button
                type="button"
                className="btn btn-ghost min-h-11 flex-1 sm:btn-sm sm:min-h-0 sm:flex-none"
                disabled={loading || saving}
                onClick={onReload}
              >
                Reload
              </button>
            </div>
          </div>
        </div>
      )}
    </PageShell>
  )
}
