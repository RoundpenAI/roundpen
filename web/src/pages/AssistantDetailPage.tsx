import { useCallback, useEffect, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  assistantsApi,
  ApiError,
  type ActivityItem,
  type AssistTicket,
  type Assistant,
  type AssistantCapabilities,
  type AssistantDirectoryGrant,
} from '../api'
import { useAssistantLayout } from '../components/AssistantLayout'

export function AssistantDetailPage() {
  const { assistantId = '' } = useParams()
  const { refresh } = useAssistantLayout()
  const [a, setA] = useState<Assistant | null>(null)
  const [activity, setActivity] = useState<ActivityItem[]>([])
  const [busyNow, setBusyNow] = useState(false)
  const [tickets, setTickets] = useState<AssistTicket[]>([])
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [name, setName] = useState('')
  const [bio, setBio] = useState('')
  const [newPath, setNewPath] = useState('')
  const [newMode, setNewMode] = useState<'read' | 'readwrite'>('read')
  const [allowText, setAllowText] = useState('')

  const load = useCallback(async () => {
    try {
      const [got, act, tix] = await Promise.all([
        assistantsApi.get(assistantId),
        assistantsApi.activity(assistantId).catch(() => ({
          activity: [] as ActivityItem[],
          busy: false,
        })),
        assistantsApi.listTickets(assistantId, true).catch(() => ({
          tickets: [] as AssistTicket[],
        })),
      ])
      setA(got)
      setName(got.name)
      setBio(got.bio)
      setAllowText((got.networkAllowlist ?? []).join('\n'))
      setActivity(act.activity ?? [])
      setBusyNow(Boolean(act.busy))
      setTickets(tix.tickets ?? [])
      setError(null)
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    }
  }, [assistantId])

  useEffect(() => {
    void load()
  }, [load])

  const patch = async (body: Parameters<typeof assistantsApi.update>[1]) => {
    setSaving(true)
    setError(null)
    try {
      const updated = await assistantsApi.update(assistantId, body)
      setA(updated)
      setName(updated.name)
      setBio(updated.bio)
      setAllowText((updated.networkAllowlist ?? []).join('\n'))
      await refresh()
    } catch (e) {
      setError(e instanceof ApiError ? e.message : String(e))
    } finally {
      setSaving(false)
    }
  }

  if (!a && !error) {
    return (
      <div className="chat-pane flex items-center justify-center opacity-50">
        加载中…
      </div>
    )
  }
  if (!a) {
    return (
      <div className="chat-pane p-6 text-error" role="alert">
        {error}
      </div>
    )
  }

  const caps = a.capabilities

  const setCap = (key: keyof AssistantCapabilities, value: boolean) => {
    const next = { ...caps, [key]: value, mobile: false, desktop: false }
    void patch({ capabilities: next })
  }

  const addGrant = () => {
    const path = newPath.trim()
    if (!path) return
    const grants: AssistantDirectoryGrant[] = [
      ...(a.directoryGrants ?? []),
      { path, mode: newMode, createdAt: new Date().toISOString() },
    ]
    setNewPath('')
    void patch({ directoryGrants: grants })
  }

  const removeGrant = (path: string) => {
    void patch({
      directoryGrants: (a.directoryGrants ?? []).filter((g) => g.path !== path),
    })
  }

  const changeIdentity = (mode: 'proxy_user' | 'independent') => {
    if (mode === a.identityMode) return
    if (
      !window.confirm(
        '切换身份模式会使已绑定渠道失效或需要重连。确定继续？',
      )
    ) {
      return
    }
    void patch({ identityMode: mode, confirmIdentityChange: true })
  }

  return (
    <div className="chat-pane">
      <div className="chat-pane-scroll mx-auto max-w-2xl space-y-6 px-3 py-4 sm:space-y-8 sm:px-4 sm:py-6">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0">
          <h1 className="font-display truncate text-xl font-semibold sm:text-2xl">
            {a.name}
          </h1>
          <p className="text-sm opacity-50">
            状态：{a.status === 'active' ? '可用' : '已停用'}
            {a.primarySessionId ? ' · 有对话' : ''}
          </p>
        </div>
        <Link
          className="btn btn-primary btn-sm min-h-11 shrink-0 sm:min-h-0"
          to={`/a/${a.id}/chat`}
        >
          打开对话
        </Link>
      </div>

      {error && (
        <p className="text-sm text-error" role="alert">
          {error}
        </p>
      )}

      <section className="space-y-3">
        <h2 className="font-medium">基本信息</h2>
        <label className="form-control">
          <span className="label-text mb-1">名称</span>
          <input
            className="input input-bordered"
            value={name}
            onChange={(e) => setName(e.target.value)}
            onBlur={() => {
              if (name.trim() && name.trim() !== a.name) {
                void patch({ name: name.trim() })
              }
            }}
          />
        </label>
        <label className="form-control">
          <span className="label-text mb-1">简介</span>
          <textarea
            className="textarea textarea-bordered min-h-24"
            value={bio}
            onChange={(e) => setBio(e.target.value)}
            onBlur={() => {
              if (bio !== a.bio) void patch({ bio })
            }}
          />
          <span className="mt-1 text-xs opacity-50">
            简介用于派活时匹配最合适的助手。
          </span>
        </label>
      </section>

      <section className="space-y-2">
        <h2 className="font-medium">此刻</h2>
        <p className="text-sm opacity-55">
          {busyNow ? '正在工作中…' : '当前空闲'}
          {a.primarySessionId ? ' · 有主对话' : ''}
        </p>
        {tickets.length > 0 && (
          <div className="space-y-2 rounded-lg border border-warning/40 bg-warning/10 p-3">
            <p className="text-sm font-medium">待处理协助单</p>
            {tickets.map((t) => (
              <div key={t.id} className="text-sm">
                <p>{t.title}</p>
                <p className="text-xs opacity-60">{t.askHuman || t.reason}</p>
                <div className="mt-1 flex flex-wrap gap-2">
                  {t.kind === 'policy_apply' ? (
                    <>
                      <button
                        type="button"
                        className="btn btn-xs"
                        onClick={() =>
                          void assistantsApi
                            .resolveTicket(t.id, { resolution: 'allow_once' })
                            .then(() => load())
                        }
                      >
                        允许一次
                      </button>
                      <button
                        type="button"
                        className="btn btn-xs btn-primary"
                        onClick={() =>
                          void assistantsApi
                            .resolveTicket(t.id, { resolution: 'permanent' })
                            .then(() => {
                              void refresh()
                              return load()
                            })
                        }
                      >
                        写入档案并继续
                      </button>
                      <button
                        type="button"
                        className="btn btn-xs btn-ghost"
                        onClick={() =>
                          void assistantsApi
                            .resolveTicket(t.id, { resolution: 'reject' })
                            .then(() => load())
                        }
                      >
                        拒绝
                      </button>
                    </>
                  ) : null}
                  <Link className="btn btn-xs btn-ghost" to={`/a/${a.id}/chat`}>
                    在对话中处理
                  </Link>
                </div>
              </div>
            ))}
          </div>
        )}
        <ul className="max-h-64 space-y-1 overflow-y-auto rounded-lg border border-base-300 p-3 text-xs">
          {activity.length === 0 && (
            <li className="opacity-45">暂无活动记录</li>
          )}
          {[...activity]
            .sort((x, y) => Date.parse(y.at) - Date.parse(x.at))
            .slice(0, 40)
            .map((it) => (
              <li key={`${it.source}-${it.id}`} className="opacity-80">
                <span className="opacity-40">
                  {new Date(it.at).toLocaleString()}
                </span>{' '}
                <span className="opacity-50">[{it.kind}]</span> {it.title}
                {it.detail ? (
                  <span className="opacity-45"> — {it.detail}</span>
                ) : null}
              </li>
            ))}
        </ul>
      </section>

      <section className="space-y-3">
        <h2 className="font-medium">可见范围</h2>
        <p className="text-sm opacity-55">
          默认有一个仅它可见的工作区。需要访问本机目录时在此授权。
        </p>
        <ul className="space-y-2">
          {(a.directoryGrants ?? []).map((g) => (
            <li
              key={g.path}
              className="flex items-center justify-between gap-2 rounded border border-base-300 px-3 py-2 text-sm"
            >
              <span className="min-w-0 truncate">
                {g.path}{' '}
                <span className="opacity-45">
                  ({g.mode === 'readwrite' ? '读写' : '只读'})
                </span>
              </span>
              <button
                type="button"
                className="btn btn-ghost btn-xs"
                onClick={() => removeGrant(g.path)}
              >
                移除
              </button>
            </li>
          ))}
        </ul>
        <div className="flex min-w-0 flex-col gap-2 sm:flex-row sm:flex-wrap">
          <input
            className="input input-bordered input-sm w-full min-w-0 flex-1"
            placeholder="/path/to/folder"
            value={newPath}
            onChange={(e) => setNewPath(e.target.value)}
          />
          <div className="flex gap-2">
            <select
              className="select select-bordered select-sm min-h-11 flex-1 sm:min-h-0 sm:flex-none"
              value={newMode}
              onChange={(e) =>
                setNewMode(e.target.value as 'read' | 'readwrite')
              }
            >
              <option value="read">只读</option>
              <option value="readwrite">读写</option>
            </select>
            <button
              type="button"
              className="btn btn-sm min-h-11 sm:min-h-0"
              onClick={addGrant}
            >
              添加授权
            </button>
          </div>
        </div>
      </section>

      <section className="space-y-3">
        <h2 className="font-medium">能力清单</h2>
        {(
          [
            ['shell', '终端', true],
            ['browser', '浏览器', true],
            ['mobile', '手机', false],
            ['desktop', '桌面', false],
          ] as const
        ).map(([key, label, ready]) => (
          <label
            key={key}
            className="flex items-center justify-between gap-3 rounded border border-base-300 px-3 py-2"
          >
            <span>
              {label}
              {!ready && (
                <span className="ml-2 text-xs opacity-45">即将推出</span>
              )}
            </span>
            <input
              type="checkbox"
              className="toggle"
              checked={Boolean(caps[key])}
              disabled={!ready || saving}
              onChange={(e) => setCap(key, e.target.checked)}
            />
          </label>
        ))}
      </section>

      <section className="space-y-3">
        <h2 className="font-medium">网络</h2>
        {(
          [
            ['none', '禁止上网'],
            ['dev_sites', '常用开发站'],
            ['all', '允许全部'],
          ] as const
        ).map(([tier, label]) => (
          <label key={tier} className="flex items-center gap-2 text-sm">
            <input
              type="radio"
              name="networkTier"
              checked={a.networkTier === tier}
              disabled={saving}
              onChange={() => {
                if (tier === 'all' && !window.confirm('允许全部出站风险最高，确定？')) {
                  return
                }
                void patch({ networkTier: tier })
              }}
            />
            {label}
          </label>
        ))}
        <label className="form-control">
          <span className="label-text mb-1">高级白名单（每行一个域名）</span>
          <textarea
            className="textarea textarea-bordered min-h-20 font-mono text-xs"
            value={allowText}
            onChange={(e) => setAllowText(e.target.value)}
            onBlur={() => {
              const list = allowText
                .split(/[\n,]+/)
                .map((s) => s.trim())
                .filter(Boolean)
              const prev = a.networkAllowlist ?? []
              if (JSON.stringify(list) !== JSON.stringify(prev)) {
                void patch({ networkAllowlist: list })
              }
            }}
          />
        </label>
      </section>

      <section className="space-y-3">
        <h2 className="font-medium">身份绑定</h2>
        <p className="text-sm opacity-60">
          当前：{a.identityMode === 'proxy_user' ? '代理我' : '独立身份'}
        </p>
        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            className={`btn btn-sm ${a.identityMode === 'proxy_user' ? 'btn-primary' : 'btn-ghost'}`}
            onClick={() => changeIdentity('proxy_user')}
          >
            代理我
          </button>
          <button
            type="button"
            className={`btn btn-sm ${a.identityMode === 'independent' ? 'btn-primary' : 'btn-ghost'}`}
            onClick={() => changeIdentity('independent')}
          >
            独立身份
          </button>
        </div>
        {a.identityMode === 'proxy_user' ? (
          <p className="text-sm opacity-55">
            使用你的账号。可在{' '}
            <Link className="link" to="/settings">
              设置
            </Link>{' '}
            中管理 Git 等连接。
          </p>
        ) : (
          <p className="text-sm opacity-55">
            助手专用账号将在后续版本连接。
          </p>
        )}
      </section>

      {saving && <p className="text-xs opacity-45">保存中…</p>}
      </div>
    </div>
  )
}
