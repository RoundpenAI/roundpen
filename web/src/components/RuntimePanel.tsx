import { useCallback, useEffect, useState, type CSSProperties } from 'react'
import {
  Banner,
  Button,
  Tag,
  Typography,
} from '@douyinfe/semi-ui-19'
import {
  ApiError,
  environments,
  runtime,
  type EngineStatus,
  type RuntimeSnapshot,
  type SetupStep,
} from '../api'
import { useT } from '../i18n'

type Props = {
  /** When set, only render this engine (e.g. qemu on the Browser page). */
  engineId?: string
  showPicker?: boolean
}

const sectionGap: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 16,
}

const preStyle: CSSProperties = {
  marginTop: 4,
  overflowX: 'auto',
  borderRadius: 6,
  background: 'var(--semi-color-fill-0)',
  padding: '6px 8px',
  fontFamily: 'var(--semi-font-family-code)',
  fontSize: '0.75rem',
}

export function EngineSetupList({ steps }: { steps: SetupStep[] }) {
  if (!steps.length) return null
  return (
    <ol style={{ margin: 0, paddingLeft: 20, display: 'flex', flexDirection: 'column', gap: 12 }}>
      {steps.map((step) => (
        <li key={step.title}>
          <Typography.Text strong>{step.title}</Typography.Text>
          {step.detail ? (
            <Typography.Text
              type="tertiary"
              size="small"
              style={{ display: 'block', marginTop: 2 }}
            >
              {step.detail}
            </Typography.Text>
          ) : null}
          {step.command ? <pre style={preStyle}>{step.command}</pre> : null}
        </li>
      ))}
    </ol>
  )
}

export function RuntimePanel({ engineId, showPicker = true }: Props) {
  const t = useT()
  const [snap, setSnap] = useState<RuntimeSnapshot | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [starting, setStarting] = useState(false)

  const load = useCallback(async () => {
    setError(null)
    try {
      setSnap(await runtime.get())
    } catch (e) {
      setError(e instanceof Error ? e.message : t('runtime.loadFailed'))
    }
  }, [t])

  useEffect(() => {
    void load()
  }, [load])

  const engines = (snap?.engines ?? []).filter((e) => !engineId || e.id === engineId)
  const selected = snap?.agentEngine || snap?.defaultAgentEngine || 'qemu'
  const current = engines.find((e) => e.id === selected) ?? engines[0]

  async function pick(id: string) {
    setSaving(true)
    setError(null)
    try {
      setSnap(await runtime.setAgentEngine(id))
    } catch (e) {
      setError(e instanceof Error ? e.message : t('runtime.saveFailed'))
    } finally {
      setSaving(false)
    }
  }

  async function startAgent() {
    if (!current) return
    setStarting(true)
    setError(null)
    try {
      await environments.ensureAgent(current.id)
      await load()
    } catch (e) {
      if (e instanceof ApiError && e.setup?.length) {
        setError(e.message)
        setSnap((prev) => {
          if (!prev) return prev
          return {
            ...prev,
            engines: prev.engines.map((eng) =>
              eng.id === (e.engine || current.id) ? { ...eng, setup: e.setup } : eng,
            ),
          }
        })
      } else {
        setError(e instanceof Error ? e.message : t('runtime.startFailed'))
      }
    } finally {
      setStarting(false)
    }
  }

  return (
    <section style={sectionGap}>
      <Typography.Title heading={5} style={{ margin: 0 }}>
        {engineId === 'qemu' ? t('runtime.titleQemu') : t('runtime.title')}
      </Typography.Title>
      <Typography.Text type="tertiary" size="small">
        {engineId ? t('runtime.descQemu') : t('runtime.desc')}
      </Typography.Text>
      {error ? (
        <div role="alert">
          <Banner
            fullMode={false}
            type="danger"
            description={error}
            closeIcon={null}
          />
        </div>
      ) : null}
      {!snap ? (
        <Typography.Text type="tertiary" size="small">
          {t('runtime.loading')}
        </Typography.Text>
      ) : (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
          {engines.map((eng) => (
            <EngineCard
              key={eng.id}
              engine={eng}
              selected={showPicker && selected === eng.id}
              selectable={showPicker}
              disabled={saving}
              onSelect={() => void pick(eng.id)}
            />
          ))}
          {showPicker ? (
            <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'center', gap: 8 }}>
              <Button
                theme="solid"
                type="primary"
                size="small"
                loading={starting}
                disabled={saving || !current}
                onClick={() => void startAgent()}
              >
                {current?.agentReady ? t('runtime.start') : t('runtime.retry')}
              </Button>
              <Button type="tertiary" size="small" onClick={() => void load()}>
                {t('runtime.recheck')}
              </Button>
            </div>
          ) : null}
        </div>
      )}
    </section>
  )
}

function EngineCard({
  engine,
  selected,
  selectable,
  disabled,
  onSelect,
}: {
  engine: EngineStatus
  selected: boolean
  selectable: boolean
  disabled: boolean
  onSelect: () => void
}) {
  const t = useT()
  const ready = selectable ? engine.agentReady : engine.browserReady || engine.ready
  return (
    <div
      style={{
        borderRadius: 8,
        border: `1px solid ${selected ? 'var(--semi-color-primary)' : 'var(--semi-color-border)'}`,
        padding: 16,
      }}
    >
      <div
        style={{
          display: 'flex',
          flexWrap: 'wrap',
          alignItems: 'flex-start',
          justifyContent: 'space-between',
          gap: 8,
        }}
      >
        <div>
          <Typography.Text strong>{engine.label}</Typography.Text>
          <Typography.Text
            type="tertiary"
            size="small"
            style={{ display: 'block', marginTop: 4 }}
          >
            {engine.summary}
          </Typography.Text>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <Tag size="small" color={ready ? 'green' : 'orange'}>
            {ready ? t('runtime.ready') : t('runtime.needsSetup')}
          </Tag>
          {selectable ? (
            <Button
              size="small"
              theme={selected ? 'solid' : 'light'}
              type={selected ? 'primary' : 'tertiary'}
              disabled={disabled}
              onClick={onSelect}
            >
              {selected ? t('runtime.selected') : t('runtime.useThis')}
            </Button>
          ) : null}
        </div>
      </div>
      {!ready && engine.setup?.length ? (
        <div style={{ marginTop: 12 }}>
          <EngineSetupList steps={engine.setup} />
        </div>
      ) : null}
    </div>
  )
}
