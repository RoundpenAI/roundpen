import { useEffect, useState, type ReactNode } from 'react'
import { Button, CodeHighlight, Typography } from '@douyinfe/semi-ui-19'
import { IconClose, IconDownload } from '@douyinfe/semi-icons'
import { meWorkspace } from '../../api'
import type { MessageKey } from '../../i18n'
import { languageForFilename } from '../../lib/previewHighlight'
import { formatBytes, type FileKind } from '../../lib/workspaceFiles'

export type PreviewTarget = {
  name: string
  /** Workspace-relative path. */
  path: string
  size: number
  kind: FileKind
}

/** Text files above this size are not fetched into the pane. */
const TEXT_PREVIEW_MAX = 5 << 20

export function FilePreview({
  target,
  onClose,
  t,
}: {
  target: PreviewTarget
  onClose: () => void
  t: (key: MessageKey, vars?: Record<string, string | number>) => string
}) {
  const [text, setText] = useState<string | null>(null)
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    setText(null)
    setFailed(false)
    if (target.kind !== 'text' || target.size > TEXT_PREVIEW_MAX) {
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

  let body: ReactNode
  if (target.kind === 'image') {
    body = (
      <img
        src={meWorkspace.contentUrl(target.path)}
        alt={target.name}
        className="rp-ws-preview-img"
      />
    )
  } else if (target.kind === 'pdf') {
    body = (
      <iframe
        title={target.name}
        src={meWorkspace.contentUrl(target.path)}
        className="rp-ws-preview-frame"
      />
    )
  } else if (target.kind === 'text') {
    if (target.size > TEXT_PREVIEW_MAX) {
      body = (
        <Typography.Text type="tertiary">
          {t('workspace.previewTooLarge')}
        </Typography.Text>
      )
    } else if (failed) {
      body = (
        <Typography.Text type="danger">
          {t('workspace.previewFailed')}
        </Typography.Text>
      )
    } else if (text === null) {
      body = <Typography.Text type="tertiary">…</Typography.Text>
    } else {
      const lang = languageForFilename(target.name)
      body = lang ? (
        <CodeHighlight
          className="rp-ws-preview-text rp-ws-preview-code"
          code={text}
          language={lang}
          lineNumber
        />
      ) : (
        <pre className="rp-ws-preview-text">{text}</pre>
      )
    }
  } else {
    body = (
      <Typography.Text type="tertiary">
        {t('workspace.previewUnsupported')}
      </Typography.Text>
    )
  }

  return (
    <div className="rp-ws-preview">
      <div className="rp-ws-preview-bar">
        <div className="rp-ws-preview-meta">
          <Typography.Text strong ellipsis={{ showTooltip: true }}>
            {target.name}
          </Typography.Text>
          <Typography.Text type="tertiary" size="small">
            {formatBytes(target.size)}
          </Typography.Text>
        </div>
        <div className="rp-ws-preview-actions">
          <Button
            type="tertiary"
            icon={<IconDownload />}
            onClick={() => {
              window.open(
                meWorkspace.downloadUrl(target.path),
                '_blank',
                'noopener',
              )
            }}
          >
            {t('workspace.download')}
          </Button>
          <Button
            type="tertiary"
            icon={<IconClose />}
            aria-label={t('workspace.close')}
            onClick={onClose}
          >
            {t('workspace.close')}
          </Button>
        </div>
      </div>
      <div className="rp-ws-preview-body">{body}</div>
    </div>
  )
}
