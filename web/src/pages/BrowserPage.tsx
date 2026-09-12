import { useCallback, useEffect, useState, type CSSProperties } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import {
  Banner,
  Button,
  Input,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui-19'
import {
  ApiError,
  browserTasks,
  environments,
  type BrowserTask,
  type EnvironmentView,
} from '../api'
import { PageShell } from '../components/PageShell'

const sectionGap: CSSProperties = {
  display: 'flex',
  flexDirection: 'column',
  gap: 16,
}

const cardStyle: CSSProperties = {
  border: '1px solid var(--semi-color-border)',
  borderRadius: 8,
  padding: 16,
}

const fieldLabel: CSSProperties = {
  display: 'block',
  marginBottom: 4,
}

export function BrowserPage() {
  const navigate = useNavigate()
  const [envs, setEnvs] = useState<EnvironmentView[]>([])
  const [tasks, setTasks] = useState<BrowserTask[]>([])
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState(false)
  const [loading, setLoading] = useState(true)
  const [kind, setKind] = useState<'explore' | 'verify'>('explore')
  const [url, setUrl] = useState(() =>
    typeof window !== 'undefined' ? window.location.origin : '',
  )
  const [brief, setBrief] = useState('')

  const load = useCallback(async () => {
    setError(null)
    try {
      const res = await environments.list()
      setEnvs(res.environments || [])
    } catch (e) {
      setError(e instanceof Error ? e.message : 'failed to load environments')
    } finally {
      setLoading(false)
    }
    try {
      const listed = await browserTasks.list()
      setTasks(listed.tasks || [])
    } catch {
      setTasks([])
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  const browser = envs.find((e) => e.slot === 'browser')

  async function ensure() {
    setBusy(true)
    setError(null)
    try {
      await environments.ensureBrowser()
      await load()
    } catch (e) {
      if (e instanceof ApiError && e.setup?.length) {
        setError(e.message)
      } else {
        setError(e instanceof Error ? e.message : 'ensure failed')
      }
    } finally {
      setBusy(false)
    }
  }

  async function openDesktop() {
    setBusy(true)
    setError(null)
    try {
      const link = await environments.browserDesktop()
      const desktop = `/vnc.html?url=${encodeURIComponent(link.wsUrl)}`
      window.open(desktop, '_blank', 'noopener,noreferrer')
      await load()
    } catch (e) {
      setError(e instanceof Error ? e.message : 'desktop link failed')
    } finally {
      setBusy(false)
    }
  }

  async function startTask() {
    const startURL = url.trim()
    if (!startURL || busy) return
    setBusy(true)
    setError(null)
    try {
      const created = await browserTasks.create({
        kind,
        url: startURL,
        brief: brief.trim() || undefined,
      })
      try {
        sessionStorage.setItem(
          `roundpen.pendingPrompt.${created.sessionId}`,
          created.prompt,
        )
      } catch {
        /* ignore */
      }
      navigate(`/a`, {
        state: { pendingPrompt: created.prompt },
      })
    } catch (e) {
      setError(e instanceof Error ? e.message : 'task failed')
      setBusy(false)
    }
  }

  return (
    <PageShell subtitle="Browser environment" current="browser" maxWidthClass="max-w-3xl">
      {error && (
        <div role="alert" style={{ marginBottom: 16 }}>
          <Banner
            fullMode={false}
            type="danger"
            description={error}
            closeIcon={null}
          />
        </div>
      )}

      <section style={sectionGap}>
        <div>
          <Typography.Title heading={3} style={{ margin: 0 }}>
            Browser
          </Typography.Title>
          <Typography.Text type="tertiary" size="small" style={{ display: 'block', marginTop: 4 }}>
            One fixed desktop per user: XFCE + Chrome in QEMU. Agents control Chrome
            over CDP; you can take over the display via VNC.
          </Typography.Text>
        </div>

        <div style={cardStyle}>
          {loading ? (
            <Typography.Text type="tertiary" size="small">
              Loading…
            </Typography.Text>
          ) : (
            <>
              <dl
                style={{
                  display: 'grid',
                  gridTemplateColumns: 'auto 1fr',
                  columnGap: 16,
                  rowGap: 8,
                  margin: 0,
                }}
              >
                <Typography.Text type="tertiary" component="dt">
                  Status
                </Typography.Text>
                <Typography.Text strong component="dd" style={{ margin: 0 }}>
                  {browser?.status || 'absent'}
                </Typography.Text>
                <Typography.Text type="tertiary" component="dt">
                  Sandbox
                </Typography.Text>
                <Typography.Text
                  component="dd"
                  style={{
                    margin: 0,
                    fontFamily: 'var(--semi-font-family-code)',
                    fontSize: 12,
                  }}
                >
                  {browser?.sandboxId || '—'}
                </Typography.Text>
                <Typography.Text type="tertiary" component="dt">
                  Template
                </Typography.Text>
                <Typography.Text component="dd" style={{ margin: 0 }}>
                  {browser?.templateId || 'browser-desktop'}
                </Typography.Text>
              </dl>
              <div style={{ marginTop: 16, display: 'flex', flexWrap: 'wrap', gap: 8 }}>
                <Button
                  theme="solid"
                  type="primary"
                  size="small"
                  loading={busy}
                  onClick={() => void ensure()}
                >
                  Start / resume
                </Button>
                <Button
                  type="tertiary"
                  size="small"
                  disabled={busy}
                  onClick={() => void openDesktop()}
                >
                  Open desktop
                </Button>
              </div>
            </>
          )}
        </div>

        <div style={cardStyle}>
          <Typography.Title heading={4} style={{ margin: 0 }}>
            Explore & verify
          </Typography.Title>
          <Typography.Text type="tertiary" size="small" style={{ display: 'block', marginTop: 4 }}>
            Hand the agent a site. It first walks interactive controls (including
            hover-revealed actions), then probes whatever looks off. Results stay
            in a chat session.
          </Typography.Text>

          <div
            style={{ marginTop: 16, display: 'flex', flexWrap: 'wrap', gap: 8 }}
            role="group"
            aria-label="Task kind"
          >
            <Button
              size="small"
              theme={kind === 'explore' ? 'solid' : 'borderless'}
              type={kind === 'explore' ? 'primary' : 'tertiary'}
              disabled={busy}
              onClick={() => setKind('explore')}
            >
              Explore
            </Button>
            <Button
              size="small"
              theme={kind === 'verify' ? 'solid' : 'borderless'}
              type={kind === 'verify' ? 'primary' : 'tertiary'}
              disabled={busy}
              onClick={() => setKind('verify')}
            >
              Verify
            </Button>
          </div>

          <div style={{ marginTop: 16 }}>
            <Typography.Text size="small" type="tertiary" style={fieldLabel}>
              Start URL
            </Typography.Text>
            <Input
              type="url"
              value={url}
              onChange={setUrl}
              placeholder="https://…"
              disabled={busy}
            />
          </div>

          <div style={{ marginTop: 12 }}>
            <Typography.Text size="small" type="tertiary" style={fieldLabel}>
              {kind === 'verify' ? 'What should be true' : 'Notes (optional)'}
            </Typography.Text>
            <TextArea
              rows={3}
              value={brief}
              onChange={setBrief}
              placeholder={
                kind === 'verify'
                  ? 'e.g. hovering a chat and clicking × removes it from the list'
                  : 'e.g. login as yourself, skip billing'
              }
              disabled={busy}
            />
          </div>

          <Button
            theme="solid"
            type="primary"
            size="small"
            style={{ marginTop: 16 }}
            loading={busy}
            disabled={!url.trim()}
            onClick={() => void startTask()}
          >
            {kind === 'verify' ? 'Start verify' : 'Start explore'}
          </Button>

          {tasks.length > 0 && (
            <ul
              style={{
                marginTop: 20,
                marginBottom: 0,
                padding: 0,
                listStyle: 'none',
                display: 'flex',
                flexDirection: 'column',
                gap: 8,
              }}
              aria-label="Recent tasks"
            >
              {tasks.map((t: BrowserTask) => (
                <li
                  key={t.id}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    gap: 8,
                    border: '1px solid var(--semi-color-border)',
                    borderRadius: 6,
                    padding: '8px 12px',
                  }}
                >
                  <Typography.Text type="tertiary" size="small" style={{ flexShrink: 0, textTransform: 'capitalize' }}>
                    {t.kind}
                  </Typography.Text>
                  <Typography.Text
                    ellipsis={{ showTooltip: true }}
                    style={{
                      flex: 1,
                      minWidth: 0,
                      fontFamily: 'var(--semi-font-family-code)',
                      fontSize: 12,
                    }}
                  >
                    {t.url}
                  </Typography.Text>
                  {t.sessionId ? (
                    <Link to={`/a`} style={{ flexShrink: 0 }}>
                      <Typography.Text link size="small">
                        Open chat
                      </Typography.Text>
                    </Link>
                  ) : (
                    <Typography.Text type="tertiary" size="small" style={{ flexShrink: 0 }}>
                      {t.status}
                    </Typography.Text>
                  )}
                </li>
              ))}
            </ul>
          )}
        </div>

        <Typography.Text type="tertiary" size="small">
          Build the guest disk with{' '}
          <code style={{ fontFamily: 'var(--semi-font-family-code)' }}>
            images/browser-qemu/build.sh
          </code>{' '}
          before first start. Desktop uses QEMU VNC over a Unix socket proxied as
          WebSocket (no guest noVNC).
        </Typography.Text>
      </section>
    </PageShell>
  )
}
