import { useEffect, useRef, useState } from 'react'
import { setupApi, type SetupActionRun, type SetupPlan } from '../api'

type Props = {
  planId: string
  onReady: () => void
  onError?: (msg: string) => void
}

function isTerminal(status: string): boolean {
  return status === 'succeeded' || status === 'skipped'
}

function planReady(plan: SetupPlan): boolean {
  return plan.actions.every((a) => isTerminal(a.status))
}

function statusLabel(status: string): string {
  switch (status) {
    case 'pending_confirm':
      return '等待确认'
    case 'pending_manual':
      return '待本机执行'
    case 'queued':
      return '排队'
    case 'running':
      return '进行中'
    case 'succeeded':
      return '完成'
    case 'failed':
      return '失败'
    case 'skipped':
      return '已跳过'
    default:
      return status
  }
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
  } catch {
    /* ignore */
  }
}

function ActionRow({
  planId,
  action,
  onUpdated,
}: {
  planId: string
  action: SetupActionRun
  onUpdated: (p: SetupPlan) => void
}) {
  const [busy, setBusy] = useState(false)

  const run = async (fn: () => Promise<SetupPlan>) => {
    setBusy(true)
    try {
      onUpdated(await fn())
    } finally {
      setBusy(false)
    }
  }

  return (
    <li className="rounded-lg border border-base-300 px-3 py-2">
      <div className="flex flex-wrap items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="font-medium">{action.title}</p>
          <p className="text-xs opacity-55">{action.reason}</p>
          <p className="mt-1 text-xs opacity-70">{statusLabel(action.status)}</p>
          {action.error ? (
            <p className="mt-1 text-xs text-error">{action.error}</p>
          ) : null}
        </div>
        <div className="flex flex-wrap gap-2">
          {action.status === 'pending_confirm' &&
            action.privilege === 'auto' && (
              <button
                type="button"
                className="btn btn-primary btn-sm min-h-11 sm:min-h-0"
                disabled={busy}
                onClick={() =>
                  void run(() => setupApi.confirm(planId, action.actionId))
                }
              >
                允许并安装
              </button>
            )}
          {action.status === 'pending_manual' && (
            <>
              <button
                type="button"
                className="btn btn-ghost btn-sm min-h-11 sm:min-h-0"
                disabled={busy || !action.command}
                onClick={() => void copyText(action.command)}
              >
                复制命令
              </button>
              <button
                type="button"
                className="btn btn-primary btn-sm min-h-11 sm:min-h-0"
                disabled={busy}
                onClick={() =>
                  void run(() => setupApi.recheck(planId, action.actionId))
                }
              >
                我已装好，重新检测
              </button>
            </>
          )}
          {action.status === 'failed' && (
            <button
              type="button"
              className="btn btn-sm min-h-11 sm:min-h-0"
              disabled={busy}
              onClick={() =>
                void run(() => setupApi.retry(planId, action.actionId))
              }
            >
              重试
            </button>
          )}
        </div>
      </div>
      <details className="mt-2 text-xs opacity-80">
        <summary className="cursor-pointer select-none opacity-60">
          详情
        </summary>
        {action.command ? (
          <pre className="mt-1 overflow-x-auto rounded bg-base-200 p-2 font-mono text-[0.7rem]">
            {action.command}
          </pre>
        ) : null}
        {action.log ? (
          <pre className="mt-1 max-h-48 overflow-auto rounded bg-base-200 p-2 font-mono text-[0.7rem] whitespace-pre-wrap">
            {action.log}
          </pre>
        ) : (
          <p className="mt-1 opacity-45">暂无日志</p>
        )}
      </details>
    </li>
  )
}

export function SetupWorkstation({ planId, onReady, onError }: Props) {
  const [plan, setPlan] = useState<SetupPlan | null>(null)
  const readyOnce = useRef(false)

  useEffect(() => {
    let cancelled = false
    const tick = async () => {
      try {
        const p = await setupApi.getPlan(planId)
        if (cancelled) return
        setPlan(p)
        if (planReady(p) && !readyOnce.current) {
          readyOnce.current = true
          onReady()
        }
      } catch (e) {
        if (!cancelled) {
          onError?.(e instanceof Error ? e.message : String(e))
        }
      }
    }
    void tick()
    const id = window.setInterval(() => void tick(), 1500)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
  }, [planId, onReady, onError])

  if (!plan) {
    return <p className="mt-6 text-sm opacity-50">正在检测工位…</p>
  }

  const done = plan.actions.filter((a) => isTerminal(a.status)).length
  const total = plan.actions.length

  return (
    <div className="mt-6 space-y-4">
      <p className="text-sm opacity-80">{plan.summary}</p>
      {total > 0 ? (
        <p className="text-xs opacity-50">
          进度 {done} / {total}
        </p>
      ) : (
        <p className="text-sm opacity-55">无需额外安装，工位已就绪。</p>
      )}
      <ul className="space-y-3">
        {plan.actions.map((a) => (
          <ActionRow
            key={a.actionId}
            planId={planId}
            action={a}
            onUpdated={setPlan}
          />
        ))}
      </ul>
    </div>
  )
}
