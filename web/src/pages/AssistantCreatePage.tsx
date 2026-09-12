import { useCallback, useEffect, useState } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { assistantsApi, ApiError, setupApi } from '../api'
import { useAssistantLayout } from '../components/AssistantLayout'
import { SetupWorkstation } from '../components/SetupWorkstation'
import { useAuth } from '../auth'

type Step = 'llm' | 'name' | 'identity' | 'preset' | 'setup'

const PRESETS: { id: string; label: string; hint: string }[] = [
  {
    id: 'writing',
    label: '写作与文件',
    hint: '读写工作区文件，不默认开终端/浏览器',
  },
  {
    id: 'code',
    label: '代码（含终端）',
    hint: '适合编程、构建与仓库操作',
  },
  {
    id: 'code_browser',
    label: '代码 + 浏览器',
    hint: '编程并可用浏览器访问网页',
  },
]

const STEP_ORDER: Step[] = ['llm', 'name', 'identity', 'preset', 'setup']

export function AssistantCreatePage() {
  const navigate = useNavigate()
  const { refresh } = useAssistantLayout()
  const auth = useAuth()
  const isAdmin = auth.status === 'ok' && auth.user.role === 'admin'

  const [step, setStep] = useState<Step>('name')
  const [llmChecked, setLlmChecked] = useState(false)
  const [name, setName] = useState('')
  const [bio, setBio] = useState('')
  const [identityMode, setIdentityMode] = useState<
    'proxy_user' | 'independent'
  >('proxy_user')
  const [preset, setPreset] = useState('code')
  const [planId, setPlanId] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [llmReason, setLlmReason] = useState('')

  const checkLlm = useCallback(async () => {
    setError(null)
    try {
      const res = await setupApi.llmReady()
      setLlmChecked(true)
      if (res.ready) {
        setStep((s) => (s === 'llm' ? 'name' : s))
        setLlmReason('')
      } else {
        setStep('llm')
        setLlmReason(res.reason || '尚未配置模型')
      }
    } catch (e) {
      setLlmChecked(true)
      setStep('llm')
      setLlmReason(e instanceof ApiError ? e.message : String(e))
    }
  }, [])

  useEffect(() => {
    void checkLlm()
  }, [checkLlm])

  const visibleSteps = STEP_ORDER.filter((s) => s !== 'llm' || step === 'llm')
  const stepIndex = Math.max(0, visibleSteps.indexOf(step)) + 1
  const stepTotal = step === 'llm' ? visibleSteps.length : visibleSteps.length

  const startSetup = async () => {
    if (busy) return
    setBusy(true)
    setError(null)
    try {
      const plan = await setupApi.createPlan({
        name: name.trim(),
        bio: bio.trim(),
        identityMode,
        preset,
      })
      setPlanId(plan.id)
      setStep('setup')
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }

  const finalize = useCallback(async () => {
    if (busy) return
    setBusy(true)
    setError(null)
    try {
      const a = await assistantsApi.create({
        name: name.trim(),
        bio: bio.trim(),
        identityMode,
        preset,
      })
      await refresh()
      const { sessionId } = await assistantsApi.ensureSession(a.id)
      navigate(`/a/${a.id}/s/${sessionId}`)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
      setBusy(false)
    }
  }, [busy, name, bio, identityMode, preset, refresh, navigate])

  if (!llmChecked) {
    return (
      <div className="chat-pane flex items-center justify-center opacity-50">
        检查模型配置…
      </div>
    )
  }

  return (
    <div className="chat-pane chat-landing">
      <div className="chat-pane-scroll mx-auto max-w-lg px-3 py-5 sm:px-4 sm:py-8">
        <h1 className="font-display text-xl font-semibold sm:text-2xl">
          新建助手
        </h1>
        <p className="mt-1 text-sm opacity-50">
          步骤 {stepIndex} / {stepTotal}
        </p>

        {error && (
          <p className="mt-4 text-sm text-error" role="alert">
            {error}
          </p>
        )}

        {step === 'llm' && (
          <div className="mt-6 space-y-4">
            <h2 className="font-medium">接通大脑</h2>
            <p className="text-sm opacity-70">
              助手需要可用的模型，才能准备工位并之后对话。
            </p>
            {llmReason ? (
              <p className="text-sm text-warning">{llmReason}</p>
            ) : null}
            {isAdmin ? (
              <p className="text-sm">
                请到{' '}
                <Link className="link" to="/settings">
                  设置 · LLM gateway
                </Link>{' '}
                配置上游地址、密钥与默认模型。
              </p>
            ) : (
              <p className="text-sm opacity-70">
                请联系管理员在系统设置中配置模型。
              </p>
            )}
            <button
              type="button"
              className="btn btn-primary min-h-11 sm:min-h-0"
              onClick={() => void checkLlm()}
            >
              已配置，重新检查
            </button>
          </div>
        )}

        {step === 'name' && (
          <div className="mt-6 space-y-4">
            <label className="form-control w-full">
              <span className="label-text mb-1">名称</span>
              <input
                className="input input-bordered w-full"
                value={name}
                onChange={(e) => setName(e.target.value)}
                placeholder="例如：后端助手"
                aria-label="名称"
              />
            </label>
            <label className="form-control w-full">
              <span className="label-text mb-1">简介（强烈建议）</span>
              <textarea
                className="textarea textarea-bordered w-full min-h-28"
                value={bio}
                onChange={(e) => setBio(e.target.value)}
                placeholder="擅长什么、负责范围、不适合什么"
                aria-label="简介"
              />
              <span className="mt-2 text-xs opacity-55">
                简介用于以后给多个助手派活时匹配最合适的人选。写得越具体，越不容易派错。
              </span>
            </label>
            <button
              type="button"
              className="btn btn-primary min-h-11 w-full sm:min-h-0 sm:w-auto"
              disabled={!name.trim()}
              onClick={() => setStep('identity')}
            >
              下一步
            </button>
          </div>
        )}

        {step === 'identity' && (
          <div className="mt-6 space-y-3">
            <p className="text-sm opacity-70">它以谁的名义对外？</p>
            <button
              type="button"
              className={`w-full rounded-lg border p-4 text-left ${
                identityMode === 'proxy_user'
                  ? 'border-primary bg-primary/10'
                  : 'border-base-300'
              }`}
              onClick={() => setIdentityMode('proxy_user')}
            >
              <div className="font-medium">代理我</div>
              <div className="mt-1 text-xs opacity-60">
                对外动作用你的账号，例如提交会显示为你。
              </div>
            </button>
            <button
              type="button"
              className={`w-full rounded-lg border p-4 text-left ${
                identityMode === 'independent'
                  ? 'border-primary bg-primary/10'
                  : 'border-base-300'
              }`}
              onClick={() => setIdentityMode('independent')}
            >
              <div className="font-medium">独立身份</div>
              <div className="mt-1 text-xs opacity-60">
                助手使用自己的账号包，不会冒充你。
              </div>
            </button>
            <div className="flex flex-col gap-2 pt-2 sm:flex-row">
              <button
                type="button"
                className="btn btn-ghost min-h-11 sm:min-h-0"
                onClick={() => setStep('name')}
              >
                上一步
              </button>
              <button
                type="button"
                className="btn btn-primary min-h-11 sm:min-h-0"
                onClick={() => setStep('preset')}
              >
                下一步
              </button>
            </div>
          </div>
        )}

        {step === 'preset' && (
          <div className="mt-6 space-y-3">
            <p className="text-sm opacity-70">先具备哪些能力？</p>
            {PRESETS.map((p) => (
              <button
                key={p.id}
                type="button"
                className={`w-full rounded-lg border p-4 text-left ${
                  preset === p.id
                    ? 'border-primary bg-primary/10'
                    : 'border-base-300'
                }`}
                onClick={() => setPreset(p.id)}
              >
                <div className="font-medium">{p.label}</div>
                <div className="mt-1 text-xs opacity-60">{p.hint}</div>
              </button>
            ))}
            <p className="text-xs opacity-45">手机 / 桌面能力即将推出。</p>
            <div className="flex flex-col gap-2 pt-2 sm:flex-row">
              <button
                type="button"
                className="btn btn-ghost min-h-11 sm:min-h-0"
                onClick={() => setStep('identity')}
              >
                上一步
              </button>
              <button
                type="button"
                className="btn btn-primary min-h-11 sm:min-h-0"
                disabled={busy}
                onClick={() => void startSetup()}
              >
                {busy ? '规划中…' : '下一步：准备工位'}
              </button>
            </div>
          </div>
        )}

        {step === 'setup' && planId && (
          <div>
            <h2 className="mt-6 font-medium">准备工位</h2>
            <SetupWorkstation
              planId={planId}
              onReady={() => void finalize()}
              onError={(msg) => setError(msg)}
            />
            {busy ? (
              <p className="mt-4 text-sm opacity-50">正在创建助手…</p>
            ) : null}
          </div>
        )}
      </div>
    </div>
  )
}
