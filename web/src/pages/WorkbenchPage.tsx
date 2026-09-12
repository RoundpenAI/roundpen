import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  Banner,
  Button,
  Card,
  InputNumber,
  Layout,
  Tabs,
  TabPane,
  Tag,
  Typography,
} from '@douyinfe/semi-ui-19'
import { IconRefresh } from '@douyinfe/semi-icons'
import { files, preview, sandboxes, SUGGESTED_CATEGORIES, type Sandbox } from '../api'
import { FileTree } from '../components/FileTree'
import {
  SandboxEditDialog,
  type SandboxEditValues,
} from '../components/SandboxEditDialog'
import { TerminalPane } from '../components/TerminalPane'

type CenterTab = 'editor' | 'preview'
type MobilePane = 'files' | 'editor' | 'preview' | 'ports' | 'term'

const MOBILE_PANES: { id: MobilePane; label: string }[] = [
  { id: 'files', label: 'Files' },
  { id: 'editor', label: 'Editor' },
  { id: 'preview', label: 'Preview' },
  { id: 'ports', label: 'Ports' },
  { id: 'term', label: 'Term' },
]

const { Header, Content } = Layout

export function WorkbenchPage() {
  const { id = '' } = useParams()
  const [sb, setSb] = useState<Sandbox | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [dir, setDir] = useState('.')
  const [filePath, setFilePath] = useState<string | null>(null)
  const [fileContent, setFileContent] = useState('')
  const [fileDirty, setFileDirty] = useState(false)
  const [saving, setSaving] = useState(false)
  const [tab, setTab] = useState<CenterTab>('editor')
  const [mobilePane, setMobilePane] = useState<MobilePane>('editor')
  const [port, setPort] = useState(3000)
  const [previewUrl, setPreviewUrl] = useState<string | null>(null)
  const [previewErr, setPreviewErr] = useState<string | null>(null)
  const [mcpPath, setMcpPath] = useState<string | null>(null)
  const [editOpen, setEditOpen] = useState(false)
  const [editBusy, setEditBusy] = useState(false)
  const [editError, setEditError] = useState<string | null>(null)

  const loadMeta = useCallback(async () => {
    try {
      const got = await sandboxes.get(id)
      setSb(got)
      setError(null)
      const profile = got.metadata?.profile || ''
      const isBrowser =
        got.category?.toLowerCase() === 'browser' || profile === 'browser'
      if (isBrowser) {
        setPort((p) => (p === 3000 ? 8000 : p))
        setMcpPath(`/v1/sandboxes/${id}/browser/mcp`)
      } else {
        setMcpPath(null)
      }
    } catch (e) {
      setError(e instanceof Error ? e.message : 'not found')
    }
  }, [id])

  useEffect(() => {
    void loadMeta()
  }, [loadMeta])

  async function openFile(path: string) {
    if (fileDirty && !confirm('Discard unsaved changes?')) return
    try {
      const text = await files.readText(id, path)
      setFilePath(path)
      setFileContent(text)
      setFileDirty(false)
      setTab('editor')
    } catch (e) {
      setError(e instanceof Error ? e.message : 'read failed')
    }
  }

  async function saveFile() {
    if (!filePath) return
    setSaving(true)
    try {
      await files.writeText(id, filePath, fileContent)
      setFileDirty(false)
    } catch (e) {
      setError(e instanceof Error ? e.message : 'save failed')
    } finally {
      setSaving(false)
    }
  }

  async function openPreview(e?: FormEvent) {
    e?.preventDefault()
    setPreviewErr(null)
    try {
      const link = await preview.link(id, port)
      setPreviewUrl(link.url)
      setTab('preview')
      setMobilePane('preview')
    } catch (err) {
      setPreviewErr(err instanceof Error ? err.message : 'preview failed')
    }
  }

  async function onSaveEdit(values: SandboxEditValues) {
    setEditBusy(true)
    setEditError(null)
    try {
      setSb(await sandboxes.patch(id, values))
      setEditOpen(false)
    } catch (e) {
      setEditError(e instanceof Error ? e.message : 'update failed')
    } finally {
      setEditBusy(false)
    }
  }

  const running = sb?.state === 'running'
  const categoryOptions = Array.from(
    new Set([
      ...SUGGESTED_CATEGORIES,
      ...(sb?.category ? [sb.category] : []),
    ]),
  )

  function selectMobile(pane: MobilePane) {
    setMobilePane(pane)
    if (pane === 'editor') setTab('editor')
    if (pane === 'preview') setTab('preview')
  }

  const centerVisible =
    mobilePane === 'editor' || mobilePane === 'preview'

  return (
    <Layout
      style={{
        height: '100%',
        minHeight: 0,
        background: 'var(--semi-color-bg-0)',
      }}
    >
      <Header
        style={{
          display: 'flex',
          flexShrink: 0,
          flexWrap: 'wrap',
          alignItems: 'center',
          gap: 8,
          padding: '8px 12px',
          background: 'var(--semi-color-bg-1)',
          borderBottom: '1px solid var(--semi-color-border)',
          height: 'auto',
        }}
      >
        <Link to="/" style={{ textDecoration: 'none' }}>
          <Typography.Title heading={5} style={{ margin: 0 }}>
            Roundpen
          </Typography.Title>
        </Link>
        <Typography.Text
          strong
          ellipsis={{ showTooltip: true }}
          style={{ maxWidth: '40vw', minWidth: 0 }}
        >
          {sb?.name || id.slice(0, 8)}
        </Typography.Text>
        <Tag
          size="small"
          color="grey"
          style={{ cursor: 'pointer', maxWidth: '30vw' }}
          onClick={() => {
            setEditError(null)
            setEditOpen(true)
          }}
        >
          {sb?.category
            ? `${sb.category}${sb.isDefault ? ' · default' : ''}`
            : 'no category'}
        </Tag>
        <Typography.Text
          type="tertiary"
          size="small"
          className="wb-hide-mobile"
          style={{ fontFamily: 'var(--semi-font-family-code)' }}
        >
          {id.slice(0, 8)}…
        </Typography.Text>
        {sb && (
          <Tag size="small" color={running ? 'green' : 'orange'}>
            {sb.state}
          </Tag>
        )}
        <div
          style={{
            marginLeft: 'auto',
            display: 'flex',
            flexWrap: 'wrap',
            alignItems: 'center',
            gap: 4,
          }}
        >
          <Button
            theme="borderless"
            type="tertiary"
            size="small"
            onClick={() => {
              setEditError(null)
              setEditOpen(true)
            }}
          >
            Edit
          </Button>
          <Button
            theme="borderless"
            type="tertiary"
            size="small"
            icon={<IconRefresh />}
            onClick={() => void loadMeta()}
          >
            Refresh
          </Button>
          {running && (
            <Button
              theme="borderless"
              type="tertiary"
              size="small"
              onClick={() =>
                void sandboxes
                  .stop(id)
                  .then(loadMeta)
                  .catch((e: Error) => setError(e.message))
              }
            >
              Stop
            </Button>
          )}
        </div>
      </Header>

      <SandboxEditDialog
        open={editOpen && sb != null}
        sandbox={sb}
        categoryOptions={categoryOptions}
        busy={editBusy}
        error={editError}
        onClose={() => {
          if (!editBusy) setEditOpen(false)
        }}
        onSave={onSaveEdit}
      />

      {error && (
        <Banner
          fullMode={false}
          type="danger"
          description={error}
          onClose={() => setError(null)}
          style={{ flexShrink: 0, margin: 0, borderRadius: 0 }}
        />
      )}

      <Content className="wb-root" style={{ padding: 0 }}>
        <div className="wb-mobile-tabs">
          <Tabs
            type="line"
            activeKey={mobilePane}
            onChange={(key) => selectMobile(key as MobilePane)}
          >
            {MOBILE_PANES.map((p) => (
              <TabPane tab={p.label} itemKey={p.id} key={p.id} />
            ))}
          </Tabs>
        </div>

        <div
          className={
            mobilePane === 'term' ? 'wb-grid wb-hide-mobile' : 'wb-grid'
          }
        >
          <aside
            className={
              mobilePane === 'files'
                ? 'rp-pane'
                : 'rp-pane wb-hide-mobile'
            }
            style={{
              overflow: 'hidden',
              borderRight: 'none',
              ...(mobilePane === 'files'
                ? { minHeight: 0, flex: 1 }
                : {}),
            }}
          >
            <FileTree
              sandboxId={id}
              path={dir}
              onPathChange={setDir}
              onOpenFile={(p) => {
                selectMobile('editor')
                void openFile(p)
              }}
            />
          </aside>

          <section
            className={
              centerVisible ? 'rp-pane' : 'rp-pane wb-hide-mobile'
            }
            style={{
              borderLeft: 'none',
              borderRight: 'none',
              ...(centerVisible ? { minHeight: 0, flex: 1 } : {}),
            }}
          >
            <div
              className="rp-pane-header"
              style={{
                display: 'flex',
                alignItems: 'center',
                gap: 8,
                padding: '6px 12px',
              }}
            >
              <div className="wb-hide-mobile">
                <Tabs
                  type="line"
                  size="small"
                  activeKey={tab}
                  onChange={(key) => setTab(key as CenterTab)}
                >
                  <TabPane tab="Editor" itemKey="editor" />
                  <TabPane tab="Preview" itemKey="preview" />
                </Tabs>
              </div>
              {tab === 'editor' && filePath && (
                <>
                  <Typography.Text
                    ellipsis={{ showTooltip: true }}
                    type="tertiary"
                    size="small"
                    style={{
                      flex: 1,
                      minWidth: 0,
                      marginLeft: 8,
                      fontFamily: 'var(--semi-font-family-code)',
                      fontSize: 11,
                      textTransform: 'none',
                      letterSpacing: 'normal',
                    }}
                  >
                    {filePath}
                    {fileDirty ? ' ·' : ''}
                  </Typography.Text>
                  <Button
                    theme="solid"
                    type="primary"
                    size="small"
                    loading={saving}
                    disabled={!fileDirty || saving}
                    onClick={() => void saveFile()}
                    style={{ marginLeft: 'auto' }}
                  >
                    Save
                  </Button>
                </>
              )}
              {tab === 'preview' && (
                <Typography.Text
                  type="tertiary"
                  size="small"
                  className="wb-show-mobile-only"
                  style={{
                    fontFamily: 'var(--semi-font-family-code)',
                    fontSize: 11,
                    textTransform: 'none',
                    letterSpacing: 'normal',
                  }}
                >
                  Preview :{port}
                </Typography.Text>
              )}
            </div>
            <div style={{ minHeight: 0, flex: 1 }}>
              {tab === 'editor' ? (
                filePath ? (
                  <textarea
                    style={{
                      height: '100%',
                      width: '100%',
                      resize: 'none',
                      border: 0,
                      background: 'transparent',
                      padding: 12,
                      fontFamily: 'var(--semi-font-family-code)',
                      fontSize: 13,
                      lineHeight: 1.625,
                      outline: 'none',
                      color: 'var(--semi-color-text-0)',
                    }}
                    value={fileContent}
                    onChange={(e) => {
                      setFileContent(e.target.value)
                      setFileDirty(true)
                    }}
                    spellCheck={false}
                  />
                ) : (
                  <div
                    style={{
                      display: 'flex',
                      height: '100%',
                      alignItems: 'center',
                      justifyContent: 'center',
                      padding: '0 16px',
                      textAlign: 'center',
                    }}
                  >
                    <Typography.Text type="tertiary" size="small">
                      Select a file
                    </Typography.Text>
                  </div>
                )
              ) : previewUrl ? (
                <iframe
                  title="preview"
                  src={previewUrl}
                  style={{
                    height: '100%',
                    width: '100%',
                    border: 0,
                    background: 'var(--semi-color-bg-0)',
                  }}
                />
              ) : (
                <div
                  style={{
                    display: 'flex',
                    height: '100%',
                    alignItems: 'center',
                    justifyContent: 'center',
                    padding: '0 16px',
                    textAlign: 'center',
                  }}
                >
                  <Typography.Text type="tertiary" size="small">
                    Open a preview port from Ports
                  </Typography.Text>
                </div>
              )}
            </div>
          </section>

          <aside
            className={
              mobilePane === 'ports'
                ? 'rp-pane'
                : 'rp-pane wb-hide-mobile'
            }
            style={{
              borderLeft: 'none',
              ...(mobilePane === 'ports'
                ? { minHeight: 0, flex: 1 }
                : {}),
            }}
          >
            <div
              className="rp-pane-header"
              style={{ padding: '8px 12px' }}
            >
              Ports
            </div>
            <form
              style={{
                display: 'flex',
                flexDirection: 'column',
                gap: 8,
                padding: 12,
              }}
              onSubmit={(e) => void openPreview(e)}
            >
              <div>
                <Typography.Text
                  size="small"
                  type="tertiary"
                  style={{ display: 'block', marginBottom: 4 }}
                >
                  Port
                </Typography.Text>
                <InputNumber
                  min={1}
                  max={65535}
                  value={port}
                  onChange={(v) => setPort(typeof v === 'number' ? v : 3000)}
                  style={{ width: '100%' }}
                />
              </div>
              <Button
                theme="solid"
                type="primary"
                htmlType="submit"
                disabled={!running}
                block
              >
                Open preview
              </Button>
              {mcpPath && (
                <Card
                  bodyStyle={{ padding: 8 }}
                  style={{ background: 'var(--semi-color-fill-0)' }}
                >
                  <Typography.Text
                    strong
                    size="small"
                    type="tertiary"
                    style={{ display: 'block', marginBottom: 4 }}
                  >
                    Agent browser MCP
                  </Typography.Text>
                  <Typography.Text
                    size="small"
                    style={{
                      display: 'block',
                      wordBreak: 'break-all',
                      fontFamily: 'var(--semi-font-family-code)',
                      marginBottom: 8,
                    }}
                  >
                    {mcpPath}
                  </Typography.Text>
                  <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4 }}>
                    {[
                      { port: 8000, label: 'MCP 8000' },
                      { port: 6080, label: 'noVNC 6080' },
                    ].map((p) => (
                      <Button
                        key={p.port}
                        theme="borderless"
                        type="tertiary"
                        size="small"
                        onClick={() => setPort(p.port)}
                      >
                        {p.label}
                      </Button>
                    ))}
                  </div>
                </Card>
              )}
              {previewErr && (
                <Typography.Text type="danger" size="small">
                  {previewErr}
                </Typography.Text>
              )}
              {previewUrl && (
                <Typography.Text
                  link={{ href: previewUrl, target: '_blank', rel: 'noreferrer' }}
                  size="small"
                  style={{ wordBreak: 'break-all' }}
                >
                  Open in tab
                </Typography.Text>
              )}
              {!running && (
                <Typography.Text type="tertiary" size="small">
                  Preview needs a running sandbox.
                </Typography.Text>
              )}
            </form>
          </aside>
        </div>

        <div
          className={
            mobilePane === 'term' ? 'rp-pane' : 'rp-pane wb-hide-mobile'
          }
          style={{
            borderTop: 'none',
            ...(mobilePane === 'term'
              ? { minHeight: 0, flex: 1 }
              : {}),
          }}
        >
          <div
            style={{
              minHeight: 0,
              flex: 1,
              background: '#161310',
            }}
          >
            <TerminalPane sandboxId={id} disabled={!running} />
          </div>
        </div>
      </Content>
    </Layout>
  )
}
