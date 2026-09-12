import { useEffect, useRef, useState, type CSSProperties } from 'react'
import {
  Button,
  Collapse,
  List,
  Progress,
  Spin,
  Tag,
  Typography,
} from '@douyinfe/semi-ui-19'
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
  const actions = plan.actions ?? []
  return actions.every((a) => isTerminal(a.status))
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

function statusColor(
  status: string,
): 'grey' | 'blue' | 'orange' | 'green' | 'red' | 'cyan' {
  switch (status) {
    case 'pending_confirm':
    case 'pending_manual':
      return 'orange'
    case 'queued':
      return 'grey'
    case 'running':
      return 'blue'
    case 'succeeded':
      return 'green'
    case 'failed':
      return 'red'
    case 'skipped':
      return 'cyan'
    default:
      return 'grey'
  }
}

async function copyText(text: string) {
  try {
    await navigator.clipboard.writeText(text)
  } catch {
    /* ignore */
  }
}

const preStyle: CSSProperties = {
  marginTop: 8,
  overflow: 'auto',
  maxHeight: 192,
  borderRadius: 6,
  padding: 8,
  fontFamily: 'var(--semi-font-family-regular), monospace',
  fontSize: 11,
  whiteSpace: 'pre-wrap',
  background: 'var(--semi-color-fill-0)',
  color: 'var(--semi-color-text-1)',
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
    <List.Item
      style={{
        border: '1px solid var(--semi-color-border)',
        borderRadius: 8,
        marginBottom: 12,
        padding: '12px 12px 4px',
        background: 'var(--semi-color-bg-1)',
        flexDirection: 'column',
        alignItems: 'stretch',
      }}
      main={
        <div>
          <div
            style={{
              display: 'flex',
              flexWrap: 'wrap',
              alignItems: 'flex-start',
              justifyContent: 'space-between',
              gap: 8,
            }}
          >
            <div style={{ minWidth: 0, flex: 1 }}>
              <Typography.Text strong style={{ display: 'block' }}>
                {action.title}
              </Typography.Text>
              <Typography.Text type="tertiary" size="small">
                {action.reason}
              </Typography.Text>
              <div style={{ marginTop: 6 }}>
                <Tag size="small" color={statusColor(action.status)}>
                  {statusLabel(action.status)}
                </Tag>
              </div>
              {action.error ? (
                <Typography.Text
                  type="danger"
                  size="small"
                  style={{ display: 'block', marginTop: 6 }}
                >
                  {action.error}
                </Typography.Text>
              ) : null}
            </div>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 8 }}>
              {action.status === 'pending_confirm' &&
                action.privilege === 'auto' && (
                  <Button
                    theme="solid"
                    type="primary"
                    size="small"
                    loading={busy}
                    onClick={() =>
                      void run(() => setupApi.confirm(planId, action.actionId))
                    }
                  >
                    允许并安装
                  </Button>
                )}
              {action.status === 'pending_manual' && (
                <>
                  <Button
                    type="tertiary"
                    size="small"
                    disabled={busy || !action.command}
                    onClick={() => void copyText(action.command)}
                  >
                    复制命令
                  </Button>
                  <Button
                    theme="solid"
                    type="primary"
                    size="small"
                    loading={busy}
                    onClick={() =>
                      void run(() => setupApi.recheck(planId, action.actionId))
                    }
                  >
                    我已装好，重新检测
                  </Button>
                </>
              )}
              {action.status === 'failed' && (
                <Button
                  size="small"
                  loading={busy}
                  onClick={() =>
                    void run(() => setupApi.retry(planId, action.actionId))
                  }
                >
                  重试
                </Button>
              )}
            </div>
          </div>
          <Collapse style={{ marginTop: 8 }}>
            <Collapse.Panel header="详情" itemKey="detail">
              {action.command ? (
                <pre style={preStyle}>{action.command}</pre>
              ) : null}
              {action.log ? (
                <pre style={preStyle}>{action.log}</pre>
              ) : (
                <Typography.Text type="tertiary" size="small">
                  暂无日志
                </Typography.Text>
              )}
            </Collapse.Panel>
          </Collapse>
        </div>
      }
    />
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
    return (
      <div style={{ marginTop: 24 }}>
        <Spin tip="正在检测工位…" />
      </div>
    )
  }

  const actions = plan.actions ?? []
  const done = actions.filter((a) => isTerminal(a.status)).length
  const total = actions.length
  const percent = total > 0 ? Math.round((done / total) * 100) : 100

  return (
    <div style={{ marginTop: 24, display: 'flex', flexDirection: 'column', gap: 16 }}>
      <Typography.Text type="secondary">{plan.summary}</Typography.Text>
      {total > 0 ? (
        <div>
          <Typography.Text type="tertiary" size="small">
            进度 {done} / {total}
          </Typography.Text>
          <Progress percent={percent} showInfo style={{ marginTop: 8 }} />
        </div>
      ) : (
        <Typography.Text type="tertiary">无需额外安装，工位已就绪。</Typography.Text>
      )}
      <List
        dataSource={actions}
        split={false}
        renderItem={(a) => (
          <ActionRow planId={planId} action={a} onUpdated={setPlan} />
        )}
      />
    </div>
  )
}
