import { useEffect, useState } from 'react'
import { Banner, Button, Input, Modal, Radio, RadioGroup, Typography } from '@douyinfe/semi-ui-19'
import {
  ApiError,
  environments,
  settingItems,
  type SettingItem,
} from '../../api'
import { Field } from './parts'
import type { Translate } from './helpers'

type Mode = 'docker' | 'remote'

type Props = {
  t: Translate
  item: SettingItem | null
  /** Ids already taken, so a new local/remote entry can pick a free slug. */
  existingIds: string[]
  onSave: (item: SettingItem) => Promise<void>
  onClose: () => void
}

function slugify(value: string): string {
  return value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9._]+/g, '-')
    .replace(/^[-._]+|[-._]+$/g, '')
    .slice(0, 64)
    .replace(/[-._]+$/, '')
}

function uniqueId(base: string, existing: string[]): string {
  const root = slugify(base) || 'browser'
  if (!existing.includes(root)) return root
  for (let i = 2; ; i++) {
    const id = `${root}-${i}`
    if (!existing.includes(id)) return id
  }
}

function modeOf(item: SettingItem | null): Mode {
  const provider = String(item?.config.provider ?? '')
  if (provider === 'remote' || provider === 'cloud') return 'remote'
  return 'docker'
}

function idFromEndpoint(endpoint: string): string {
  try {
    const raw = endpoint.trim()
    const withScheme = /^[a-z][a-z0-9+.-]*:\/\//i.test(raw) ? raw : `https://${raw}`
    const host = new URL(withScheme).hostname
    return slugify(host) || 'remote'
  } catch {
    return 'remote'
  }
}

/**
 * Browser source form: pick local Docker (platform installs browserless) or
 * paste a remote browserless URL. Id / name / token / port stay out of the way.
 */
export function BrowserEditorModal({ t, item, existingIds, onSave, onClose }: Props) {
  const isNew = Boolean(item) && !item?.id
  const [mode, setMode] = useState<Mode>('docker')
  const [endpoint, setEndpoint] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [setup, setSetup] = useState<ApiError['setup']>()
  // After a successful save, keep the assigned id so a failed Docker install
  // can be retried without creating a second item.
  const [savedId, setSavedId] = useState<string | null>(null)

  useEffect(() => {
    if (!item) {
      setMode('docker')
      setEndpoint('')
      setError(null)
      setSetup(undefined)
      setSavedId(null)
      return
    }
    setMode(modeOf(item))
    setEndpoint(typeof item.config.endpoint === 'string' ? item.config.endpoint : '')
    setError(null)
    setSetup(undefined)
    setSavedId(item.id || null)
  }, [item])

  const open = item !== null

  async function submit() {
    if (!item) return
    if (mode === 'remote' && !endpoint.trim()) {
      setError(t('settings.browser.urlRequired'))
      return
    }
    setBusy(true)
    setError(null)
    setSetup(undefined)
    try {
      const id =
        item.id ||
        savedId ||
        (mode === 'docker'
          ? uniqueId('local', existingIds)
          : uniqueId(idFromEndpoint(endpoint), existingIds))
      const name =
        item.name ||
        (mode === 'docker' ? t('settings.browser.mode.docker') : t('settings.browser.mode.remote'))
      const next: SettingItem = {
        ...item,
        id,
        name,
        enabled: true,
        config: {
          ...item.config,
          provider: mode,
          endpoint: mode === 'remote' ? endpoint.trim() : '',
          ...(mode === 'docker' ? { port: item.config.port ?? 3000 } : {}),
        },
      }
      await onSave(next)
      setSavedId(next.id)

      // Make it the active source and, for Docker, pull/start the managed container.
      try {
        await settingItems.setBinding('browser.default', next.id)
      } catch {
        /* binding is best-effort; the item is already saved */
      }
      if (mode === 'docker') {
        try {
          await environments.ensureBrowser()
        } catch (e) {
          if (e instanceof ApiError && e.setup?.length) {
            setError(e.message || t('settings.browser.installFailed'))
            setSetup(e.setup)
            return
          }
          setError(e instanceof Error ? e.message : t('settings.browser.installFailed'))
          return
        }
      }
      onClose()
    } catch (e) {
      setError(e instanceof Error ? e.message : t('settings.items.saveFailed'))
    } finally {
      setBusy(false)
    }
  }

  return (
    <Modal
      title={isNew ? t('settings.browser.addTitle') : t('settings.browser.editTitle')}
      visible={open}
      onCancel={() => {
        if (!busy) onClose()
      }}
      footer={null}
      maskClosable={!busy}
      closeOnEsc={!busy}
      width={480}
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        {error && <Banner type="danger" description={error} />}
        {setup && setup.length > 0 && (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
            {setup.map((step) => (
              <div key={step.title}>
                <Typography.Text strong>{step.title}</Typography.Text>
                {step.detail && (
                  <Typography.Text type="tertiary" size="small" style={{ display: 'block' }}>
                    {step.detail}
                  </Typography.Text>
                )}
                {step.command && (
                  <Typography.Text
                    size="small"
                    style={{ display: 'block', fontFamily: 'var(--semi-font-family-code)' }}
                  >
                    {step.command}
                  </Typography.Text>
                )}
              </div>
            ))}
          </div>
        )}

        <Field label={t('settings.browser.mode')} hint={t('settings.browser.modeHint')}>
          <RadioGroup
            direction="vertical"
            value={mode}
            onChange={(e) => setMode(e.target.value as Mode)}
          >
            <Radio value="docker">{t('settings.browser.mode.docker')}</Radio>
            <Radio value="remote">{t('settings.browser.mode.remote')}</Radio>
          </RadioGroup>
        </Field>

        {mode === 'docker' ? (
          <Typography.Text type="tertiary" size="small">
            {t('settings.browser.dockerHelp')}
          </Typography.Text>
        ) : (
          <Field label={t('settings.browser.url')} hint={t('settings.browser.urlHint')}>
            <Input
              autoComplete="off"
              placeholder="wss://browserless.example.com/chrome"
              value={endpoint}
              onChange={setEndpoint}
            />
          </Field>
        )}

        <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
          <Button onClick={onClose} disabled={busy}>
            {t('settings.items.cancel')}
          </Button>
          <Button theme="solid" loading={busy} onClick={() => void submit()}>
            {mode === 'docker' && isNew
              ? t('settings.browser.install')
              : t('settings.items.save')}
          </Button>
        </div>
      </div>
    </Modal>
  )
}
