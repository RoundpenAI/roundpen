import { useCallback, useEffect, useState, type CSSProperties } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  Banner,
  Button,
  Input,
  Radio,
  RadioGroup,
  Spin,
  Steps,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui-19'
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

const STEP_TITLE: Record<Step, string> = {
  llm: '配置模型',
  name: '名称',
  identity: '身份',
  preset: '能力',
  setup: '工位',
}

const fieldLabel: CSSProperties = {
  display: 'block',
  marginBottom: 4,
}

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
  const stepIndex = Math.max(0, visibleSteps.indexOf(step))
  const stepTotal = visibleSteps.length

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
      <div
        className="chat-pane"
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
        }}
      >
        <Spin tip="检查模型配置…" />
      </div>
    )
  }

  return (
    <div className="chat-pane chat-landing">
      <div
        className="chat-pane-scroll"
        style={{
          maxWidth: 512,
          margin: '0 auto',
          padding: '20px 12px',
          width: '100%',
          boxSizing: 'border-box',
        }}
      >
        <Typography.Title heading={3} style={{ margin: 0 }}>
          新建助手
        </Typography.Title>
        <Typography.Text type="tertiary" size="small" style={{ display: 'block', marginTop: 4 }}>
          步骤 {stepIndex + 1} / {stepTotal}
        </Typography.Text>

        <Steps
          type="basic"
          size="small"
          current={stepIndex}
          style={{ marginTop: 16 }}
        >
          {visibleSteps.map((s) => (
            <Steps.Step key={s} title={STEP_TITLE[s]} />
          ))}
        </Steps>

        {error && (
          <div role="alert" style={{ marginTop: 16 }}>
            <Banner
              fullMode={false}
              type="danger"
              description={error}
              closeIcon={null}
            />
          </div>
        )}

        {step === 'llm' && (
          <div style={{ marginTop: 24, display: 'flex', flexDirection: 'column', gap: 16 }}>
            <Typography.Title heading={5} style={{ margin: 0 }}>
              接通大脑
            </Typography.Title>
            <Typography.Text type="secondary">
              助手需要可用的模型，才能准备工位并之后对话。
            </Typography.Text>
            {llmReason ? (
              <Banner
                fullMode={false}
                type="warning"
                description={llmReason}
                closeIcon={null}
              />
            ) : null}
            {isAdmin ? (
              <Typography.Text>
                请到{' '}
                <Link to="/settings" style={{ color: 'var(--semi-color-link)' }}>
                  设置 · LLM gateway
                </Link>{' '}
                配置上游地址、密钥与默认模型。
              </Typography.Text>
            ) : (
              <Typography.Text type="tertiary">
                请联系管理员在系统设置中配置模型。
              </Typography.Text>
            )}
            <div>
              <Button theme="solid" type="primary" onClick={() => void checkLlm()}>
                已配置，重新检查
              </Button>
            </div>
          </div>
        )}

        {step === 'name' && (
          <div style={{ marginTop: 24, display: 'flex', flexDirection: 'column', gap: 16 }}>
            <div>
              <Typography.Text type="tertiary" size="small" style={fieldLabel}>
                名称
              </Typography.Text>
              <Input
                value={name}
                onChange={setName}
                placeholder="例如：后端助手"
                aria-label="名称"
              />
            </div>
            <div>
              <Typography.Text type="tertiary" size="small" style={fieldLabel}>
                简介（强烈建议）
              </Typography.Text>
              <TextArea
                value={bio}
                onChange={setBio}
                placeholder="擅长什么、负责范围、不适合什么"
                aria-label="简介"
                rows={5}
              />
              <Typography.Text
                type="tertiary"
                size="small"
                style={{ display: 'block', marginTop: 8 }}
              >
                简介用于以后给多个助手派活时匹配最合适的人选。写得越具体，越不容易派错。
              </Typography.Text>
            </div>
            <div>
              <Button
                theme="solid"
                type="primary"
                disabled={!name.trim()}
                onClick={() => setStep('identity')}
              >
                下一步
              </Button>
            </div>
          </div>
        )}

        {step === 'identity' && (
          <div style={{ marginTop: 24, display: 'flex', flexDirection: 'column', gap: 12 }}>
            <Typography.Text type="secondary">它以谁的名义对外？</Typography.Text>
            <RadioGroup
              direction="vertical"
              value={identityMode}
              onChange={(e) =>
                setIdentityMode(e.target.value as 'proxy_user' | 'independent')
              }
            >
              <Radio
                value="proxy_user"
                extra="对外动作用你的账号，例如提交会显示为你。"
              >
                代理我
              </Radio>
              <Radio
                value="independent"
                extra="助手使用自己的账号包，不会冒充你。"
                style={{ marginTop: 8 }}
              >
                独立身份
              </Radio>
            </RadioGroup>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, paddingTop: 8 }}>
              <Button type="tertiary" onClick={() => setStep('name')}>
                上一步
              </Button>
              <Button theme="solid" type="primary" onClick={() => setStep('preset')}>
                下一步
              </Button>
            </div>
          </div>
        )}

        {step === 'preset' && (
          <div style={{ marginTop: 24, display: 'flex', flexDirection: 'column', gap: 12 }}>
            <Typography.Text type="secondary">先具备哪些能力？</Typography.Text>
            <RadioGroup
              direction="vertical"
              value={preset}
              onChange={(e) => setPreset(String(e.target.value))}
            >
              {PRESETS.map((p, i) => (
                <Radio
                  key={p.id}
                  value={p.id}
                  extra={p.hint}
                  style={i > 0 ? { marginTop: 8 } : undefined}
                >
                  {p.label}
                </Radio>
              ))}
            </RadioGroup>
            <Typography.Text type="tertiary" size="small">
              手机 / 桌面能力即将推出。
            </Typography.Text>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8, paddingTop: 8 }}>
              <Button type="tertiary" onClick={() => setStep('identity')}>
                上一步
              </Button>
              <Button
                theme="solid"
                type="primary"
                loading={busy}
                onClick={() => void startSetup()}
              >
                {busy ? '规划中…' : '下一步：准备工位'}
              </Button>
            </div>
          </div>
        )}

        {step === 'setup' && planId && (
          <div>
            <Typography.Title heading={5} style={{ marginTop: 24, marginBottom: 0 }}>
              准备工位
            </Typography.Title>
            <SetupWorkstation
              planId={planId}
              onReady={() => void finalize()}
              onError={(msg) => setError(msg)}
            />
            {busy ? (
              <Typography.Text type="tertiary" style={{ display: 'block', marginTop: 16 }}>
                正在创建助手…
              </Typography.Text>
            ) : null}
          </div>
        )}
      </div>
    </div>
  )
}
