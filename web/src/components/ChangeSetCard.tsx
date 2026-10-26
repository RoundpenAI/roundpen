import { useState } from 'react'
import { Link } from 'react-router-dom'
import {
  shouldFoldDiff,
  workspaceRelPath,
  type ChangeSet,
  type FileChange,
  workspacePreviewHref,
} from '../lib/changeSet'
import { downloadWorkspacePaths } from '../lib/workspaceDownload'
import { useT } from '../i18n'

type Props = {
  changeSet: ChangeSet
  /** Optional heading override. */
  title?: string
}

export function ChangeSetCard({ changeSet, title }: Props) {
  const t = useT()
  const [open, setOpen] = useState(true)
  const n = changeSet.files.length
  const heading =
    title ||
    (n === 1
      ? t('chat.changeset.fileChanged')
      : t('chat.changeset.filesChanged', { count: n }))

  return (
    <div className="rp-changeset">
      <div className="rp-changeset-summary-row">
        <button
          type="button"
          className="rp-changeset-summary"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
        >
          <span className={`chat-tool-chevron ${open ? 'open' : ''}`} aria-hidden>
            ▸
          </span>
          <span className="rp-changeset-title">{heading}</span>
          {changeSet.plus > 0 ? (
            <span className="chat-tool-plus"> +{changeSet.plus}</span>
          ) : null}
          {changeSet.minus > 0 ? (
            <span className="chat-tool-minus"> -{changeSet.minus}</span>
          ) : null}
        </button>
        {n > 0 ? (
          <button
            type="button"
            className="rp-changeset-action"
            onClick={() =>
              downloadWorkspacePaths(changeSet.files.map((f) => workspaceRelPath(f.path)))
            }
          >
            {t('chat.changeset.downloadAll')}
          </button>
        ) : null}
      </div>
      {open && (
        <ul className="rp-changeset-list">
          {changeSet.files.map((f) => (
            <FileChangeRow key={f.path} file={f} />
          ))}
        </ul>
      )}
    </div>
  )
}

function FileChangeRow({ file }: { file: FileChange }) {
  const t = useT()
  const large = shouldFoldDiff(file.unified)
  const [expanded, setExpanded] = useState(!large)
  const rel = workspaceRelPath(file.path)
  const href = workspacePreviewHref(file.path)

  return (
    <li className="rp-changeset-file">
      <div className="rp-changeset-file-head">
        <button
          type="button"
          className="rp-changeset-file-toggle"
          aria-expanded={expanded}
          onClick={() => setExpanded((v) => !v)}
        >
          <span className={`chat-tool-chevron ${expanded ? 'open' : ''}`} aria-hidden>
            ▸
          </span>
          <code className="rp-changeset-path">{file.path}</code>
          {file.plus > 0 ? (
            <span className="chat-tool-plus"> +{file.plus}</span>
          ) : null}
          {file.minus > 0 ? (
            <span className="chat-tool-minus"> -{file.minus}</span>
          ) : null}
        </button>
        <button
          type="button"
          className="rp-changeset-action"
          onClick={() => downloadWorkspacePaths([rel])}
        >
          {t('chat.changeset.download')}
        </button>
        <Link className="rp-changeset-action" to={href} title={t('chat.changeset.openTitle')}>
          {t('chat.changeset.open')}
        </Link>
      </div>
      {expanded ? (
        <pre className="rp-changeset-diff" tabIndex={0}>
          {file.unified.split(/\r?\n/).map((line, i) => (
            <span
              key={i}
              className={
                line.startsWith('+') && !line.startsWith('+++')
                  ? 'rp-diff-add'
                  : line.startsWith('-') && !line.startsWith('---')
                    ? 'rp-diff-del'
                    : line.startsWith('@@')
                      ? 'rp-diff-hunk'
                      : undefined
              }
            >
              {line}
              {'\n'}
            </span>
          ))}
        </pre>
      ) : (
        <button
          type="button"
          className="rp-changeset-fold"
          onClick={() => setExpanded(true)}
        >
          {t('chat.changeset.fold', {
            lines: file.unified.split(/\r?\n/).length,
          })}
        </button>
      )}
    </li>
  )
}
