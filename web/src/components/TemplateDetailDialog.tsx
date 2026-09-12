import { useEffect, useState, type FormEvent } from 'react'
import {
  Banner,
  Button,
  Checkbox,
  InputNumber,
  Modal,
  Popconfirm,
  Select,
  Spin,
  Tag,
  TextArea,
  Toast,
  Typography,
} from '@douyinfe/semi-ui-19'
import {
  templateDisplayName,
  templates,
  type Template,
  type TemplateDetail,
  type TemplatePatch,
} from '../api'

type Props = {
  open: boolean
  templateId: string | null
  busy?: boolean
  error?: string | null
  onClose: () => void
  onSaved: (tpl: Template) => void
  onDeleted: (templateID: string) => void
  onBuild: (tpl: Template) => void
}

function formatWhen(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

function buildActionLabel(status: string): string {
  switch (status) {
    case 'building':
      return 'Logs'
    case 'ready':
      return 'Rebuild'
    case 'error':
      return 'Retry build'
    default:
      return 'Build'
  }
}

function statusTagColor(
  status: string,
): 'green' | 'blue' | 'red' | 'orange' | 'grey' {
  switch (status) {
    case 'ready':
      return 'green'
    case 'building':
      return 'blue'
    case 'error':
      return 'red'
    case 'waiting':
      return 'orange'
    default:
      return 'grey'
  }
}

export function TemplateDetailDialog({
  open,
  templateId,
  busy = false,
  error = null,
  onClose,
  onSaved,
  onDeleted,
  onBuild,
}: Props) {
  const [detail, setDetail] = useState<TemplateDetail | null>(null)
  const [loading, setLoading] = useState(false)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [saveError, setSaveError] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)
  const [deleteBusy, setDeleteBusy] = useState(false)

  const [description, setDescription] = useState('')
  const [profile, setProfile] = useState('dev')
  const [cpuCount, setCpuCount] = useState(1)
  const [memoryMB, setMemoryMB] = useState(512)
  const [diskSizeMB, setDiskSizeMB] = useState(5120)
  const [isPublic, setIsPublic] = useState(true)

  function syncFormFromDetail(d: TemplateDetail) {
    setDescription(d.description ?? '')
    setProfile(d.profile || 'dev')
    setCpuCount(d.cpuCount)
    setMemoryMB(d.memoryMB)
    setDiskSizeMB(d.diskSizeMB)
    setIsPublic(d.public)
  }

  useEffect(() => {
    if (!open || !templateId) {
      setDetail(null)
      return
    }
    setLoading(true)
    setLoadError(null)
    setSaveError(null)
    void templates
      .get(templateId)
      .then((d) => {
        setDetail(d)
        syncFormFromDetail(d)
      })
      .catch((e) =>
        setLoadError(e instanceof Error ? e.message : 'failed to load'),
      )
      .finally(() => setLoading(false))
  }, [open, templateId])

  const builtin = detail?.builtin ?? false
  const displayName = detail
    ? templateDisplayName(detail)
    : templateId
      ? templateId.slice(0, 8)
      : ''

  async function submit(e: FormEvent) {
    e.preventDefault()
    if (!detail || saving) return
    setSaveError(null)
    setSaving(true)
    const patch: TemplatePatch = {
      description,
      profile,
      public: isPublic,
      cpuCount: cpuCount > 0 ? cpuCount : 1,
      memoryMB: memoryMB > 0 ? memoryMB : 512,
      diskSizeMB: diskSizeMB > 0 ? diskSizeMB : 5120,
    }
    try {
      const updated = await templates.update(detail.templateID, patch)
      onSaved(updated)
      const refreshed = await templates.get(detail.templateID)
      setDetail(refreshed)
      syncFormFromDetail(refreshed)
      Toast.success('Settings saved.')
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'save failed')
    } finally {
      setSaving(false)
    }
  }

  async function onDelete() {
    if (!detail || builtin) return
    setDeleteBusy(true)
    try {
      await templates.remove(detail.templateID)
      onDeleted(detail.templateID)
      onClose()
    } catch (err) {
      setSaveError(err instanceof Error ? err.message : 'delete failed')
    } finally {
      setDeleteBusy(false)
    }
  }

  const profileOptions = [
    { value: 'shell', label: 'shell — workspace + terminal' },
    { value: 'dev', label: 'dev — shell + ports' },
    { value: 'browser', label: 'browser — dev + Chrome / MCP tools' },
  ]
  if (profile && !['shell', 'dev', 'browser'].includes(profile)) {
    profileOptions.push({ value: profile, label: profile })
  }

  return (
    <Modal
      title={
        <div style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
          <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
            {displayName || 'Template'}
          </span>
          {builtin && (
            <Tag size="small" color="grey">
              Built-in
            </Tag>
          )}
        </div>
      }
      visible={open && templateId != null}
      onCancel={() => {
        if (!busy && !deleteBusy) onClose()
      }}
      footer={null}
      maskClosable={!busy && !deleteBusy}
      closeOnEsc={!busy && !deleteBusy}
      width={512}
      bodyStyle={{ maxHeight: 'min(90dvh, 720px)', overflowY: 'auto' }}
    >
      {templateId && (
        <Typography.Text
          type="tertiary"
          size="small"
          ellipsis={{ showTooltip: true }}
          style={{
            display: 'block',
            marginBottom: 16,
            fontFamily: 'var(--semi-font-family-code)',
          }}
        >
          {templateId}
        </Typography.Text>
      )}

      {loading ? (
        <div style={{ padding: '24px 0', textAlign: 'center' }}>
          <Spin />
        </div>
      ) : loadError ? (
        <div role="alert">
          <Banner
            fullMode={false}
            type="danger"
            description={loadError}
            closeIcon={null}
          />
        </div>
      ) : detail ? (
        <>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'auto 1fr',
              gap: '8px 16px',
              marginBottom: 16,
              fontSize: 12,
              color: 'var(--semi-color-text-2)',
            }}
          >
            <span>Status</span>
            <span>
              <Tag
                color={statusTagColor(detail.buildStatus || 'waiting')}
                size="small"
              >
                {detail.buildStatus || 'waiting'}
              </Tag>
            </span>
            <span>Namespace</span>
            <Typography.Text
              size="small"
              style={{ fontFamily: 'var(--semi-font-family-code)' }}
            >
              {detail.namespace}
            </Typography.Text>
            <span>Profile</span>
            <Typography.Text
              size="small"
              style={{ fontFamily: 'var(--semi-font-family-code)' }}
            >
              {detail.profile || 'dev'}
            </Typography.Text>
            <span>Slot</span>
            <Typography.Text
              size="small"
              style={{ fontFamily: 'var(--semi-font-family-code)' }}
            >
              {detail.slot || 'agent'}
            </Typography.Text>
            <span>Usage</span>
            <span>
              {detail.spawnCount ?? 0} spawns · {detail.buildCount ?? 0} builds
            </span>
            <span>Updated</span>
            <span>{formatWhen(detail.updatedAt)}</span>
          </div>

          {builtin && (
            <Typography.Text
              type="tertiary"
              size="small"
              style={{ display: 'block', marginBottom: 12 }}
            >
              Seeded system template — editable and rebuildable; cannot be deleted.
            </Typography.Text>
          )}

          {(detail.tags?.length ?? 0) > 0 && (
            <div style={{ marginBottom: 16 }}>
              <Typography.Text
                size="small"
                type="tertiary"
                style={{ display: 'block', marginBottom: 8 }}
              >
                Tags
              </Typography.Text>
              <div
                style={{
                  maxHeight: 96,
                  overflowY: 'auto',
                  display: 'flex',
                  flexDirection: 'column',
                  gap: 4,
                }}
              >
                {detail.tags.map((t) => (
                  <Typography.Text
                    key={t.tag}
                    size="small"
                    ellipsis={{ showTooltip: true }}
                    style={{ fontFamily: 'var(--semi-font-family-code)' }}
                  >
                    {t.tag} → {t.buildID.slice(0, 8)}…
                  </Typography.Text>
                ))}
              </div>
            </div>
          )}

          {detail.builds.length > 0 && (
            <div style={{ marginBottom: 16 }}>
              <Typography.Text
                size="small"
                type="tertiary"
                style={{ display: 'block', marginBottom: 8 }}
              >
                Build history
              </Typography.Text>
              <div
                style={{
                  maxHeight: 128,
                  overflowY: 'auto',
                  display: 'flex',
                  flexDirection: 'column',
                  gap: 4,
                }}
              >
                {detail.builds.map((b) => (
                  <Typography.Text
                    key={b.buildID}
                    size="small"
                    ellipsis={{ showTooltip: true }}
                    style={{ fontFamily: 'var(--semi-font-family-code)' }}
                  >
                    {b.buildID.slice(0, 8)}… · {b.status}
                    {b.artifactRef ? ` · ${b.artifactRef}` : ''}
                  </Typography.Text>
                ))}
              </div>
            </div>
          )}

          <form
            onSubmit={(e) => void submit(e)}
            style={{ display: 'flex', flexDirection: 'column', gap: 16 }}
          >
            <div>
              <Typography.Text size="small" type="tertiary">
                Description
              </Typography.Text>
              <TextArea
                rows={2}
                value={description}
                onChange={setDescription}
              />
            </div>

            <div>
              <Typography.Text size="small" type="tertiary">
                Profile
              </Typography.Text>
              <Select
                value={profile}
                onChange={(v) => setProfile(String(v))}
                optionList={profileOptions}
                style={{ width: '100%' }}
              />
            </div>

            <div
              style={{
                display: 'grid',
                gridTemplateColumns: '1fr 1fr 1fr',
                gap: 12,
              }}
            >
              <div>
                <Typography.Text size="small" type="tertiary">
                  CPU
                </Typography.Text>
                <InputNumber
                  min={1}
                  value={cpuCount}
                  onChange={(v) => setCpuCount(typeof v === 'number' ? v : 1)}
                  style={{ width: '100%' }}
                />
              </div>
              <div>
                <Typography.Text size="small" type="tertiary">
                  Memory
                </Typography.Text>
                <InputNumber
                  min={128}
                  step={128}
                  value={memoryMB}
                  onChange={(v) => setMemoryMB(typeof v === 'number' ? v : 512)}
                  style={{ width: '100%' }}
                />
              </div>
              <div>
                <Typography.Text size="small" type="tertiary">
                  Disk
                </Typography.Text>
                <InputNumber
                  min={512}
                  step={512}
                  value={diskSizeMB}
                  onChange={(v) =>
                    setDiskSizeMB(typeof v === 'number' ? v : 5120)
                  }
                  style={{ width: '100%' }}
                />
              </div>
            </div>

            <Checkbox
              checked={isPublic}
              onChange={(e) => setIsPublic(!!e.target.checked)}
            >
              Public template
            </Checkbox>

            {(error || saveError) && (
              <div role="alert">
                <Banner
                  fullMode={false}
                  type="danger"
                  description={error || saveError}
                  closeIcon={null}
                />
              </div>
            )}

            <div
              style={{
                display: 'flex',
                flexWrap: 'wrap',
                justifyContent: 'flex-end',
                gap: 8,
                marginTop: 4,
              }}
            >
              {!builtin && (
                <Popconfirm
                  title={`Delete template “${displayName}”?`}
                  content="This cannot be undone."
                  onConfirm={() => void onDelete()}
                  disabled={busy || saving || deleteBusy}
                >
                  <Button
                    type="danger"
                    theme="borderless"
                    style={{ marginRight: 'auto' }}
                    loading={deleteBusy}
                    disabled={busy || saving}
                  >
                    Delete
                  </Button>
                </Popconfirm>
              )}
              <Button
                type="tertiary"
                disabled={busy || saving || deleteBusy}
                onClick={onClose}
              >
                Close
              </Button>
              <Button
                type="tertiary"
                disabled={busy || saving || deleteBusy}
                onClick={() => onBuild(detail)}
              >
                {buildActionLabel(detail.buildStatus || 'waiting')}
              </Button>
              <Button
                htmlType="submit"
                theme="solid"
                type="primary"
                loading={saving}
                disabled={busy || deleteBusy}
              >
                Save
              </Button>
            </div>
          </form>
        </>
      ) : null}
    </Modal>
  )
}
