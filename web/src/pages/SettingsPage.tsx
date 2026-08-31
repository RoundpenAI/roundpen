import { useCallback, useEffect, useMemo, useState, type ReactNode } from 'react'
import { Link, Navigate, useNavigate } from 'react-router-dom'
import {
  adminSettings,
  templateDisplayName,
  templates,
  type AppSettings,
  type SettingsResponse,
  type Template,
} from '../api'
import { doLogout, useAuth } from '../auth'

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
  llmgwLogBodyMaxBytes: -1,
  llmgwEmbeddingModel: 'text-embedding-3-small',
  llmgwOpenaiBaseUrl: '',
  llmgwOpenaiApiKey: '',
  llmgwAnthropicBaseUrl: '',
  llmgwAnthropicApiKey: '',
  llmgwVirtualKeys: '',
}

const BUILDER_OPTIONS = [
  { value: 'auto', label: 'Auto (detect from backend / Kaniko config)' },
  { value: 'docker', label: 'Docker' },
  { value: 'kaniko', label: 'Kaniko' },
  { value: '', label: 'Disabled' },
] as const

const SANDBOX_TTL_OPTIONS = [
  { value: 900, label: '15 minutes' },
  { value: 1800, label: '30 minutes' },
  { value: 3600, label: '1 hour' },
  { value: 7200, label: '2 hours' },
  { value: 14400, label: '4 hours' },
] as const

const PREVIEW_TTL_OPTIONS = [
  { value: 300, label: '5 minutes' },
  { value: 900, label: '15 minutes' },
  { value: 1800, label: '30 minutes' },
  { value: 3600, label: '1 hour' },
] as const

const LOG_BODY_OPTIONS = [
  { value: -1, label: 'Default (64 KiB)' },
  { value: 0, label: 'Off' },
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

function ttlOptionsWithCurrent(
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

function Field({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="form-control w-full min-w-0 gap-1.5">
      <span className="label-text text-xs leading-snug opacity-60">{label}</span>
      {children}
    </label>
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
  const navigate = useNavigate()
  const [data, setData] = useState<SettingsResponse | null>(null)
  const [form, setForm] = useState<AppSettings>(emptySettings)
  const [error, setError] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [dirty, setDirty] = useState(false)
  const [templateList, setTemplateList] = useState<Template[]>([])

  const load = useCallback(async () => {
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
  }, [])

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
    () => ttlOptionsWithCurrent(LOG_BODY_OPTIONS, form.llmgwLogBodyMaxBytes),
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
  if (auth.user.role !== 'admin') {
    return <Navigate to="/" replace />
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

  const user = auth.user
  const sys = data?.system

  return (
    <div className="rp-settings mx-auto flex min-h-full max-w-3xl flex-col px-4 pt-6 sm:py-8">
      <header className="mb-6 flex flex-wrap items-end justify-between gap-3">
        <div className="min-w-0">
          <p className="font-display text-2xl font-semibold tracking-tight">
            Roundpen
          </p>
          <p className="mt-1 text-sm opacity-55">System settings</p>
        </div>
        <div className="flex min-w-0 items-center gap-2 text-sm">
          <span className="max-w-[40vw] truncate opacity-60 sm:max-w-[12rem]">
            {user.username}
          </span>
          <button
            type="button"
            className="btn btn-ghost btn-sm shrink-0"
            onClick={() => void doLogout().then(() => navigate('/login'))}
          >
            Sign out
          </button>
        </div>
      </header>

      <nav className="mb-6 flex flex-wrap gap-x-4 gap-y-2 border-b border-base-300 pb-4 text-sm">
        <Link to="/" className="link link-hover opacity-55">
          Sandboxes
        </Link>
        <Link to="/registry" className="link link-hover opacity-55">
          Templates
        </Link>
        <span className="font-medium">Settings</span>
      </nav>

      {error && (
        <div className="mb-4 text-sm text-error" role="alert">
          {error}
        </div>
      )}

      {loading ? (
        <p className="text-sm opacity-50">Loading…</p>
      ) : (
        <div className="flex flex-col gap-8 pb-28 sm:pb-24">
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

          <Section title="LLM gateway">
            <Toggle
              checked={form.llmgwEnabled}
              onChange={(v) => patch({ llmgwEnabled: v })}
            >
              Enable LLM gateway relay
            </Toggle>
            <Field label="Public base URL (for setup docs)">
              <input
                className={controlClass}
                inputMode="url"
                autoComplete="url"
                placeholder="http://127.0.0.1:9527"
                value={form.llmgwPublicUrl}
                onChange={(e) => patch({ llmgwPublicUrl: e.target.value })}
              />
            </Field>
            <Field label="Log body max bytes">
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
            <Field label="Embedding model (upstream)">
              <input
                className={controlClass}
                spellCheck={false}
                placeholder="text-embedding-3-small"
                value={form.llmgwEmbeddingModel}
                onChange={(e) => patch({ llmgwEmbeddingModel: e.target.value })}
              />
            </Field>
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="OpenAI base URL">
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
              <Field label="OpenAI API key">
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
              <Field label="Anthropic base URL">
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
              <Field label="Anthropic API key">
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
            <Field label="Virtual keys (vk-dev:dev,vk-prod)">
              <input
                className={controlClass}
                spellCheck={false}
                autoComplete="off"
                placeholder="vk-dev:dev"
                value={form.llmgwVirtualKeys}
                onChange={(e) => patch({ llmgwVirtualKeys: e.target.value })}
              />
            </Field>
            <p className="text-xs leading-relaxed opacity-50">
              API keys are stored in the database and shown masked. Leave a
              masked field untouched to keep the existing secret. Changes apply
              immediately without restart.
            </p>
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
              </dl>
              {sys.templateBuilderHint && (
                <p className="mt-3 text-xs leading-relaxed opacity-55">
                  {sys.templateBuilderHint}
                </p>
              )}
              <p className="mt-3 text-xs leading-relaxed opacity-45">
                Backend, database, and listen address require environment
                variables and a process restart. Template builds and LLM gateway
                settings above apply at runtime.
              </p>
            </section>
          )}
        </div>
      )}

      {!loading && (
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
    </div>
  )
}
