import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  Banner,
  Button,
  List,
  Modal,
  Steps,
  Toast,
  Typography,
} from '@douyinfe/semi-ui-19'
import {
  ApiError,
  environments,
  settingItems,
  setupApi,
  workspaceRoot,
  type LlmReady,
  type SettingItem,
} from '../api'
import { isFnOSApp } from '../api/client'
import { useAuth } from '../auth'
import { useT } from '../i18n'
import { pickUserDirectory } from '../lib/fnosPickFolder'
import { isOnboardingDone, markOnboardingDone } from '../lib/onboarding'
import { LlmSetupChooser } from './LlmSetupChooser'

type StepKey = 'llm' | 'folders' | 'browser'

const STEPS: StepKey[] = ['llm', 'folders', 'browser']

/**
 * First-run guide: model → folder grants → browser. Shown once per user in
 * this browser until they finish or dismiss.
 */
export function OnboardingWizard() {
  const auth = useAuth()
  const t = useT()
  const username = auth.status === 'ok' ? auth.user.username : ''
  const isAdmin = auth.status === 'ok' && auth.user.role === 'admin'
  const fnos = isFnOSApp()

  const [open, setOpen] = useState(false)
  const [step, setStep] = useState<StepKey>('llm')
  const [llm, setLlm] = useState<LlmReady | null>(null)
  const [foldersEnabled, setFoldersEnabled] = useState(false)
  const [folders, setFolders] = useState<{ path: string; displayPath: string }[]>(
    [],
  )
  const [workspaceBound, setWorkspaceBound] = useState(false)
  const [workspaceLabel, setWorkspaceLabel] = useState('')
  const [browserReady, setBrowserReady] = useState(false)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const dismiss = useCallback(() => {
    if (username) markOnboardingDone(username)
    setOpen(false)
  }, [username])

  const refreshFolders = useCallback(async () => {
    const [acc, root] = await Promise.all([
      workspaceRoot.accessibleFolders().catch(() => null),
      workspaceRoot.get().catch(() => null),
    ])
    if (acc) {
      setFoldersEnabled(Boolean(acc.enabled))
      setFolders(acc.folders ?? [])
    }
    if (root) {
      setWorkspaceBound(Boolean(root.bound))
      setWorkspaceLabel(root.displayPath || '')
    }
  }, [])

  const refreshBrowser = useCallback(async () => {
    if (!isAdmin) {
      setBrowserReady(false)
      return
    }
    try {
      const res = await settingItems.list('browser')
      const items = (res.items ?? []).filter((it) => it.enabled)
      setBrowserReady(items.length > 0)
    } catch {
      setBrowserReady(false)
    }
  }, [isAdmin])

  const refreshLlm = useCallback(async () => {
    try {
      const res = await setupApi.llmReady()
      setLlm(res)
    } catch {
      setLlm({ ready: false, mode: 'none', freeAvailable: true })
    }
  }, [])

  useEffect(() => {
    if (auth.status !== 'ok' || !username) return
    if (isOnboardingDone(username)) return
    let alive = true
    void (async () => {
      await Promise.all([refreshLlm(), refreshFolders(), refreshBrowser()])
      if (!alive) return
      setOpen(true)
    })()
    return () => {
      alive = false
    }
  }, [
    auth.status,
    username,
    refreshLlm,
    refreshFolders,
    refreshBrowser,
  ])

  const stepIndex = STEPS.indexOf(step)

  const llmDone = Boolean(llm?.ready)
  const foldersDone =
    !fnos ||
    !foldersEnabled ||
    folders.length > 0 ||
    workspaceBound
  const browserDone = !isAdmin || browserReady

  const pickFolder = async () => {
    setBusy(true)
    setError(null)
    try {
      const path = await pickUserDirectory(t('onboarding.folders.pickTitle'))
      if (!path) return
      try {
        await workspaceRoot.set(path)
        Toast.success(t('onboarding.folders.bound'))
      } catch (e) {
        // Authorization from the picker may still succeed even if bind fails.
        Toast.warning(
          e instanceof Error ? e.message : t('onboarding.folders.bindFailed'),
        )
      }
      await refreshFolders()
    } catch (e) {
      const raw = e instanceof Error ? e.message : String(e)
      if (raw === 'auth_timeout') {
        setError(t('onboarding.folders.timeout'))
      } else if (raw === 'access_denied') {
        setError(t('onboarding.folders.denied'))
      } else {
        setError(raw)
      }
    } finally {
      setBusy(false)
    }
  }

  const enableLocalBrowser = async () => {
    if (!isAdmin || busy) return
    setBusy(true)
    setError(null)
    try {
      const existing = await settingItems.list('browser')
      const ids = (existing.items ?? []).map((it) => it.id)
      let item: SettingItem | undefined = (existing.items ?? []).find(
        (it) => it.config.provider === 'docker' || !it.config.provider,
      )
      if (!item) {
        const id = ids.includes('local') ? `local-${Date.now()}` : 'local'
        const saved = await settingItems.save({
          kind: 'browser',
          id,
          name: t('settings.browser.mode.docker'),
          enabled: true,
          position: 0,
          config: { provider: 'docker', port: 3000, endpoint: '' },
        })
        item = saved.item
      }
      await settingItems.setBinding('browser.default', item.id)
      try {
        await environments.ensureBrowser()
      } catch (e) {
        if (e instanceof ApiError && e.setup?.length) {
          setError(
            [e.message, ...e.setup.map((s) => s.detail || s.title)]
              .filter(Boolean)
              .join(' · ') || t('settings.browser.installFailed'),
          )
          await refreshBrowser()
          return
        }
        throw e
      }
      Toast.success(t('onboarding.browser.enabled'))
      await refreshBrowser()
    } catch (e) {
      setError(
        e instanceof Error ? e.message : t('settings.browser.installFailed'),
      )
    } finally {
      setBusy(false)
    }
  }

  const goNext = () => {
    const i = STEPS.indexOf(step)
    if (i < STEPS.length - 1) setStep(STEPS[i + 1]!)
    else dismiss()
  }

  if (!open) return null

  return (
    <Modal
      title={t('onboarding.title')}
      visible={open}
      onCancel={dismiss}
      maskClosable={false}
      width={560}
      footer={
        <div
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            gap: 8,
            flexWrap: 'wrap',
          }}
        >
          <Button type="tertiary" onClick={dismiss}>
            {t('onboarding.later')}
          </Button>
          <div style={{ display: 'flex', gap: 8 }}>
            {stepIndex > 0 ? (
              <Button onClick={() => setStep(STEPS[stepIndex - 1]!)}>
                {t('onboarding.back')}
              </Button>
            ) : null}
            <Button theme="solid" type="primary" onClick={goNext}>
              {stepIndex >= STEPS.length - 1
                ? t('onboarding.finish')
                : t('onboarding.next')}
            </Button>
          </div>
        </div>
      }
    >
      <div style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
        <Typography.Text type="secondary">
          {t('onboarding.subtitle')}
        </Typography.Text>
        <Steps type="basic" current={stepIndex} size="small">
          <Steps.Step
            title={t('onboarding.step.llm')}
            status={llmDone ? 'finish' : step === 'llm' ? 'process' : 'wait'}
          />
          <Steps.Step
            title={t('onboarding.step.folders')}
            status={
              foldersDone ? 'finish' : step === 'folders' ? 'process' : 'wait'
            }
          />
          <Steps.Step
            title={t('onboarding.step.browser')}
            status={
              browserDone ? 'finish' : step === 'browser' ? 'process' : 'wait'
            }
          />
        </Steps>

        {error ? (
          <Banner
            fullMode={false}
            type="danger"
            description={error}
            closeIcon={null}
          />
        ) : null}

        {step === 'llm' ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <LlmSetupChooser
              status={llm}
              isAdmin={isAdmin}
              onReady={(s) => {
                setLlm(s)
              }}
            />
            {isAdmin ? (
              <Typography.Text type="tertiary" size="small">
                {t('onboarding.llm.byokHint')}{' '}
                <Link to="/admin/settings/llmgw" onClick={dismiss}>
                  {t('onboarding.llm.openSettings')}
                </Link>
              </Typography.Text>
            ) : null}
          </div>
        ) : null}

        {step === 'folders' ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <Typography.Text>{t('onboarding.folders.intro')}</Typography.Text>
            {!fnos ? (
              <Typography.Text type="tertiary" size="small">
                {t('onboarding.folders.notFnos')}
              </Typography.Text>
            ) : !foldersEnabled ? (
              <Typography.Text type="tertiary" size="small">
                {t('onboarding.folders.unavailable')}
              </Typography.Text>
            ) : (
              <>
                {workspaceBound ? (
                  <Banner
                    fullMode={false}
                    type="success"
                    closeIcon={null}
                    description={t('onboarding.folders.workspace', {
                      path: workspaceLabel || '—',
                    })}
                  />
                ) : null}
                <List
                  size="small"
                  dataSource={folders}
                  emptyContent={
                    <Typography.Text type="tertiary" size="small">
                      {t('onboarding.folders.empty')}
                    </Typography.Text>
                  }
                  renderItem={(f) => (
                    <List.Item
                      main={
                        <Typography.Text ellipsis={{ showTooltip: true }}>
                          {f.displayPath || f.path}
                        </Typography.Text>
                      }
                    />
                  )}
                />
                <Button
                  theme="solid"
                  type="primary"
                  loading={busy}
                  onClick={() => void pickFolder()}
                >
                  {t('onboarding.folders.pick')}
                </Button>
              </>
            )}
          </div>
        ) : null}

        {step === 'browser' ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            <Typography.Text>{t('onboarding.browser.intro')}</Typography.Text>
            {!isAdmin ? (
              <Typography.Text type="tertiary" size="small">
                {t('onboarding.browser.needAdmin')}
              </Typography.Text>
            ) : browserReady ? (
              <Banner
                fullMode={false}
                type="success"
                closeIcon={null}
                description={t('onboarding.browser.ready')}
              />
            ) : (
              <>
                <Typography.Text type="tertiary" size="small">
                  {t('onboarding.browser.dockerHelp')}
                </Typography.Text>
                <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                  <Button
                    theme="solid"
                    type="primary"
                    loading={busy}
                    onClick={() => void enableLocalBrowser()}
                  >
                    {t('onboarding.browser.enableLocal')}
                  </Button>
                  <Link to="/admin/settings/browser" onClick={dismiss}>
                    <Button theme="light">{t('onboarding.browser.openSettings')}</Button>
                  </Link>
                </div>
              </>
            )}
          </div>
        ) : null}
      </div>
    </Modal>
  )
}
