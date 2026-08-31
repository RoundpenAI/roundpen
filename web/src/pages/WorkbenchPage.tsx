import { useCallback, useEffect, useState, type FormEvent } from 'react'
import { Link, useParams } from 'react-router-dom'
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

  return (
    <div className="flex h-full min-h-0 flex-col">
      <header className="flex shrink-0 flex-wrap items-center gap-2 border-b border-base-300 bg-base-200 px-3 py-2">
        <Link to="/" className="font-display text-lg font-semibold tracking-tight link link-hover">
          Roundpen
        </Link>
        <span className="min-w-0 max-w-[40vw] truncate text-sm font-medium sm:max-w-none">
          {sb?.name || id.slice(0, 8)}
        </span>
        {sb?.category ? (
          <button
            type="button"
            className="badge badge-ghost badge-sm max-w-[30vw] truncate"
            onClick={() => {
              setEditError(null)
              setEditOpen(true)
            }}
          >
            {sb.category}
            {sb.isDefault ? ' · default' : ''}
          </button>
        ) : (
          <button
            type="button"
            className="badge badge-ghost badge-sm opacity-50"
            onClick={() => {
              setEditError(null)
              setEditOpen(true)
            }}
          >
            no category
          </button>
        )}
        <span className="hidden truncate font-mono text-xs opacity-45 sm:inline">
          {id.slice(0, 8)}…
        </span>
        {sb && (
          <span
            className={`badge badge-sm ${
              running ? 'badge-success' : 'badge-warning'
            }`}
          >
            {sb.state}
          </span>
        )}
        <div className="ml-auto flex flex-wrap items-center gap-1 sm:gap-2">
          <button
            type="button"
            className="btn btn-ghost btn-xs min-h-8"
            onClick={() => {
              setEditError(null)
              setEditOpen(true)
            }}
          >
            Edit
          </button>
          <button
            type="button"
            className="btn btn-ghost btn-xs min-h-8"
            onClick={() => void loadMeta()}
          >
            Refresh
          </button>
          {running && (
            <button
              type="button"
              className="btn btn-ghost btn-xs min-h-8"
              onClick={() =>
                void sandboxes.stop(id).then(loadMeta).catch((e: Error) => setError(e.message))
              }
            >
              Stop
            </button>
          )}
        </div>
      </header>

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
        <div className="shrink-0 bg-error/15 px-3 py-1.5 text-xs text-error">
          {error}
          <button
            type="button"
            className="btn btn-ghost btn-xs ml-2"
            onClick={() => setError(null)}
          >
            dismiss
          </button>
        </div>
      )}

      <div className="flex min-h-0 flex-1 flex-col md:grid md:grid-rows-[minmax(0,1fr)_minmax(180px,38%)]">
        <div className="flex shrink-0 overflow-x-auto border-b border-base-300 md:hidden">
          {MOBILE_PANES.map((p) => (
            <button
              key={p.id}
              type="button"
              className={`btn btn-ghost btn-sm min-h-11 shrink-0 rounded-none ${
                mobilePane === p.id ? 'btn-active' : ''
              }`}
              onClick={() => selectMobile(p.id)}
            >
              {p.label}
            </button>
          ))}
        </div>

        <div
          className={`grid min-h-0 flex-1 grid-cols-1 md:grid-cols-[220px_minmax(0,1fr)_200px] ${
            mobilePane === 'term' ? 'max-md:hidden' : ''
          }`}
        >
          <aside
            className={`rp-pane min-h-0 overflow-hidden border-r-0 ${
              mobilePane === 'files' ? 'max-md:min-h-0 max-md:flex-1' : 'max-md:hidden'
            }`}
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
            className={`rp-pane flex min-h-0 flex-col border-l-0 border-r-0 ${
              mobilePane === 'editor' || mobilePane === 'preview'
                ? 'max-md:min-h-0 max-md:flex-1'
                : 'max-md:hidden'
            }`}
          >
            <div className="rp-pane-header flex items-center gap-2 px-3 py-1.5">
              <button
                type="button"
                className={`btn btn-ghost btn-xs hidden md:inline-flex ${tab === 'editor' ? 'btn-active' : ''}`}
                onClick={() => setTab('editor')}
              >
                Editor
              </button>
              <button
                type="button"
                className={`btn btn-ghost btn-xs hidden md:inline-flex ${tab === 'preview' ? 'btn-active' : ''}`}
                onClick={() => setTab('preview')}
              >
                Preview
              </button>
              {tab === 'editor' && filePath && (
                <>
                  <span className="truncate font-mono text-[11px] normal-case tracking-normal opacity-60 md:ml-2">
                    {filePath}
                    {fileDirty ? ' ·' : ''}
                  </span>
                  <button
                    type="button"
                    className="btn btn-primary btn-xs ml-auto min-h-8"
                    disabled={!fileDirty || saving}
                    onClick={() => void saveFile()}
                  >
                    {saving ? 'Saving…' : 'Save'}
                  </button>
                </>
              )}
              {tab === 'preview' && (
                <span className="font-mono text-[11px] normal-case tracking-normal opacity-60 md:hidden">
                  Preview :{port}
                </span>
              )}
            </div>
            <div className="min-h-0 flex-1">
              {tab === 'editor' ? (
                filePath ? (
                  <textarea
                    className="h-full w-full resize-none border-0 bg-transparent p-3 font-mono text-base leading-relaxed outline-none md:text-[13px]"
                    value={fileContent}
                    onChange={(e) => {
                      setFileContent(e.target.value)
                      setFileDirty(true)
                    }}
                    spellCheck={false}
                  />
                ) : (
                  <div className="flex h-full items-center justify-center px-4 text-center text-sm opacity-40">
                    Select a file
                  </div>
                )
              ) : previewUrl ? (
                <iframe
                  title="preview"
                  src={previewUrl}
                  className="h-full w-full border-0 bg-base-100"
                />
              ) : (
                <div className="flex h-full items-center justify-center px-4 text-center text-sm opacity-40">
                  Open a preview port from Ports
                </div>
              )}
            </div>
          </section>

          <aside
            className={`rp-pane flex min-h-0 flex-col border-l-0 ${
              mobilePane === 'ports' ? 'max-md:min-h-0 max-md:flex-1' : 'max-md:hidden'
            }`}
          >
            <div className="rp-pane-header px-3 py-2">Ports</div>
            <form
              className="flex flex-col gap-2 p-3"
              onSubmit={(e) => void openPreview(e)}
            >
              <label className="form-control">
                <span className="label-text mb-1 text-xs opacity-60">Port</span>
                <input
                  type="number"
                  min={1}
                  max={65535}
                  inputMode="numeric"
                  className="input input-bordered w-full sm:input-sm"
                  value={port}
                  onChange={(e) => setPort(Number(e.target.value) || 3000)}
                />
              </label>
              <button type="submit" className="btn btn-primary min-h-11 sm:btn-sm sm:min-h-0" disabled={!running}>
                Open preview
              </button>
              {mcpPath && (
                <div className="space-y-1.5 rounded-md bg-base-200 px-2 py-2 text-xs">
                  <p className="font-medium opacity-70">Agent browser MCP</p>
                  <code className="block break-all opacity-80">{mcpPath}</code>
                  <div className="flex flex-wrap gap-1">
                    {[
                      { port: 8000, label: 'MCP 8000' },
                      { port: 6080, label: 'noVNC 6080' },
                    ].map((p) => (
                      <button
                        key={p.port}
                        type="button"
                        className="btn btn-ghost btn-xs"
                        onClick={() => setPort(p.port)}
                      >
                        {p.label}
                      </button>
                    ))}
                  </div>
                </div>
              )}
              {previewErr && (
                <p className="text-xs text-error">{previewErr}</p>
              )}
              {previewUrl && (
                <a
                  href={previewUrl}
                  target="_blank"
                  rel="noreferrer"
                  className="link link-primary break-all text-xs"
                >
                  Open in tab
                </a>
              )}
              {!running && (
                <p className="text-xs opacity-50">
                  Preview needs a running sandbox.
                </p>
              )}
            </form>
          </aside>
        </div>

        <div
          className={`rp-pane flex min-h-0 flex-col border-t-0 ${
            mobilePane === 'term' ? 'max-md:min-h-0 max-md:flex-1' : 'max-md:hidden'
          }`}
        >
          <div className="min-h-0 flex-1 bg-[#161310]">
            <TerminalPane sandboxId={id} disabled={!running} />
          </div>
        </div>
      </div>
    </div>
  )
}
