import { useState } from 'react'
import { useNavigate } from 'react-router-dom'
import { assistantsApi, ApiError, type AssistantCapabilities } from '../api'
import { useAssistantLayout } from '../components/AssistantLayout'

type Step = 1 | 2 | 3

const PRESETS: {
  id: string
  label: string
  hint: string
  caps?: AssistantCapabilities
}[] = [
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

export function AssistantCreatePage() {
  const navigate = useNavigate()
  const { refresh } = useAssistantLayout()
  const [step, setStep] = useState<Step>(1)
  const [name, setName] = useState('')
  const [bio, setBio] = useState('')
  const [identityMode, setIdentityMode] = useState<
    'proxy_user' | 'independent'
  >('proxy_user')
  const [preset, setPreset] = useState('code')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState<string | null>(null)

  const submit = async () => {
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
  }

  return (
    <div className="chat-pane chat-landing">
      <div className="chat-pane-scroll mx-auto max-w-lg px-4 py-8">
        <h1 className="font-display text-2xl font-semibold">新建助手</h1>
        <p className="mt-1 text-sm opacity-50">步骤 {step} / 3</p>

        {error && (
          <p className="mt-4 text-sm text-error" role="alert">
            {error}
          </p>
        )}

        {step === 1 && (
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
              className="btn btn-primary"
              disabled={!name.trim()}
              onClick={() => setStep(2)}
            >
              下一步
            </button>
          </div>
        )}

        {step === 2 && (
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
            <div className="flex gap-2 pt-2">
              <button
                type="button"
                className="btn btn-ghost"
                onClick={() => setStep(1)}
              >
                上一步
              </button>
              <button
                type="button"
                className="btn btn-primary"
                onClick={() => setStep(3)}
              >
                下一步
              </button>
            </div>
          </div>
        )}

        {step === 3 && (
          <div className="mt-6 space-y-3">
            <p className="text-sm opacity-70">先具备哪些能力？</p>
            {PRESETS.map((p) => (
              <button
                key={p.id}
                type="button"
                className={`w-full rounded-lg border p-4 text-left ${
                  preset === p.id ? 'border-primary bg-primary/10' : 'border-base-300'
                }`}
                onClick={() => setPreset(p.id)}
              >
                <div className="font-medium">{p.label}</div>
                <div className="mt-1 text-xs opacity-60">{p.hint}</div>
              </button>
            ))}
            <p className="text-xs opacity-45">手机 / 桌面能力即将推出。</p>
            <div className="flex gap-2 pt-2">
              <button
                type="button"
                className="btn btn-ghost"
                onClick={() => setStep(2)}
              >
                上一步
              </button>
              <button
                type="button"
                className="btn btn-primary"
                disabled={busy}
                onClick={() => void submit()}
              >
                {busy ? '创建中…' : '创建'}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>
  )
}
