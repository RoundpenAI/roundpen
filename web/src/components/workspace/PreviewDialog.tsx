import { useEffect, useState } from 'react'
import { Button, Modal, Typography } from '@douyinfe/semi-ui-19'
import { meWorkspace } from '../../api'
import type { MessageKey } from '../../i18n'
import { formatBytes, type FileKind } from '../../lib/workspaceFiles'

export type PreviewTarget = {
  name: string
  /** Workspace-relative path. */
  path: string
  size: number
  kind: FileKind
}

/** Text files above this size are not fetched into the dialog. */
const TEXT_PREVIEW_MAX = 5 << 20

export function PreviewDialog({
  target,
  onClose,
  t,
}: {
  target: PreviewTarget | null
  onClose: () => void
  t: (key: MessageKey, vars?: Record<string, string | number>) => string
}) {
  const [text, setText] = useState<string | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    setText(null)
    setFailed(false)
    if (!target || target.kind !== 'text' || target.size > TEXT_PREVIEW_MAX) {
      return
    }
    const ctl = new AbortController()
    fetch(meWorkspace.contentUrl(target.path), {
      credentials: 'include',
      signal: ctl.signal,
    })
      .then((res) => {
        if (!res.ok) throw new Error(String(res.status))
        return res.text()
      })
      .then(setText)
      .catch(() => {
        if (!ctl.signal.aborted) setFailed(true)
      })
    return () => ctl.abort()
  }, [target])

  const body = () => {
    if (!target) return null
    if (target.kind === 'image') {
      return (
        <img
          src={meWorkspace.contentUrl(target.path)}
          alt={target.name}
          style={{
            display: 'block',
            maxWidth: '100%',
            maxHeight: '60vh',
            margin: '0 auto',
            objectFit: 'contain',
          }}
        />
      )
    }
    if (target.kind === 'pdf') {
      return (
        <iframe
          title={target.name}
          src={meWorkspace.contentUrl(target.path)}
          style={{ width: '100%', height: '60vh', border: 'none' }}
        />
      )
    }
    if (target.kind === 'text') {
      if (target.size > TEXT_PREVIEW_MAX) {
        return (
          <Typography.Text type="tertiary">
            {t('workspace.previewTooLarge')}
          </Typography.Text>
        )
      }
      if (failed) {
        return (
          <Typography.Text type="danger">
            {t('workspace.previewFailed')}
          </Typography.Text>
        )
      }
      if (text === null) {
        return <Typography.Text type="tertiary">…</Typography.Text>
      }
      return (
        <pre
          style={{
            maxHeight: '60vh',
            overflow: 'auto',
            margin: 0,
            padding: 12,
            borderRadius: 6,
            background: 'var(--semi-color-fill-0)',
            fontSize: 12,
            whiteSpace: 'pre-wrap',
            wordBreak: 'break-word',
          }}
        >
          {text}
        </pre>
      )
    }
    return (
      <Typography.Text type="tertiary">
        {t('workspace.previewUnsupported')}
      </Typography.Text>
    )
  }

  return (
    <Modal
      title={target?.name}
      visible={target != null}
      onCancel={onClose}
      footer={
        <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end' }}>
          {target ? (
            <Typography.Text
              type="tertiary"
              size="small"
              style={{ marginRight: 'auto', lineHeight: '32px' }}
            >
              {formatBytes(target.size)}
            </Typography.Text>
          ) : null}
          <Button type="tertiary" onClick={onClose}>
            {t('workspace.close')}
          </Button>
          {target ? (
            <Button
              theme="solid"
              onClick={() => {
                window.open(meWorkspace.downloadUrl(target.path), '_blank', 'noopener')
              }}
            >
              {t('workspace.download')}
            </Button>
          ) : null}
        </div>
      }
    >
      {body()}
    </Modal>
  )
}
